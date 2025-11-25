import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import NodeCard from '../components/NodeCard'
import type { MonitorDashboard } from '../types'
import api from '../services/api'
import { formatBeijingTime } from '../utils/date'
import './SharedMonitor.css'

export default function SharedMonitor() {
  const { token } = useParams<{ token: string }>()
  const [dashboard, setDashboard] = useState<MonitorDashboard | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    async function fetchData() {
      if (!token) {
        setError('无效的分享链接')
        setLoading(false)
        return
      }
      try {
        const data = await api.getSharedMonitor(token)
        setDashboard(data)
        document.title = `${data.account_name} - 监控大屏`
      } catch (err) {
        setError('分享链接无效或已过期')
      } finally {
        setLoading(false)
      }
    }
    fetchData()
  }, [token])

  if (loading) {
    return <div className="shared-monitor-loading">加载中...</div>
  }

  if (error) {
    return (
      <div className="shared-monitor-error">
        <h2>{error}</h2>
        <p>请检查分享链接是否正确，或联系分享者获取新的链接。</p>
      </div>
    )
  }

  return (
    <div className="shared-monitor-page">
      <header>
        <h1>{dashboard?.account_name} · 监控大屏</h1>
        <p className="readonly-notice">
          只读模式 · 数据实时刷新（最近更新：{formatBeijingTime(dashboard?.updated_at)}）
        </p>
      </header>

      <div className="nodes-grid">
        {dashboard?.nodes.map((node) => (
          <NodeCard
            key={node.id}
            node={node}
            historyRefreshKey={0}
            historyDisabled={true}
          />
        ))}
      </div>

      <footer>
        <p>Powered by qcc_plus</p>
      </footer>
    </div>
  )
}
