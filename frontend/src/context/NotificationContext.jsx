import React, { createContext, useContext, useState, useEffect, useCallback } from 'react'
import { notificationService } from '../services/notificationService'
import { wsClient } from '../services/websocket'
import { useAuth } from './AuthContext'

const NotificationContext = createContext(null)

export function NotificationProvider({ children }) {
  const { isAuthenticated } = useAuth()
  const [notifications, setNotifications] = useState([])
  const [unreadCount, setUnreadCount] = useState(0)
  const [isLoading, setIsLoading] = useState(false)

  const refreshNotifications = useCallback(async () => {
    if (!isAuthenticated) {
      setNotifications([])
      setUnreadCount(0)
      return
    }

    try {
      setIsLoading(true)
      const data = await notificationService.listNotifications()
      const notifs = Array.isArray(data) ? data : (data?.notifications || [])
      const unread = typeof data?.unread_count === 'number'
        ? data.unread_count
        : notifs.filter((n) => !n.is_read).length
      setNotifications(notifs)
      setUnreadCount(unread)
    } catch {
      // ignore fetch errors
    } finally {
      setIsLoading(false)
    }
  }, [isAuthenticated])

  useEffect(() => {
    refreshNotifications()
  }, [refreshNotifications])

  // Listen to incoming WebSocket notifications
  useEffect(() => {
    if (!isAuthenticated) return

    const handleNewNotification = (payload) => {
      setNotifications((prev) => [payload, ...prev])
      setUnreadCount((c) => c + 1)
    }

    const handleReconnect = () => {
      refreshNotifications()
    }

    const unsubNotif = wsClient.on('NOTIFICATION_CREATED', handleNewNotification)
    const unsubOrder = wsClient.on('ORDER_STATUS_UPDATED', () => {
      refreshNotifications()
    })
    const unsubConnect = wsClient.on('_connected', handleReconnect)

    return () => {
      unsubNotif()
      unsubOrder()
      unsubConnect()
    }
  }, [isAuthenticated, refreshNotifications])

  const markAsRead = async (id) => {
    try {
      await notificationService.markAsRead(id)
      setNotifications((prev) =>
        prev.map((n) => (n.id === id ? { ...n, is_read: true } : n))
      )
      setUnreadCount((c) => Math.max(0, c - 1))
    } catch {
      // ignore
    }
  }

  const markAllAsRead = async () => {
    try {
      await notificationService.markAllAsRead()
      setNotifications((prev) => prev.map((n) => ({ ...n, is_read: true })))
      setUnreadCount(0)
    } catch {
      // ignore
    }
  }

  return (
    <NotificationContext.Provider
      value={{
        notifications,
        unreadCount,
        isLoading,
        refreshNotifications,
        markAsRead,
        markAllAsRead,
      }}
    >
      {children}
    </NotificationContext.Provider>
  )
}

export function useNotifications() {
  const context = useContext(NotificationContext)
  if (!context) {
    throw new Error('useNotifications must be used within a NotificationProvider')
  }
  return context
}
