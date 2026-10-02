import React, { useState, useEffect, useCallback } from 'react'
import {
  Clock,
  ChevronRight,
  Loader2,
  AlertCircle,
  RefreshCw,
  ShoppingBag,
} from 'lucide-react'
import { orderService } from '../services/orderService'
import { wsClient } from '../services/websocket'

export function OrderHistoryPage({ onSelectOrder, onNavigateToMenu }) {
  const [orders, setOrders] = useState([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState(null)

  const fetchOrders = useCallback(async () => {
    try {
      setIsLoading(true)
      setError(null)
      const data = await orderService.listOrders()
      const list = Array.isArray(data) ? data : (data?.orders || [])
      setOrders(
        list.sort(
          (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
        )
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load orders')
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchOrders()
  }, [fetchOrders])

  // Live order updates
  useEffect(() => {
    const handleStatusUpdate = (payload) => {
      setOrders((prev) =>
        prev.map((o) =>
          o.id === payload.order_id ? { ...o, status: payload.status } : o
        )
      )
    }

    const unsub = wsClient.on('ORDER_STATUS_UPDATED', handleStatusUpdate)
    return () => unsub()
  }, [])

  const getStatusBadge = (status) => {
    switch (status) {
      case 'PENDING_PAYMENT':
        return 'bg-amber-50 text-amber-700 border-amber-200'
      case 'CONFIRMED':
        return 'bg-blue-50 text-blue-700 border-blue-200'
      case 'PREPARING':
        return 'bg-orange-50 text-orange-700 border-orange-200'
      case 'READY':
        return 'bg-emerald-50 text-emerald-700 border-emerald-200 font-bold'
      case 'COMPLETED':
        return 'bg-gray-100 text-gray-700 border-gray-200'
      case 'CANCELLED':
        return 'bg-red-50 text-red-700 border-red-200'
      default:
        return 'bg-gray-50 text-gray-700 border-gray-200'
    }
  }

  return (
    <div className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8 py-10 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl sm:text-3xl font-black text-gray-900">Your Order History</h1>
          <p className="text-xs text-gray-500 mt-1">
            Track live cafeteria kitchen queue status
          </p>
        </div>
        <button
          onClick={fetchOrders}
          className="p-2.5 bg-white text-gray-600 hover:text-gray-900 rounded-xl border border-gray-200 shadow-sm flex items-center space-x-1 text-xs font-semibold"
        >
          <RefreshCw className="w-3.5 h-3.5" />
          <span>Refresh</span>
        </button>
      </div>

      {isLoading ? (
        <div className="py-24 flex flex-col items-center justify-center space-y-3">
          <Loader2 className="w-8 h-8 animate-spin text-orange-600" />
          <p className="text-sm font-medium text-gray-500">Fetching order history...</p>
        </div>
      ) : error ? (
        <div className="p-6 bg-red-50 text-red-700 rounded-2xl border border-red-100 flex items-center space-x-3">
          <AlertCircle className="w-6 h-6 text-red-500 shrink-0" />
          <div>
            <p className="font-bold text-sm">Failed to load order history</p>
            <p className="text-xs text-red-600">{error}</p>
          </div>
        </div>
      ) : orders.length === 0 ? (
        <div className="py-20 text-center bg-white rounded-3xl border border-gray-100 p-8 shadow-sm space-y-4">
          <ShoppingBag className="w-12 h-12 text-gray-300 mx-auto" />
          <div>
            <h3 className="text-lg font-bold text-gray-800">No past orders yet</h3>
            <p className="text-xs text-gray-400 mt-1">
              Your placed orders and cafeteria pickups will appear here.
            </p>
          </div>
          <button
            onClick={onNavigateToMenu}
            className="px-5 py-2.5 bg-orange-600 hover:bg-orange-700 text-white rounded-xl text-xs font-bold shadow-md shadow-orange-200"
          >
            Start an Order
          </button>
        </div>
      ) : (
        <div className="space-y-4">
          {orders.map((order) => (
            <div
              key={order.id}
              onClick={() => onSelectOrder(order.id)}
              className="group bg-white p-6 rounded-3xl border border-gray-100 shadow-sm hover:shadow-md transition-all cursor-pointer flex flex-col sm:flex-row sm:items-center justify-between gap-4"
            >
              <div className="space-y-2">
                <div className="flex items-center space-x-3">
                  <span className="font-mono text-xs font-bold text-gray-500">
                    #{order.id.slice(0, 8)}
                  </span>
                  <span
                    className={`text-xs px-2.5 py-0.5 rounded-full border font-bold ${getStatusBadge(
                      order.status
                    )}`}
                  >
                    {order.status.replace('_', ' ')}
                  </span>
                  {order.status === 'READY' && (
                    <span className="animate-pulse bg-emerald-500 text-white text-[10px] px-2 py-0.5 rounded-full font-bold">
                      Ready for Pickup!
                    </span>
                  )}
                </div>

                <div className="flex items-center space-x-4 text-xs text-gray-400">
                  <span className="flex items-center">
                    <Clock className="w-3.5 h-3.5 mr-1" />
                    {new Date(order.created_at).toLocaleString()}
                  </span>
                  <span>•</span>
                  <span>{order.items?.length || 0} item(s)</span>
                </div>
              </div>

              <div className="flex items-center justify-between sm:justify-end space-x-6">
                <div className="text-right">
                  <span className="text-xs text-gray-400">Total</span>
                  <p className="text-lg font-black text-gray-900">
                    ₹{order.total_amount ? order.total_amount.toFixed(2) : '0.00'}
                  </p>
                </div>
                <div className="w-8 h-8 rounded-full bg-gray-50 group-hover:bg-orange-50 group-hover:text-orange-600 flex items-center justify-center text-gray-400 transition-colors">
                  <ChevronRight className="w-4 h-4" />
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
