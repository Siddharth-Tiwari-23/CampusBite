// CampusBite WebSocket Client (JavaScript)

class WebSocketClient {
  constructor() {
    this.ws = null
    this.token = null
    this.reconnectTimer = null
    this.reconnectAttempts = 0
    this.maxReconnectDelay = 15000
    this.baseDelay = 1000
    this.listeners = new Map() // eventType -> Set of callbacks
    this.statusListeners = new Set() // Set of (status: string) => void
    this.isConnected = false
    this.isExplicitlyClosed = false
  }

  connect(token) {
    if (!token) return

    this.token = token
    this.isExplicitlyClosed = false

    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return
    }

    this.notifyStatus('connecting')

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const defaultWsUrl = `${protocol}//${window.location.host}/api/v1/ws`

    let baseUrl = import.meta.env.VITE_WS_URL
    if (!baseUrl && import.meta.env.VITE_API_BASE_URL && import.meta.env.VITE_API_BASE_URL.startsWith('http')) {
      baseUrl = `${import.meta.env.VITE_API_BASE_URL.replace(/^http/, 'ws')}/ws`
    }
    if (!baseUrl) {
      baseUrl = defaultWsUrl
    }
    const url = `${baseUrl}?token=${encodeURIComponent(token)}`

    try {
      this.ws = new WebSocket(url)

      this.ws.onopen = () => {
        this.isConnected = true
        this.reconnectAttempts = 0
        this.notifyStatus('connected')
        this.emit('_connected', { timestamp: new Date().toISOString() })
      }

      this.ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data)
          if (message && message.type) {
            this.emit(message.type, message.payload)
          }
          this.emit('*', message)
        } catch {
          // ignore non-json messages
        }
      }

      this.ws.onerror = () => {
        // error event
      }

      this.ws.onclose = () => {
        this.isConnected = false
        this.notifyStatus('disconnected')
        if (!this.isExplicitlyClosed) {
          this.scheduleReconnect()
        }
      }
    } catch {
      this.scheduleReconnect()
    }
  }

  scheduleReconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
    }

    const delay = Math.min(
      this.baseDelay * Math.pow(1.5, this.reconnectAttempts),
      this.maxReconnectDelay
    )
    this.reconnectAttempts++

    this.reconnectTimer = setTimeout(() => {
      if (this.token && !this.isExplicitlyClosed) {
        this.connect(this.token)
      }
    }, delay)
  }

  disconnect() {
    this.isExplicitlyClosed = true
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    this.isConnected = false
    this.notifyStatus('disconnected')
  }

  on(event, callback) {
    if (!this.listeners.has(event)) {
      this.listeners.set(event, new Set())
    }
    this.listeners.get(event).add(callback)

    return () => {
      this.off(event, callback)
    }
  }

  off(event, callback) {
    const handlers = this.listeners.get(event)
    if (handlers) {
      handlers.delete(callback)
      if (handlers.size === 0) {
        this.listeners.delete(event)
      }
    }
  }

  onStatusChange(callback) {
    this.statusListeners.add(callback)
    callback(this.isConnected ? 'connected' : 'disconnected')
    return () => {
      this.statusListeners.delete(callback)
    }
  }

  notifyStatus(status) {
    this.statusListeners.forEach((cb) => {
      try {
        cb(status)
      } catch {
        // ignore callback error
      }
    })
  }

  emit(event, data) {
    const handlers = this.listeners.get(event)
    if (handlers) {
      handlers.forEach((cb) => {
        try {
          cb(data)
        } catch {
          // ignore handler error
        }
      })
    }
  }

  send(data) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data))
    }
  }
}

export const wsClient = new WebSocketClient()
