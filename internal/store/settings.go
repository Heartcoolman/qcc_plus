package store

import "time"

// Setting 配置项
type Setting struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Scope       string    `json:"scope"` // system, account, user
	AccountID   *string   `json:"account_id,omitempty"`
	Value       any       `json:"value"`
	DataType    string    `json:"data_type"` // string, number, boolean, object, array, duration
	Category    string    `json:"category"`  // monitor, health, performance, notification, security
	Description *string   `json:"description,omitempty"`
	IsSecret    bool      `json:"is_secret"`
	Version     int       `json:"version"`
	UpdatedBy   *string   `json:"updated_by,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// SettingsStore 配置存储接口
type SettingsStore interface {
	// 获取所有配置（支持过滤）
	ListSettings(scope, category, accountID string) ([]Setting, error)

	// 获取单个配置
	GetSetting(key, scope, accountID string) (*Setting, error)

	// 创建或更新配置（乐观锁）
	UpsertSetting(s *Setting) error

	// 更新配置（带版本检查）
	UpdateSetting(s *Setting) error

	// 删除配置
	DeleteSetting(key, scope, accountID string) error

	// 批量更新
	BatchUpdateSettings(settings []Setting) error

	// 获取全局版本号（用于热更新检测）
	GetGlobalVersion() (int64, error)
}
