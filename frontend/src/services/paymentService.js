import { request } from './api'

export const paymentService = {
  async verifyPayment(payload) {
    return request('/payments/verify', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
}
