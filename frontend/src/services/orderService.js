import { request } from './api'

export const orderService = {
  async createOrder(payload, idempotencyKey) {
    const headers = {}
    if (idempotencyKey) {
      headers['Idempotency-Key'] = idempotencyKey
    }

    return request('/orders', {
      method: 'POST',
      headers,
      body: JSON.stringify(payload),
    })
  },

  async listOrders() {
    return request('/orders', { method: 'GET' })
  },

  async getOrder(orderId) {
    return request(`/orders/${orderId}`, { method: 'GET' })
  },

  async updateOrderStatus(orderId, status) {
    return request(`/orders/${orderId}/status`, {
      method: 'PATCH',
      body: JSON.stringify({ status }),
    })
  },

  async initiatePayment(orderId) {
    return request(`/orders/${orderId}/payment`, {
      method: 'POST',
    })
  },
}
