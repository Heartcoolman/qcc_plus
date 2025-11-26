import { useState } from 'react'
import { useSettings } from '../contexts/SettingsContext'
import './SystemSettings.css'

// 配置分类
const CATEGORIES = [
  { key: 'monitor', label: '显示设置' },
  { key: 'health', label: '监控设置' },
  { key: 'performance', label: '性能设置' },
  { key: 'notification', label: '通知设置' },
]

export default function SystemSettings() {
  const { settings, loading, updateSetting, refresh } = useSettings()
  const [activeCategory, setActiveCategory] = useState('monitor')
  const [saving, setSaving] = useState<string | null>(null)

  const handleChange = async (key: string, value: any) => {
    setSaving(key)
    try {
      await updateSetting(key, value)
    } catch (e: any) {
      alert(e?.message || '保存失败')
    } finally {
      setSaving(null)
    }
  }

  const filteredSettings = Object.values(settings).filter(s => s.category === activeCategory)

  if (loading) return <div className="settings-loading">加载中...</div>

  return (
    <div className="system-settings">
      <div className="settings-header">
        <h1>系统设置</h1>
        <button onClick={refresh}>刷新</button>
      </div>

      <div className="settings-tabs">
        {CATEGORIES.map(cat => (
          <button
            key={cat.key}
            className={`tab ${activeCategory === cat.key ? 'active' : ''}`}
            onClick={() => setActiveCategory(cat.key)}
          >
            {cat.label}
          </button>
        ))}
      </div>

      <div className="settings-list">
        {filteredSettings.map(setting => (
          <div key={setting.key} className="setting-item">
            <div className="setting-info">
              <div className="setting-key">{setting.key}</div>
              <div className="setting-desc">{setting.description || '-'}</div>
            </div>
            <div className="setting-control">
              {renderControl(setting, handleChange, saving === setting.key)}
            </div>
          </div>
        ))}
        {filteredSettings.length === 0 && (
          <div className="no-settings">该分类暂无配置项</div>
        )}
      </div>
    </div>
  )
}

function renderControl(setting: any, onChange: (key: string, value: any) => void, saving: boolean) {
  const { key, value, data_type, is_secret } = setting

  if (is_secret) {
    return <span className="secret-mask">******</span>
  }

  switch (data_type) {
    case 'boolean':
      return (
        <label className="toggle">
          <input
            type="checkbox"
            checked={!!value}
            onChange={e => onChange(key, e.target.checked)}
            disabled={saving}
          />
          <span className="slider"></span>
        </label>
      )
    case 'number':
      return (
        <input
          type="number"
          value={value ?? ''}
          onChange={e => onChange(key, parseFloat(e.target.value))}
          disabled={saving}
        />
      )
    case 'object':
      return (
        <textarea
          value={JSON.stringify(value, null, 2)}
          onChange={e => {
            try {
              onChange(key, JSON.parse(e.target.value))
            } catch {
              /* ignore parse errors */
            }
          }}
          disabled={saving}
        />
      )
    default:
      return (
        <input
          type="text"
          value={value ?? ''}
          onChange={e => onChange(key, e.target.value)}
          disabled={saving}
        />
      )
  }
}
