import { request } from './api'

export const menuService = {
  async getMenu(params) {
    let query = ''
    if (typeof params === 'string') {
      query = params.startsWith('?') ? params : `?category=${encodeURIComponent(params)}`
    } else if (params && typeof params === 'object') {
      const q = new URLSearchParams(params).toString()
      query = q ? `?${q}` : ''
    }
    return request(`/menu${query}`, { method: 'GET' })
  },

  async getMenuItem(id) {
    return request(`/menu/${id}`, { method: 'GET' })
  },

  async createMenuItem(payload) {
    return request('/menu', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },

  async updateMenuItem(id, payload) {
    return request(`/menu/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },

  async deleteMenuItem(id) {
    return request(`/menu/${id}`, {
      method: 'DELETE',
    })
  },

  async getInventory(itemId) {
    return request(`/inventory/${itemId}`, { method: 'GET' })
  },

  async updateInventory(itemId, payload) {
    return request(`/inventory/${itemId}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
}
