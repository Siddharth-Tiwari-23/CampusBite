import { request, setAuthToken, setStoredUser } from './api'

export const authService = {
  async register(payload) {
    return request('/auth/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  async login(payload) {
    const res = await request('/auth/login', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
    if (res && res.token) {
      setAuthToken(res.token)
      if (res.user) {
        setStoredUser(res.user)
      }
    }
    return res
  },

  async getMe() {
    return request('/auth/me', {
      method: 'GET',
    })
  },

  logout() {
    setAuthToken('')
    setStoredUser(null)
  },
}
