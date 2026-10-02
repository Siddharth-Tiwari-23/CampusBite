import React, { createContext, useContext, useState, useEffect } from 'react'
import { authService } from '../services/authService'
import { getAuthToken, getStoredUser, setStoredUser } from '../services/api'
import { wsClient } from '../services/websocket'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [token, setTokenState] = useState(getAuthToken())
  const [user, setUserState] = useState(getStoredUser())
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    async function initAuth() {
      const savedToken = getAuthToken()
      if (savedToken) {
        try {
          const res = await authService.getMe()
          if (res && res.user) {
            setUserState(res.user)
            setStoredUser(res.user)
            wsClient.connect(savedToken)
          }
        } catch {
          // invalid/expired token
          authService.logout()
          setTokenState('')
          setUserState(null)
          wsClient.disconnect()
        }
      }
      setIsLoading(false)
    }

    initAuth()
  }, [])

  const login = async (email, password) => {
    const res = await authService.login({ email, password })
    if (res && res.token) {
      setTokenState(res.token)
      setUserState(res.user)
      wsClient.connect(res.token)
    }
    return res
  }

  const register = async (name, email, password, role = 'STUDENT') => {
    const res = await authService.register({ name, email, password, role })
    if (res && res.token) {
      setTokenState(res.token)
      setUserState(res.user)
      wsClient.connect(res.token)
    }
    return res
  }

  const logout = () => {
    authService.logout()
    setTokenState('')
    setUserState(null)
    wsClient.disconnect()
  }

  const isAuthenticated = Boolean(token && user)
  const isAdmin = user?.role === 'ADMIN'
  const isStaff = user?.role === 'STAFF' || user?.role === 'ADMIN'

  return (
    <AuthContext.Provider
      value={{
        token,
        user,
        isLoading,
        isAuthenticated,
        isAdmin,
        isStaff,
        login,
        register,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}
