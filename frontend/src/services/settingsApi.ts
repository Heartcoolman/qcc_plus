import { request } from './api'

export interface Setting {
  id: number
  key: string
  scope: string
  account_id?: string
  value: any
  data_type: string
  category: string
  description?: string
  is_secret: boolean
  version: number
  updated_at: string
}

export interface SettingsResponse {
  data: Setting[]
  version: number
}

export const settingsApi = {
  // 获取配置列表
  list: async (params?: { scope?: string; category?: string }): Promise<SettingsResponse> => {
    const query = params ? new URLSearchParams(params as any).toString() : ''
    const url = query ? `/api/settings?${query}` : '/api/settings'
    return request<SettingsResponse>(url)
  },

  // 获取单个配置
  get: async (key: string): Promise<Setting> => {
    return request<Setting>(`/api/settings/${encodeURIComponent(key)}`)
  },

  // 更新配置
  update: async (key: string, value: any, version: number, scope = 'system'): Promise<{ success: boolean; new_version: number }> => {
    return request(`/api/settings/${encodeURIComponent(key)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        value,
        version,
        scope,
      }),
    })
  },

  // 获取版本号
  getVersion: async (): Promise<number> => {
    const res = await request<{ version: number }>('/api/settings/version')
    return res.version || 0
  },
}
