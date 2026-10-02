import React, { useState } from 'react'
import {
  ShoppingBag,
  Trash2,
  Plus,
  Minus,
  ArrowRight,
  ShieldCheck,
  AlertCircle,
  Loader2,
  Clock,
} from 'lucide-react'
import { useCart } from '../context/CartContext'
import { orderService } from '../services/orderService'
import { PaymentModal } from '../components/PaymentModal'

export function CartPage({ onNavigateToMenu, onOrderCreated }) {
  const { cart, isLoading, updateCartItem, deleteCartItem, clearCart, refreshCart } = useCart()
  const [specialInstructions, setSpecialInstructions] = useState('')
  const [isCheckingOut, setIsCheckingOut] = useState(false)
  const [checkoutError, setCheckoutError] = useState(null)
  const [activePayment, setActivePayment] = useState(null)

  // Generate unique idempotency key per checkout attempt session
  const [idempotencyKey, setIdempotencyKey] = useState(() => {
    return 'cb_idemp_' + Math.random().toString(36).substring(2) + Date.now().toString(36)
  })

  const items = cart?.items || []
  const subtotal = items.reduce(
    (sum, item) => sum + (item.price || item.menu_item?.price || 0) * (item.quantity || 0),
    0
  )
  const total = subtotal

  const handleCheckout = async () => {
    if (items.length === 0) return

    setIsCheckingOut(true)
    setCheckoutError(null)

    try {
      // 1. Transactional order creation with idempotency key
      const orderRes = await orderService.createOrder(
        { special_instructions: specialInstructions },
        idempotencyKey
      )

      const order = orderRes?.order || orderRes
      if (!order || !order.id) {
        throw new Error('Failed to create order from tray')
      }

      // 2. Initiate Razorpay payment for this order
      const paymentRes = await orderService.initiatePayment(order.id)

      // Open payment modal
      setActivePayment(paymentRes)
    } catch (err) {
      // In case of error, refresh cart to ensure sync
      refreshCart()
      setCheckoutError(err instanceof Error ? err.message : 'Checkout failed')
    } finally {
      setIsCheckingOut(false)
    }
  }

  const handlePaymentSuccess = (orderId) => {
    setActivePayment(null)
    clearCart()
    // Reset idempotency key for future checkouts
    setIdempotencyKey('cb_idemp_' + Math.random().toString(36).substring(2) + Date.now().toString(36))
    onOrderCreated?.(orderId)
  }

  if (items.length === 0 && !isLoading) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-20 text-center space-y-6">
        <div className="w-24 h-24 bg-orange-50 rounded-3xl flex items-center justify-center mx-auto text-orange-400">
          <ShoppingBag className="w-12 h-12" />
        </div>
        <div className="space-y-2">
          <h2 className="text-2xl font-black text-gray-900">Your Tray is Empty</h2>
          <p className="text-sm text-gray-500 max-w-sm mx-auto">
            You haven't added any cafeteria items yet. Check out today's delicious menu!
          </p>
        </div>
        <button
          onClick={onNavigateToMenu}
          className="px-6 py-3.5 bg-orange-600 hover:bg-orange-700 text-white font-bold rounded-2xl shadow-lg shadow-orange-200 transition-all inline-flex items-center space-x-2 text-sm"
        >
          <span>Explore Menu</span>
          <ArrowRight className="w-4 h-4" />
        </button>
      </div>
    )
  }

  return (
    <div className="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-10">
      <div className="flex items-center justify-between pb-6 border-b border-gray-200">
        <div>
          <h1 className="text-2xl sm:text-3xl font-black text-gray-900">Your Meal Tray</h1>
          <p className="text-xs text-gray-500 mt-1">
            Items are reserved once checkout is confirmed
          </p>
        </div>
        {items.length > 0 && (
          <button
            onClick={clearCart}
            className="text-xs text-red-600 hover:text-red-700 font-semibold flex items-center space-x-1"
          >
            <Trash2 className="w-4 h-4" />
            <span>Clear Tray</span>
          </button>
        )}
      </div>

      {checkoutError && (
        <div className="mt-6 p-4 bg-red-50 text-red-700 text-sm rounded-2xl border border-red-100 flex items-start space-x-3">
          <AlertCircle className="w-5 h-5 text-red-500 shrink-0 mt-0.5" />
          <div className="space-y-1">
            <p className="font-bold">Checkout Notice</p>
            <p className="text-xs">{checkoutError}</p>
          </div>
        </div>
      )}

      <div className="mt-8 grid grid-cols-1 lg:grid-cols-3 gap-8">
        {/* Cart Item List */}
        <div className="lg:col-span-2 space-y-4">
          {items.map((item) => {
            const price = item.price || item.menu_item?.price || 0
            const itemTotal = item.subtotal || price * item.quantity
            const itemId = item.menu_item_id || item.id
            const itemName = item.name || item.menu_item?.name
            const itemImage = item.image_url || item.menu_item?.image_url

            return (
              <div
                key={itemId}
                className="bg-white p-5 rounded-3xl border border-gray-100 shadow-sm flex items-center justify-between gap-4"
              >
                <div className="flex items-center space-x-4">
                  <div className="w-16 h-16 rounded-2xl bg-gray-100 overflow-hidden shrink-0">
                    {itemImage ? (
                      <img
                        src={itemImage}
                        alt={itemName}
                        className="w-full h-full object-cover"
                      />
                    ) : (
                      <div className="w-full h-full flex items-center justify-center text-gray-400">
                        <ShoppingBag className="w-6 h-6" />
                      </div>
                    )}
                  </div>
                  <div>
                    <h4 className="font-bold text-gray-900 text-sm">{itemName}</h4>
                    <p className="text-xs text-gray-400 mt-0.5">₹{price.toFixed(2)} each</p>
                    {item.special_instructions && (
                      <p className="text-[11px] text-orange-600 bg-orange-50 px-2 py-0.5 rounded-md mt-1 inline-block">
                        Note: {item.special_instructions}
                      </p>
                    )}
                  </div>
                </div>

                {/* Quantity Controls & Total */}
                <div className="flex items-center space-x-4">
                  <div className="flex items-center space-x-2 bg-gray-50 border border-gray-200 rounded-xl p-1">
                    <button
                      onClick={() => {
                        if (item.quantity <= 1) {
                          deleteCartItem(itemId)
                        } else {
                          updateCartItem(itemId, item.quantity - 1, item.special_instructions)
                        }
                      }}
                      className="p-1 rounded-lg hover:bg-white text-gray-600 transition-colors"
                    >
                      <Minus className="w-3.5 h-3.5" />
                    </button>
                    <span className="w-6 text-center text-xs font-bold text-gray-900">
                      {item.quantity}
                    </span>
                    <button
                      onClick={() =>
                        updateCartItem(itemId, item.quantity + 1, item.special_instructions)
                      }
                      className="p-1 rounded-lg hover:bg-white text-gray-600 transition-colors"
                    >
                      <Plus className="w-3.5 h-3.5" />
                    </button>
                  </div>

                  <div className="text-right min-w-[70px]">
                    <span className="text-sm font-black text-gray-900">
                      ₹{itemTotal.toFixed(2)}
                    </span>
                  </div>

                  <button
                    onClick={() => deleteCartItem(itemId)}
                    className="p-2 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded-xl transition-colors"
                    title="Remove item"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            )
          })}

          {/* Kitchen Instructions */}
          <div className="bg-white p-5 rounded-3xl border border-gray-100 shadow-sm space-y-2">
            <label className="block text-xs font-bold uppercase tracking-wider text-gray-600">
              Kitchen Preparation Note
            </label>
            <input
              type="text"
              value={specialInstructions}
              onChange={(e) => setSpecialInstructions(e.target.value)}
              placeholder="e.g. Extra napkins, less spicy sauce, allergen notice..."
              className="w-full px-4 py-3 rounded-2xl bg-gray-50 border border-gray-200 text-xs focus:outline-none focus:ring-2 focus:ring-orange-500 focus:border-transparent transition-all"
            />
          </div>
        </div>

        {/* Order Summary & Checkout Card */}
        <div className="bg-white p-6 rounded-3xl border border-gray-100 shadow-sm h-fit space-y-6">
          <h3 className="font-black text-gray-900 text-lg">Order Summary</h3>

          <div className="space-y-3 text-sm text-gray-600">
            <div className="flex justify-between">
              <span>Subtotal</span>
              <span className="font-semibold text-gray-900">₹{subtotal.toFixed(2)}</span>
            </div>
            <div className="pt-3 border-t border-gray-100 flex justify-between items-baseline">
              <span className="font-bold text-gray-900 text-base">Grand Total</span>
              <span className="font-black text-2xl text-orange-600">₹{total.toFixed(2)}</span>
            </div>
          </div>

          <div className="p-3 bg-amber-50 rounded-2xl border border-amber-100 flex items-start space-x-2 text-amber-800 text-xs">
            <Clock className="w-4 h-4 shrink-0 mt-0.5 text-amber-600" />
            <span>
              Inventory is held for 15 minutes upon placing the order before payment expiry.
            </span>
          </div>

          <button
            onClick={handleCheckout}
            disabled={isCheckingOut || items.length === 0}
            className="w-full py-4 px-6 bg-orange-600 hover:bg-orange-700 text-white font-black rounded-2xl shadow-xl shadow-orange-200 transition-all flex items-center justify-center space-x-2 disabled:opacity-50 text-sm"
          >
            {isCheckingOut ? (
              <>
                <Loader2 className="w-5 h-5 animate-spin" />
                <span>Reserving Inventory...</span>
              </>
            ) : (
              <>
                <span>Proceed to Pay</span>
                <ArrowRight className="w-4 h-4" />
              </>
            )}
          </button>

          <div className="flex items-center justify-center space-x-1.5 text-xs text-gray-400">
            <ShieldCheck className="w-4 h-4 text-emerald-600" />
            <span>100% Concurrency-Safe Transaction</span>
          </div>
        </div>
      </div>

      {/* Payment Gateway Modal */}
      {activePayment && (
        <PaymentModal
          isOpen={Boolean(activePayment)}
          paymentDetails={activePayment}
          onClose={() => setActivePayment(null)}
          onSuccess={handlePaymentSuccess}
        />
      )}
    </div>
  )
}
