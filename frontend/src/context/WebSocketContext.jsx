import React, { createContext, useContext, useState, useEffect } from 'react'
import { wsClient } from '../services/websocket'

const WebSocketContext = createContext(null)

export function WebSocketProvider({ children }) {
  const [status, setStatus] = useState('disconnected')

  useEffect(() => {
    const unsub = wsClient.onStatusChange((newStatus) => {
      setStatus(newStatus)
    })
    return () => unsub()
  }, [])

  return (
    <WebSocketContext.Provider
      value={{
        status,
        isConnected: status === 'connected',
        isConnecting: status === 'connecting',
        wsClient,
      }}
    >
      {children}
    </WebSocketContext.Provider>
  )
}

export function useWebSocket() {
  const context = useContext(WebSocketContext)
  if (!context) {
    throw new Error('useWebSocket must be used within a WebSocketProvider')
  }
  return context
}
