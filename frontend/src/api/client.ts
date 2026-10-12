// API client for the launch daemon.
// All requests include XTransformPort=8800 for Caddy gateway routing.

const API_PORT = '8800'

function apiUrl(path: string): string {
  const url = new URL(path, window.location.origin)
  url.searchParams.set('XTransformPort', API_PORT)
  return url.toString()
}

function wsUrl(path: string): string {
  const url = new URL(path, window.location.origin)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  url.searchParams.set('XTransformPort', API_PORT)
  return url.toString()
}

export interface ServiceSpec {
  name: string
  argv: string[]
  env?: string[]
  keepAlive: 'always' | 'on-failure' | 'never'
  runAtLoad?: boolean
  stopTimeoutSecs?: number
  maxRetries?: number
}

export interface ServiceStatus {
  state: string
  pid?: number
  startedAt?: string
  restarts: number
  lastExitCode?: number
  lastExitAt?: string
  heartbeatAt?: string
  heartbeatStatus?: string
  lastError?: string
}

export interface Service {
  spec: ServiceSpec
  status: ServiceStatus
}

export interface Health {
  version: string
  uptimeSec: number
  services: {
    running: number
    failed: number
    total: number
  }
  goroutines: number
}

export interface LogEntry {
  timestamp: string
  line: string
}

export interface Event {
  type: 'state' | 'log' | 'resync'
  name?: string
  data?: string
  line?: string
  timestamp: string
}

class ApiClient {
  private token: string | null = null

  setToken(token: string | null) {
    this.token = token
  }

  private headers(): HeadersInit {
    const h: HeadersInit = { 'Content-Type': 'application/json' }
    if (this.token) {
      h['Authorization'] = `Bearer ${this.token}`
    }
    return h
  }

  async health(): Promise<Health> {
    const res = await fetch(apiUrl('/api/v1/health'), { headers: this.headers() })
    if (!res.ok) throw new Error(`health: ${res.status}`)
    return res.json()
  }

  async listServices(): Promise<Service[]> {
    const res = await fetch(apiUrl('/api/v1/services'), { headers: this.headers() })
    if (!res.ok) throw new Error(`list: ${res.status}`)
    return res.json()
  }

  async getService(name: string): Promise<Service> {
    const res = await fetch(apiUrl(`/api/v1/services/${name}`), { headers: this.headers() })
    if (!res.ok) throw new Error(`get ${name}: ${res.status}`)
    return res.json()
  }

  async registerService(spec: ServiceSpec): Promise<void> {
    const res = await fetch(apiUrl('/api/v1/services'), {
      method: 'POST',
      headers: this.headers(),
      body: JSON.stringify(spec),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }))
      throw new Error(err.error || `register: ${res.status}`)
    }
  }

  async unregisterService(name: string): Promise<void> {
    const res = await fetch(apiUrl(`/api/v1/services/${name}`), {
      method: 'DELETE',
      headers: this.headers(),
    })
    if (!res.ok) throw new Error(`unregister: ${res.status}`)
  }

  async startService(name: string): Promise<void> {
    const res = await fetch(apiUrl(`/api/v1/services/${name}/start`), {
      method: 'POST',
      headers: this.headers(),
    })
    if (!res.ok) throw new Error(`start: ${res.status}`)
  }

  async stopService(name: string): Promise<void> {
    const res = await fetch(apiUrl(`/api/v1/services/${name}/stop`), {
      method: 'POST',
      headers: this.headers(),
    })
    if (!res.ok) throw new Error(`stop: ${res.status}`)
  }

  async restartService(name: string): Promise<void> {
    const res = await fetch(apiUrl(`/api/v1/services/${name}/restart`), {
      method: 'POST',
      headers: this.headers(),
    })
    if (!res.ok) throw new Error(`restart: ${res.status}`)
  }

  async getLogs(name: string, tail = 200): Promise<LogEntry[]> {
    const res = await fetch(apiUrl(`/api/v1/services/${name}/logs?tail=${tail}`), {
      headers: this.headers(),
    })
    if (!res.ok) throw new Error(`logs: ${res.status}`)
    const data = await res.json()
    return data.lines || []
  }

  connectEvents(onEvent: (event: Event) => void, onStateChange: (connected: boolean) => void): () => void {
    const url = wsUrl('/api/v1/events')
    const tokenParam = this.token ? `&token=${this.token}` : ''
    const ws = new WebSocket(url + tokenParam)

    ws.onopen = () => onStateChange(true)
    ws.onclose = () => onStateChange(false)
    ws.onerror = () => onStateChange(false)

    ws.onmessage = (e) => {
      try {
        const event = JSON.parse(e.data) as Event
        onEvent(event)
      } catch {
        // ignore malformed messages
      }
    }

    return () => ws.close()
  }
}

export const api = new ApiClient()
