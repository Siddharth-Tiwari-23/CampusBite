import React, { useState, useEffect, useCallback } from 'react'
import {
  ArrowLeft,
  CheckCircle2,
  ChefHat,
  PackageCheck,
  Ban,
  CreditCard,
  Loader2,
  AlertCircle,
} from 'lucide-react'
import { orderService } from '../services/orderService'
import { wsClient } from '../services/websocket'
import { PaymentModal } from '../components/PaymentModal'

export function OrderDetailPage({ orderId, onBack }) {
  const [order, setOrder] = useState(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState(null)
  const [activePayment, setActivePayment] = useState(null)
  const [isInitiatingPay, setIsInitiatingPay] = useState(false)

  const fetchOrder = useCallback(async () => {
    try {
      setIsLoading(true)
      setError(null)
      const data = await orderService.getOrder(orderId)
      setOrder(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch order details')
    } finally {
      setIsLoading(false)
    }
  }, [orderId])

  useEffect(() => {
    fetchOrder()
  }, [fetchOrder])

  // Real-time WebSocket updates for this specific order
  useEffect(() => {
    const handleStatusUpdate = (payload) => {
      if (payload.order_id === orderId) {
        setOrder((prev) => (prev ? { ...prev, status: payload.status } : prev))
      }
    }

    const unsub = wsClient.on('ORDER_STATUS_UPDATED', handleStatusUpdate)
    return () => unsub()
  }, [orderId])

  const handlePayNow = async () => {
    if (!order) return
    setIsInitiatingPay(true)
    try {
      const paymentRes = await orderService.initiatePayment(order.id)
      setActivePayment(paymentRes)
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to initiate payment')
    } finally {
      setIsInitiatingPay(false)
    }
  }

  const handlePaymentSuccess = () => {
    setActivePayment(null)
    fetchOrder()
  }

  if (isLoading) {
    return (
      <div className="py-32 flex flex-col items-center justify-center space-y-3">
        <Loader2 className="w-8 h-8 animate-spin text-orange-600" />
        <p className="text-sm font-medium text-gray-500">Loading order #{orderId.slice(0, 8)}...</p>
      </div>
    )
  }

  if (error || !order) {
    return (
      <div className="max-w-3xl mx-auto px-4 py-12">
        <button
          onClick={onBack}
          className="inline-flex items-center text-xs font-bold text-gray-600 hover:text-gray-900 mb-6"
        >
          <ArrowLeft className="w-4 h-4 mr-1" /> Back to orders
        </button>
        <div className="p-6 bg-red-50 text-red-700 rounded-3xl border border-red-100 flex items-center space-x-3">
          <AlertCircle className="w-6 h-6 text-red-500 shrink-0" />
          <div>
            <p className="font-bold text-sm">Error Loading Order</p>
            <p className="text-xs">{error || 'Order not found'}</p>
          </div>
        </div>
      </div>
    )
  }

  // Order Stepper
  const steps = [
    { key: 'CONFIRMED', label: 'Confirmed', icon: CheckCircle2 },
    { key: 'PREPARING', label: 'In Kitchen', icon: ChefHat },
    { key: 'READY', label: 'Ready for Pickup', icon: PackageCheck },
    { key: 'COMPLETED', label: 'Completed', icon: CheckCircle2 },
  ]

  const statusIndexMap = {
    PENDING_PAYMENT: -1,
    CONFIRMED: 0,
    PREPARING: 1,
    READY: 2,
    COMPLETED: 3,
    CANCELLED: -2,
  }

  const currentStepIdx = statusIndexMap[order.status] ?? 0
  const isCancelled = order.status === 'CANCELLED'
  const isPendingPay = order.status === 'PENDING_PAYMENT'

  return (
    <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-10 space-y-8">
      {/* Top Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <button
            onClick={onBack}
            className="inline-flex items-center text-xs font-bold text-gray-500 hover:text-gray-900 mb-2 transition-colors"
          >
            <ArrowLeft className="w-4 h-4 mr-1" /> Back to all orders
          </button>
          <div className="flex items-center space-x-3">
            <h1 className="text-2xl sm:text-3xl font-black text-gray-900">
              Order #{order.id.slice(0, 8)}
            </h1>
            <span
              className={`px-3 py-1 rounded-full text-xs font-black uppercase tracking-wider ${
                order.status === 'READY'
                  ? 'bg-emerald-100 text-emerald-800 animate-pulse'
                  : isPendingPay
                  ? 'bg-amber-100 text-amber-800'
                  : isCancelled
                  ? 'bg-red-100 text-red-800'
                  : 'bg-orange-100 text-orange-800'
              }`}
            >
              {order.status.replace('_', ' ')}
            </span>
          </div>
          <p className="text-xs text-gray-400 mt-1">
            Placed on {new Date(order.created_at).toLocaleString()}
          </p>
        </div>

        {isPendingPay && (
          <button
            onClick={handlePayNow}
            disabled={isInitiatingPay}
            className="px-6 py-3 bg-emerald-600 hover:bg-emerald-700 text-white font-bold rounded-2xl shadow-lg shadow-emerald-200 transition-all flex items-center justify-center space-x-2 text-sm disabled:opacity-50"
          >
            {isInitiatingPay ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <>
                <CreditCard className="w-4 h-4" />
                <span>Pay ₹{order.total_amount.toFixed(2)}</span>
              </>
            )}
          </button>
        )}
      </div>

      {/* Live Stepper Tracker */}
      {!isCancelled && !isPendingPay && (
        <div className="bg-white p-6 sm:p-8 rounded-3xl border border-gray-100 shadow-sm space-y-6">
          <h3 className="text-xs font-bold uppercase tracking-wider text-gray-400">
            Live Kitchen Tracking
          </h3>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 relative">
            {steps.map((step, idx) => {
              const Icon = step.icon
              const isPast = idx < currentStepIdx
              const isCurrent = idx === currentStepIdx
              const isFuture = idx > currentStepIdx

              return (
                <div key={step.key} className="flex flex-col items-center text-center space-y-2">
                  <div
                    className={`w-12 h-12 rounded-2xl flex items-center justify-center transition-all ${
                      isCurrent
                        ? 'bg-orange-600 text-white shadow-lg shadow-orange-200 ring-4 ring-orange-100 scale-110'
                        : isPast
                        ? 'bg-emerald-600 text-white'
                        : 'bg-gray-100 text-gray-400'
                    }`}
                  >
                    <Icon className="w-6 h-6" />
                  </div>
                  <span
                    className={`text-xs font-bold ${
                      isCurrent
                        ? 'text-orange-600'
                        : isPast
                        ? 'text-emerald-700'
                        : 'text-gray-400'
                    }`}
                  >
                    {step.label}
                  </span>
                </div>
              )
            })}
          </div>

          {order.status === 'READY' && (
            <div className="p-4 bg-emerald-50 rounded-2xl border border-emerald-200 text-center space-y-1">
              <p className="font-bold text-emerald-900 text-sm">
                🎉 Your food is hot and ready at Counter #1!
              </p>
              <p className="text-xs text-emerald-700">
                Please show your Order #{order.id.slice(0, 8)} to the cafeteria staff for collection.
              </p>
            </div>
          )}
        </div>
      )}

      {isCancelled && (
        <div className="bg-red-50 p-6 rounded-3xl border border-red-100 flex items-center space-x-3 text-red-800">
          <Ban className="w-6 h-6 text-red-600 shrink-0" />
          <div>
            <p className="font-bold text-sm">Order Cancelled</p>
            <p className="text-xs text-red-600 mt-0.5">
              This order was cancelled and inventory reservation has been released back to stock.
            </p>
          </div>
        </div>
      )}

      {/* Itemized Breakdown */}
      <div className="bg-white p-6 sm:p-8 rounded-3xl border border-gray-100 shadow-sm space-y-6">
        <h3 className="text-base font-black text-gray-900">Ordered Items</h3>
        <div className="divide-y divide-gray-100">
          {order.items?.map((item) => (
            <div key={item.id} className="py-4 flex items-center justify-between">
              <div className="flex items-center space-x-3">
                <div className="w-10 h-10 rounded-xl bg-orange-50 text-orange-600 flex items-center justify-center font-bold text-xs">
                  {item.quantity}x
                </div>
                <div>
                  <h4 className="font-bold text-gray-900 text-sm">
                    {item.menu_item?.name || 'Cafeteria Item'}
                  </h4>
                  <p className="text-xs text-gray-400">
                    ₹{item.price_at_order.toFixed(2)} each
                  </p>
                  {item.special_instructions && (
                    <p className="text-[11px] text-orange-600 bg-orange-50 px-2 py-0.5 rounded-md mt-1 inline-block">
                      Note: {item.special_instructions}
                    </p>
                  )}
                </div>
              </div>
              <span className="font-black text-gray-900 text-sm">
                ₹{(item.price_at_order * item.quantity).toFixed(2)}
              </span>
            </div>
          ))}
        </div>

        {/* Totals */}
        <div className="pt-4 border-t border-gray-100 space-y-2 text-sm text-gray-600">
          <div className="flex justify-between font-black text-base text-gray-900 pt-2">
            <span>Total Amount</span>
            <span className="text-orange-600">₹{order.total_amount.toFixed(2)}</span>
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
