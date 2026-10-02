// CampusBite API Client (JavaScript)

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api/v1'

export class ApiError extends Error {
  constructor(message, status, data = null) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.data = data
  }
}

export function getAuthToken() {
  return localStorage.getItem('token') || ''
}

export function setAuthToken(token) {
  if (token) {
    localStorage.setItem('token', token)
  } else {
    localStorage.removeItem('token')
  }
}

export function getStoredUser() {
  const raw = localStorage.getItem('user')
  if (!raw) return null
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}

export function setStoredUser(user) {
  if (user) {
    localStorage.setItem('user', JSON.stringify(user))
  } else {
    localStorage.removeItem('user')
  }
}

export async function request(path, options = {}) {
  const url = `${API_BASE_URL}${path.startsWith('/') ? path : `/${path}`}`
  const token = getAuthToken()

  const headers = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(options.headers || {}),
  }

  const response = await fetch(url, {
    ...options,
    headers,
  })

  let responseData
  const contentType = response.headers.get('content-type')
  if (contentType && contentType.includes('application/json')) {
    try {
      responseData = await response.json()
    } catch {
      responseData = null
    }
  } else {
    responseData = await response.text()
  }

  if (!response.ok) {
    if (response.status === 401) {
      // Clear token on 401 Unauthorized
      setAuthToken('')
      setStoredUser(null)
    }

    const message =
      (responseData && typeof responseData === 'object' && responseData.error) ||
      (responseData && typeof responseData === 'object' && responseData.message) ||
      (typeof responseData === 'string' && responseData) ||
      `Request failed with status ${response.status}`

    throw new ApiError(message, response.status, responseData)
  }

  return responseData
}
