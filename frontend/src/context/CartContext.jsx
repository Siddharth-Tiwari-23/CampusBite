import React, { createContext, useContext, useState, useEffect, useCallback } from 'react'
import { cartService } from '../services/cartService'
import { useAuth } from './AuthContext'

const CartContext = createContext(null)

export function CartProvider({ children }) {
  const { isAuthenticated } = useAuth()
  const [cart, setCart] = useState(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState(null)

  const refreshCart = useCallback(async () => {
    if (!isAuthenticated) {
      setCart(null)
      return
    }

    try {
      setIsLoading(true)
      setError(null)
      const data = await cartService.getCart()
      setCart(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch cart')
    } finally {
      setIsLoading(false)
    }
  }, [isAuthenticated])

  useEffect(() => {
    refreshCart()
  }, [refreshCart])

  const addToCart = async (itemId, quantity = 1, specialInstructions = '') => {
    try {
      setIsLoading(true)
      setError(null)
      const data = await cartService.addToCart(itemId, quantity, specialInstructions)
      setCart(data)
      return data
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to add item to cart'
      setError(msg)
      throw err
    } finally {
      setIsLoading(false)
    }
  }

  const updateCartItem = async (cartItemId, quantity, specialInstructions = '') => {
    try {
      setIsLoading(true)
      setError(null)
      const data = await cartService.updateCartItem(cartItemId, quantity, specialInstructions)
      setCart(data)
      return data
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to update cart'
      setError(msg)
      throw err
    } finally {
      setIsLoading(false)
    }
  }

  const deleteCartItem = async (cartItemId) => {
    try {
      setIsLoading(true)
      setError(null)
      const data = await cartService.deleteCartItem(cartItemId)
      setCart(data)
      return data
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to remove item'
      setError(msg)
      throw err
    } finally {
      setIsLoading(false)
    }
  }

  const clearCart = async () => {
    try {
      setIsLoading(true)
      setError(null)
      await cartService.clearCart()
      setCart(null)
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to clear cart'
      setError(msg)
      throw err
    } finally {
      setIsLoading(false)
    }
  }

  const totalItemCount =
    cart?.items?.reduce((acc, item) => acc + (item.quantity || 0), 0) || 0

  return (
    <CartContext.Provider
      value={{
        cart,
        isLoading,
        error,
        totalItemCount,
        refreshCart,
        addToCart,
        updateCartItem,
        deleteCartItem,
        clearCart,
      }}
    >
      {children}
    </CartContext.Provider>
  )
}

export function useCart() {
  const context = useContext(CartContext)
  if (!context) {
    throw new Error('useCart must be used within a CartProvider')
  }
  return context
}
