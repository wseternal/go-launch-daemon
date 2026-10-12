import { Badge, Tag } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'

const stateColors: Record<string, string> = {
  registered: 'default',
  starting: 'processing',
  running: 'success',
  stopping: 'warning',
  stopped: 'default',
  failed: 'error',
  restarting: 'processing',
  backoff: 'warning',
}

export function StatusBadge({ state }: { state: string }) {
  const color = stateColors[state] || 'default'
  return <Badge status={color as 'default' | 'processing' | 'success' | 'warning' | 'error'} text={<Tag>{state}</Tag>} />
}

export function HealthIndicator() {
  const { data: health } = useQuery({
    queryKey: ['health'],
    queryFn: api.health.bind(api),
    refetchInterval: 5000,
  })

  if (!health) return null

  const { running, failed, total } = health.services

  return (
    <span style={{ fontSize: 12, color: '#666' }}>
      <Badge status="success" text={`${running} running`} />
      {failed > 0 && <Badge status="error" text={`${failed} failed`} style={{ marginLeft: 8 }} />}
      <span style={{ marginLeft: 8 }}>/ {total} total</span>
    </span>
  )
}
