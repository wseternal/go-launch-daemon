import { useParams, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Card, Descriptions, Button, Space, Spin, Tag, Typography, message, Popconfirm } from 'antd'
import { ArrowLeftOutlined, PlayCircleOutlined, PauseCircleOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { LogViewer } from '../components/LogViewer'
import { useEvents } from '../hooks/useEvents'
import dayjs from 'dayjs'

const { Title } = Typography

export default function ServiceDetail() {
  const { name } = useParams<{ name: string }>()
  const queryClient = useQueryClient()
  useEvents()

  const { data: service, isLoading } = useQuery({
    queryKey: ['service', name],
    queryFn: () => api.getService(name!),
    enabled: !!name,
    refetchInterval: 3000,
  })

  const { data: logs } = useQuery({
    queryKey: ['logs', name],
    queryFn: () => api.getLogs(name!, 200),
    enabled: !!name,
    refetchInterval: 5000,
  })

  const startMutation = useMutation({
    mutationFn: () => api.startService(name!),
    onSuccess: () => {
      message.success('Service starting')
      queryClient.invalidateQueries({ queryKey: ['service', name] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const stopMutation = useMutation({
    mutationFn: () => api.stopService(name!),
    onSuccess: () => {
      message.success('Service stopping')
      queryClient.invalidateQueries({ queryKey: ['service', name] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const restartMutation = useMutation({
    mutationFn: () => api.restartService(name!),
    onSuccess: () => {
      message.success('Service restarting')
      queryClient.invalidateQueries({ queryKey: ['service', name] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  if (isLoading) {
    return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />
  }

  if (!service) {
    return <Card>Service not found</Card>
  }

  const { spec, status } = service
  const isRunning = status.state === 'running' || status.state === 'starting'
  const isStopped = status.state === 'stopped' || status.state === 'failed' || status.state === 'registered'

  const uptime = status.startedAt && isRunning
    ? formatDuration(dayjs().diff(dayjs(status.startedAt), 'second'))
    : '-'

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Card>
        <Space align="center" style={{ marginBottom: 16 }}>
          <Link to="/">
            <Button icon={<ArrowLeftOutlined />} type="text" />
          </Link>
          <Title level={4} style={{ margin: 0 }}>{spec.name}</Title>
          <StatusBadge state={status.state} />
        </Space>

        <Space style={{ marginBottom: 16 }}>
          {isStopped && (
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              onClick={() => startMutation.mutate()}
            >
              Start
            </Button>
          )}
          {isRunning && (
            <>
              <Popconfirm
                title="Stop this service?"
                onConfirm={() => stopMutation.mutate()}
                okText="Stop"
                cancelText="Cancel"
              >
                <Button icon={<PauseCircleOutlined />}>Stop</Button>
              </Popconfirm>
              <Button
                icon={<ReloadOutlined />}
                onClick={() => restartMutation.mutate()}
              >
                Restart
              </Button>
            </>
          )}
        </Space>

        <Descriptions bordered column={2} size="small">
          <Descriptions.Item label="Command">
            <code>{spec.argv.join(' ')}</code>
          </Descriptions.Item>
          <Descriptions.Item label="Keep Alive">
            <Tag>{spec.keepAlive}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="PID">
            {status.pid || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="Uptime">
            {uptime}
          </Descriptions.Item>
          <Descriptions.Item label="Restarts">
            {status.restarts}
          </Descriptions.Item>
          <Descriptions.Item label="Last Exit Code">
            {status.lastExitCode !== undefined ? (
              <Tag color={status.lastExitCode === 0 ? 'success' : 'error'}>
                {status.lastExitCode}
              </Tag>
            ) : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="Last Exit At">
            {status.lastExitAt ? dayjs(status.lastExitAt).format('HH:mm:ss') : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="Stop Timeout">
            {spec.stopTimeoutSecs || 10}s
          </Descriptions.Item>
          {spec.env && spec.env.length > 0 && (
            <Descriptions.Item label="Environment" span={2}>
              {spec.env.map((e, i) => (
                <Tag key={i}>{e}</Tag>
              ))}
            </Descriptions.Item>
          )}
          {status.lastError && (
            <Descriptions.Item label="Last Error" span={2}>
              <Tag color="error">{status.lastError}</Tag>
            </Descriptions.Item>
          )}
        </Descriptions>
      </Card>

      <Card title="Logs" bordered={false}>
        <LogViewer logs={logs || []} />
      </Card>
    </Space>
  )
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  const hours = Math.floor(seconds / 3600)
  const mins = Math.floor((seconds % 3600) / 60)
  return `${hours}h ${mins}m`
}
