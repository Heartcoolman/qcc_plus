package proxy

import (
	"reflect"
	"sync"

	"qcc_plus/internal/store"
)

// SettingsCache 配置缓存
// 负责从存储加载配置并在变更时触发回调。
type SettingsCache struct {
	mu       sync.RWMutex
	data     map[string]any // key -> value
	version  int64          // 全局版本号（最大设置版本）
	store    store.SettingsStore
	onChange []func(key string, value any) // 变更回调
}

func NewSettingsCache(s store.SettingsStore) *SettingsCache {
	c := &SettingsCache{
		data:  make(map[string]any),
		store: s,
	}
	c.loadAll()
	return c
}

// Get 获取配置值
func (c *SettingsCache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[key]
	return v, ok
}

// GetInt 获取整数配置
func (c *SettingsCache) GetInt(key string, defaultVal int) int {
	if v, ok := c.Get(key); ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return defaultVal
}

// GetString 获取字符串配置
func (c *SettingsCache) GetString(key string, defaultVal string) string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

// GetBool 获取布尔配置
func (c *SettingsCache) GetBool(key string, defaultVal bool) bool {
	if v, ok := c.Get(key); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

// Set 更新配置（同时更新数据库），并触发回调。
func (c *SettingsCache) Set(key string, value any) error {
	if c.store == nil {
		return nil
	}

	setting := &store.Setting{
		Key:   key,
		Value: value,
		Scope: "system",
	}
	if err := c.store.UpsertSetting(setting); err != nil {
		return err
	}

	c.mu.Lock()
	c.data[key] = value
	if v := int64(setting.Version); v > 0 {
		c.version = maxInt64(c.version, v)
	} else {
		c.version++
	}
	c.mu.Unlock()

	c.notifyChange(key, value)
	return nil
}

// UpdateLocal 在外部已经更新存储成功后，刷新缓存并触发回调。
func (c *SettingsCache) UpdateLocal(key string, value any, version int64) {
	c.mu.Lock()
	c.data[key] = value
	if version > 0 {
		c.version = maxInt64(c.version, version)
	}
	c.mu.Unlock()
	c.notifyChange(key, value)
}

// OnChange 注册变更回调
func (c *SettingsCache) OnChange(fn func(key string, value any)) {
	c.mu.Lock()
	c.onChange = append(c.onChange, fn)
	c.mu.Unlock()
}

// loadAll 从数据库加载所有配置
func (c *SettingsCache) loadAll() {
	c.reload(false)
}

// Refresh 刷新缓存（定期调用），对变更项触发回调。
func (c *SettingsCache) Refresh() {
	c.reload(true)
}

func (c *SettingsCache) reload(notify bool) {
	if c.store == nil {
		return
	}
	settings, err := c.store.ListSettings("system", "", "")
	if err != nil {
		return
	}

	newData := make(map[string]any, len(settings))
	var maxVer int64
	for _, s := range settings {
		newData[s.Key] = s.Value
		if v := int64(s.Version); v > maxVer {
			maxVer = v
		}
	}

	var changed []struct {
		key string
		val any
	}
	var removed []string

	c.mu.Lock()
	if notify {
		for k, v := range newData {
			if old, ok := c.data[k]; !ok || !reflect.DeepEqual(old, v) {
				changed = append(changed, struct {
					key string
					val any
				}{k, v})
			}
		}
		for k := range c.data {
			if _, ok := newData[k]; !ok {
				removed = append(removed, k)
			}
		}
	}
	c.data = newData
	if maxVer > 0 {
		c.version = maxVer
	}
	c.mu.Unlock()

	if notify {
		for _, ch := range changed {
			c.notifyChange(ch.key, ch.val)
		}
		for _, key := range removed {
			c.notifyChange(key, nil)
		}
	}
}

func (c *SettingsCache) notifyChange(key string, value any) {
	c.mu.RLock()
	callbacks := append([]func(string, any){}, c.onChange...)
	c.mu.RUnlock()

	for _, fn := range callbacks {
		fn(key, value)
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
