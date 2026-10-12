import { useEffect, useRef, useState } from 'react'
import { Button, Space } from 'antd'
import { PauseOutlined, CaretRightOutlined, VerticalAlignBottomOutlined } from '@ant-design/icons'

interface LogLine {
  timestamp: string
  line: string
}

interface LogViewerProps {
  logs: LogLine[]
  maxLines?: number
}

export function LogViewer({ logs, maxLines = 500 }: LogViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [autoScroll, setAutoScroll] = useState(true)
  const [skipped, setSkipped] = useState(0)

  // Trim to maxLines and track skipped count.
  const displayLogs = logs.slice(-maxLines)
  const skippedCount = logs.length - displayLogs.length

  useEffect(() => {
    if (skippedCount !== skipped) {
      setSkipped(skippedCount)
    }
  }, [skippedCount, skipped])

  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
  }, [displayLogs, autoScroll])

  // Detect user scrolling up to pause auto-scroll.
  useEffect(() => {
    const el = containerRef.current
    if (!el) return

    const handleScroll = () => {
      const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50
      setAutoScroll(atBottom)
    }

    el.addEventListener('scroll', handleScroll)
    return () => el.removeEventListener('scroll', handleScroll)
  }, [])

  const scrollToBottom = () => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
      setAutoScroll(true)
    }
  }

  return (
    <div>
      <Space style={{ marginBottom: 8 }}>
        <Button
          size="small"
          icon={autoScroll ? <PauseOutlined /> : <CaretRightOutlined />}
          onClick={() => setAutoScroll(!autoScroll)}
        >
          {autoScroll ? 'Pause' : 'Resume'}
        </Button>
        <Button size="small" icon={<VerticalAlignBottomOutlined />} onClick={scrollToBottom}>
          Bottom
        </Button>
        {skipped > 0 && (
          <span style={{ fontSize: 12, color: '#999' }}>{skipped} lines skipped</span>
        )}
      </Space>
      <div
        ref={containerRef}
        style={{
          height: 400,
          overflow: 'auto',
          background: '#1e1e1e',
          color: '#d4d4d4',
          fontFamily: 'monospace',
          fontSize: 12,
          padding: 12,
          borderRadius: 4,
        }}
      >
        {displayLogs.length === 0 ? (
          <div style={{ color: '#666' }}>No logs yet...</div>
        ) : (
          displayLogs.map((log, i) => (
            <div key={i} style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
              {log.line}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
