import type { HealthCheckRecord, MonitorNode } from '../types'
import { formatBeijingTime } from '../utils/date'
import HealthTimeline from './HealthTimeline'
import './NodeCard.css'

interface NodeCardProps {
	node: MonitorNode
	historyRefreshKey: number
	healthEvent?: HealthCheckRecord | null
	historyDisabled?: boolean
}

const statusLabel: Record<string, string> = {
  online: '在线',
  offline: '离线',
  disabled: '停用',
}

export default function NodeCard({ node, historyRefreshKey, healthEvent, historyDisabled }: NodeCardProps) {
	const resolvedStatus = node.disabled ? 'disabled' : node.status || 'offline'
	const successRate = Number.isFinite(node.success_rate) ? Number(node.success_rate) : 0
	const avgTime = Number.isFinite(node.avg_response_time) ? Number(node.avg_response_time) : 0
  const totalReq = Number(node.total_requests ?? 0)
  const failedReq = Number(node.failed_requests ?? 0)
  const lastCheck = node.last_check_at ? formatBeijingTime(node.last_check_at) : '暂无'
  const lastError = (node.last_error || '').trim()

  return (
    <div className="node-card">
      <div className="node-card__header">
        <div className="node-card__title-wrap">
          <div className="node-card__title">{node.name || '未命名节点'}</div>
          <div className="node-card__url">{node.url || '-'}</div>
        </div>
        <div className={`node-card__status ${resolvedStatus}`}>
          <span className="dot" />
          {statusLabel[resolvedStatus] || resolvedStatus || '未知'}
        </div>
      </div>

	<div className="node-card__metrics">
		<Metric label="权重" value={node.weight ?? '-'} />
		<Metric label="成功率" value={`${successRate.toFixed(1)}%`} />
		<Metric label="平均耗时" value={avgTime ? `${avgTime} ms` : '--'} />
		<Metric label="请求数" value={totalReq.toLocaleString()} />
		<Metric label="失败数" value={failedReq.toLocaleString()} danger={failedReq > 0} />
		<Metric label="健康检查" value={lastCheck} />
	</div>

	<HealthTimeline
		nodeId={node.id}
		refreshKey={historyRefreshKey}
		latest={healthEvent}
		disabled={historyDisabled}
	/>

	<div className="node-card__footer">
        <div className="node-card__badges">
          {node.is_active && <span className="badge badge-primary">当前主用</span>}
          {node.disabled && <span className="badge badge-muted">已停用</span>}
        </div>
        {lastError && <div className="node-card__error">最后错误：{lastError}</div>}
      </div>
    </div>
  )
}

function Metric({ label, value, danger = false }: { label: string; value: string | number; danger?: boolean }) {
  return (
    <div className={`node-card__metric${danger ? ' danger' : ''}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}
