import { request } from './api'

export const cartService = {
  async getCart() {
    return request('/cart', { method: 'GET' })
  },

  async addToCart(itemId, quantity, specialInstructions = '') {
    return request('/cart/items', {
      method: 'POST',
      body: JSON.stringify({
        menu_item_id: itemId,
        item_id: itemId,
        quantity,
        special_instructions: specialInstructions,
      }),
    })
  },

  async updateCartItem(cartItemId, quantity, specialInstructions = '') {
    return request(`/cart/items/${cartItemId}`, {
      method: 'PATCH',
      body: JSON.stringify({
        quantity,
        special_instructions: specialInstructions,
      }),
    })
  },

  async deleteCartItem(cartItemId) {
    return request(`/cart/items/${cartItemId}`, {
      method: 'DELETE',
    })
  },

  async clearCart() {
    return request('/cart', {
      method: 'DELETE',
    })
  },
}
