import { request } from './api'

export const notificationService = {
  async listNotifications(unreadOnly = false) {
    const query = unreadOnly ? '?unread_only=true' : ''
    return request(`/notifications${query}`, { method: 'GET' })
  },

  async markAsRead(id) {
    return request(`/notifications/${id}/read`, {
      method: 'PATCH',
    })
  },

  async markAllAsRead() {
    return request('/notifications/read-all', {
      method: 'PATCH',
    })
  },
}
