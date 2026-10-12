import { useEffect, useRef, useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, type Event } from '../api/client'

// useEvents connects to the WebSocket event stream and updates the query cache.
export function useEvents() {
  const queryClient = useQueryClient()
  const connectedRef = useRef(false)
  const reconnectTimeoutRef = useRef<number | null>(null)
  const reconnectDelayRef = useRef(1000)

  const connect = useCallback(() => {
    const cleanup = api.connectEvents(
      (event: Event) => {
        if (event.type === 'resync') {
          // Full resync — invalidate all service queries.
          queryClient.invalidateQueries({ queryKey: ['services'] })
          return
        }

        if (event.type === 'state' && event.name) {
          // State change — invalidate the specific service.
          queryClient.invalidateQueries({ queryKey: ['service', event.name] })
          queryClient.invalidateQueries({ queryKey: ['services'] })
        }

        if (event.type === 'log' && event.name) {
          // Log line — append to logs cache.
          const key = ['logs', event.name]
          queryClient.setQueryData<LogLine[]>(key, (old) => {
            const line: LogLine = {
              timestamp: event.timestamp,
              line: event.line || '',
            }
            const updated = [...(old || []), line]
            // Cap at 500 lines.
            return updated.slice(-500)
          })
        }
      },
      (connected: boolean) => {
        connectedRef.current = connected
        if (!connected) {
          // Reconnect with exponential backoff.
          const delay = reconnectDelayRef.current
          reconnectTimeoutRef.current = window.setTimeout(() => {
            reconnectDelayRef.current = Math.min(delay * 2, 16000)
            connect()
          }, delay)
        } else {
          // Reset backoff on successful connection.
          reconnectDelayRef.current = 1000
        }
      }
    )

    return cleanup
  }, [queryClient])

  useEffect(() => {
    const cleanup = connect()

    // Polling fallback when disconnected (2s interval).
    const pollInterval = window.setInterval(() => {
      if (!connectedRef.current) {
        queryClient.invalidateQueries({ queryKey: ['services'] })
      }
    }, 2000)

    // WS watchdog (30s) — reconnect if no events received.
    const watchdog = window.setInterval(() => {
      if (connectedRef.current) {
        // Force a resync check.
        queryClient.invalidateQueries({ queryKey: ['services'] })
      }
    }, 30000)

    return () => {
      cleanup()
      window.clearInterval(pollInterval)
      window.clearInterval(watchdog)
      if (reconnectTimeoutRef.current) {
        window.clearTimeout(reconnectTimeoutRef.current)
      }
    }
  }, [connect, queryClient])
}

interface LogLine {
  timestamp: string
  line: string
}
