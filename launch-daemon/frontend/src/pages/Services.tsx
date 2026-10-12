import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Table, Button, Space, Tag, Popconfirm, message, Card, Typography } from 'antd'
import { PlayCircleOutlined, PauseCircleOutlined, ReloadOutlined, DeleteOutlined } from '@ant-design/icons'
import { Link } from 'react-router-dom'
import { api, type Service } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { useEvents } from '../hooks/useEvents'
import dayjs from 'dayjs'

const { Text } = Typography

export default function Services() {
  const queryClient = useQueryClient()
  useEvents() // Connect to WebSocket for live updates.

  const { data: services, isLoading } = useQuery({
    queryKey: ['services'],
    queryFn: api.listServices.bind(api),
    refetchInterval: 5000, // Poll every 5s as fallback.
  })

  const startMutation = useMutation({
    mutationFn: api.startService.bind(api),
    onSuccess: () => {
      message.success('Service starting')
      queryClient.invalidateQueries({ queryKey: ['services'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const stopMutation = useMutation({
    mutationFn: api.stopService.bind(api),
    onSuccess: () => {
      message.success('Service stopping')
      queryClient.invalidateQueries({ queryKey: ['services'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const restartMutation = useMutation({
    mutationFn: api.restartService.bind(api),
    onSuccess: () => {
      message.success('Service restarting')
      queryClient.invalidateQueries({ queryKey: ['services'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const unregisterMutation = useMutation({
    mutationFn: api.unregisterService.bind(api),
    onSuccess: () => {
      message.success('Service unregistered')
      queryClient.invalidateQueries({ queryKey: ['services'] })
    },
    onError: (err: Error) => message.error(err.message),
  })

  const columns = [
    {
      title: 'Name',
      dataIndex: ['spec', 'name'],
      key: 'name',
      render: (name: string) => (
        <Link to={`/services/${name}`}>
          <Text strong>{name}</Text>
        </Link>
      ),
    },
    {
      title: 'Status',
      dataIndex: ['status', 'state'],
      key: 'state',
      width: 120,
      render: (state: string) => <StatusBadge state={state} />,
    },
    {
      title: 'PID',
      dataIndex: ['status', 'pid'],
      key: 'pid',
      width: 80,
      render: (pid: number | undefined) => pid || '-',
    },
    {
      title: 'Uptime',
      dataIndex: ['status', 'startedAt'],
      key: 'uptime',
      width: 120,
      render: (startedAt: string | undefined, record: Service) => {
        if (!startedAt || record.status.state !== 'running') return '-'
        const seconds = dayjs().diff(dayjs(startedAt), 'second')
        if (seconds < 60) return `${seconds}s`
        if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
        return `${Math.floor(seconds / 3600)}h`
      },
    },
    {
      title: 'Restarts',
      dataIndex: ['status', 'restarts'],
      key: 'restarts',
      width: 80,
    },
    {
      title: 'Last Exit',
      dataIndex: ['status', 'lastExitCode'],
      key: 'lastExit',
      width: 80,
      render: (code: number | undefined) => {
        if (code === undefined) return '-'
        return (
          <Tag color={code === 0 ? 'success' : 'error'}>
            {code}
          </Tag>
        )
      },
    },
    {
      title: 'Policy',
      dataIndex: ['spec', 'keepAlive'],
      key: 'keepAlive',
      width: 100,
    },
    {
      title: 'Actions',
      key: 'actions',
      width: 180,
      render: (_: unknown, record: Service) => {
        const { state } = record.status
        const name = record.spec.name
        const isRunning = state === 'running' || state === 'starting'
        const isStopped = state === 'stopped' || state === 'failed' || state === 'registered'

        return (
          <Space size="small">
            {isStopped && (
              <Button
                type="link"
                size="small"
                icon={<PlayCircleOutlined />}
                onClick={() => startMutation.mutate(name)}
              >
                Start
              </Button>
            )}
            {isRunning && (
              <>
                <Popconfirm
                  title="Stop this service?"
                  onConfirm={() => stopMutation.mutate(name)}
                  okText="Stop"
                  cancelText="Cancel"
                >
                  <Button type="link" size="small" icon={<PauseCircleOutlined />}>
                    Stop
                  </Button>
                </Popconfirm>
                <Button
                  type="link"
                  size="small"
                  icon={<ReloadOutlined />}
                  onClick={() => restartMutation.mutate(name)}
                >
                  Restart
                </Button>
              </>
            )}
            {isStopped && (
              <Popconfirm
                title="Unregister this service?"
                description="This cannot be undone."
                onConfirm={() => unregisterMutation.mutate(name)}
                okText="Delete"
                cancelText="Cancel"
                okButtonProps={{ danger: true }}
              >
                <Button type="link" size="small" danger icon={<DeleteOutlined />}>
                  Delete
                </Button>
              </Popconfirm>
            )}
          </Space>
        )
      },
    },
  ]

  return (
    <Card title="Services" bordered={false}>
      <Table
        dataSource={services || []}
        columns={columns}
        rowKey={(record) => record.spec.name}
        loading={isLoading}
        pagination={false}
        size="middle"
      />
    </Card>
  )
}
