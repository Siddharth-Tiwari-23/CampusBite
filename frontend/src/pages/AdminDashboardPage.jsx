import React, { useState, useEffect, useCallback } from 'react'
import {
  Shield,
  UtensilsCrossed,
  Sparkles,
  TrendingUp,
  Clock,
  Plus,
  Edit2,
  Trash2,
  Loader2,
  AlertCircle,
  CheckCircle2,
  Send,
} from 'lucide-react'
import { orderService } from '../services/orderService'
import { menuService } from '../services/menuService'
import { analyticsService } from '../services/analyticsService'
import { wsClient } from '../services/websocket'

export function AdminDashboardPage() {
  const [activeTab, setActiveTab] = useState('orders')

  // Orders State
  const [orders, setOrders] = useState([])
  const [ordersLoading, setOrdersLoading] = useState(false)
  const [statusFilter, setStatusFilter] = useState('')
  const [updatingOrderId, setUpdatingOrderId] = useState(null)

  // Menu State
  const [menuItems, setMenuItems] = useState([])
  const [menuLoading, setMenuLoading] = useState(false)
  const [editingItem, setEditingItem] = useState(null)
  const [isNewItemModal, setIsNewItemModal] = useState(false)
  const [itemForm, setItemForm] = useState({
    name: '',
    description: '',
    price: 0,
    category: 'Fast Food',
    image_url: '',
    is_available: true,
    stock_quantity: 50,
  })

  // Analytics State
  const [analyticsQuestion, setAnalyticsQuestion] = useState('')
  const [analyticsResult, setAnalyticsResult] = useState(null)
  const [analyticsLoading, setAnalyticsLoading] = useState(false)
  const [analyticsError, setAnalyticsError] = useState(null)

  // Demand Trends State
  const [trendPeriod, setTrendPeriod] = useState('THIS_WEEK')
  const [trendData, setTrendData] = useState(null)
  const [trendsLoading, setTrendsLoading] = useState(false)

function getCategoryForItem(item) {
  if (item.category) return item.category
  const name = (item.name || '').toLowerCase()
  if (name.includes('momo')) return 'Momos & Quick Bites'
  if (name.includes('paratha')) return 'Parathas & Meals'
  if (name.includes('pizza') || name.includes('pasta')) return 'Pizza & Continental'
  if (name.includes('coffee') || name.includes('chai') || name.includes('tea') || name.includes('beverage') || name.includes('shake')) return 'Beverages'
  if (name.includes('samosa') || name.includes('fries') || name.includes('spring roll') || name.includes('snack')) return 'Snacks & Sides'
  if (name.includes('burger') || name.includes('sandwich')) return 'Burgers & Sandwiches'
  if (name.includes('roll') || name.includes('wrap')) return 'Rolls & Wraps'
  if (name.includes('dosa') || name.includes('idli') || name.includes('vada')) return 'South Indian'
  if (name.includes('biryani') || name.includes('chole') || name.includes('dal') || name.includes('rajma') || name.includes('meal')) return 'North Indian & Meals'
  return 'Cafeteria Specials'
}

  // 1. Fetch Orders
  const fetchOrders = useCallback(async () => {
    setOrdersLoading(true)
    try {
      const data = await orderService.listOrders()
      const list = Array.isArray(data) ? data : (data?.orders || [])
      setOrders(
        list.sort(
          (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
        )
      )
    } catch {
      // ignore
    } finally {
      setOrdersLoading(false)
    }
  }, [])

  // 2. Fetch Menu
  const fetchMenu = useCallback(async () => {
    setMenuLoading(true)
    try {
      const [menuData, invData] = await Promise.all([
        menuService.getMenu('?all=true'),
        menuService.getInventory('').catch(() => null),
      ])
      const rawList = Array.isArray(menuData) ? menuData : (menuData?.items || [])
      const invList = Array.isArray(invData) ? invData : (invData?.inventory || [])
      const invMap = new Map(invList.map((i) => [i.menu_item_id, i.quantity]))

      const merged = rawList.map((item) => ({
        ...item,
        category: getCategoryForItem(item),
        stock_quantity: invMap.has(item.id) ? invMap.get(item.id) : (item.stock_quantity ?? 50),
      }))
      setMenuItems(merged)
    } catch {
      // ignore
    } finally {
      setMenuLoading(false)
    }
  }, [])

  // 3. Fetch Demand Trends (Backend logic preserved)
  const fetchTrends = useCallback(async (period) => {
    setTrendsLoading(true)
    try {
      const data = await analyticsService.queryTrends(period)
      setTrendData(data)
    } catch {
      // ignore
    } finally {
      setTrendsLoading(false)
    }
  }, [])

  useEffect(() => {
    if (activeTab === 'orders') fetchOrders()
    if (activeTab === 'menu') fetchMenu()
    if (activeTab === 'trends') fetchTrends(trendPeriod)
  }, [activeTab, fetchOrders, fetchMenu, fetchTrends, trendPeriod])

  // Live order events
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

  const handleUpdateStatus = async (orderId, newStatus) => {
    setUpdatingOrderId(orderId)
    try {
      await orderService.updateOrderStatus(orderId, newStatus)
      setOrders((prev) =>
        prev.map((o) => (o.id === orderId ? { ...o, status: newStatus } : o))
      )
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to update order status')
    } finally {
      setUpdatingOrderId(null)
    }
  }

  const handleSaveMenuItem = async (e) => {
    e.preventDefault()
    try {
      if (editingItem) {
        await menuService.updateMenuItem(editingItem.id, {
          name: itemForm.name,
          description: itemForm.description,
          price: Number(itemForm.price),
          image_url: itemForm.image_url,
          is_available: itemForm.is_available,
        })
        await menuService.updateInventory(editingItem.id, {
          quantity: Number(itemForm.stock_quantity),
        })
      } else {
        await menuService.createMenuItem({
          name: itemForm.name,
          description: itemForm.description,
          price: Number(itemForm.price),
          image_url: itemForm.image_url,
          is_available: itemForm.is_available,
          initial_quantity: Number(itemForm.stock_quantity),
        })
      }

      setIsNewItemModal(false)
      setEditingItem(null)
      fetchMenu()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to save menu item')
    }
  }

  const handleDeleteMenuItem = async (id) => {
    if (!confirm('Are you sure you want to delete this menu item?')) return
    try {
      await menuService.deleteMenuItem(id)
      fetchMenu()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete item')
    }
  }

  const handleAskAnalytics = async (e) => {
    e?.preventDefault()
    if (!analyticsQuestion.trim()) return

    setAnalyticsLoading(true)
    setAnalyticsError(null)
    try {
      const res = await analyticsService.queryAnalytics(analyticsQuestion)
      setAnalyticsResult(res)
    } catch (err) {
      setAnalyticsError(err instanceof Error ? err.message : 'Failed to execute query')
    } finally {
      setAnalyticsLoading(false)
    }
  }

  const filteredOrders = orders.filter((o) =>
    statusFilter ? o.status === statusFilter : true
  )

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-10 space-y-8">
      {/* Top Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <div className="flex items-center space-x-2 text-purple-700 text-xs font-bold uppercase tracking-wider">
            <Shield className="w-4 h-4" />
            <span>Cafeteria Administration</span>
          </div>
          <h1 className="text-3xl font-black text-gray-900 mt-1">Management Hub</h1>
          <p className="text-xs text-gray-500">
            Control live kitchen queues, menu stock, and AI-driven business intelligence
          </p>
        </div>

        {/* Tab Controls */}
        <div className="flex items-center bg-gray-100 p-1 rounded-2xl">
          <button
            onClick={() => setActiveTab('orders')}
            className={`px-4 py-2 rounded-xl text-xs font-bold transition-all flex items-center space-x-1.5 ${
              activeTab === 'orders'
                ? 'bg-white text-gray-900 shadow-sm'
                : 'text-gray-600 hover:text-gray-900'
            }`}
          >
            <Clock className="w-3.5 h-3.5" />
            <span>Live Queue</span>
          </button>
          <button
            onClick={() => setActiveTab('menu')}
            className={`px-4 py-2 rounded-xl text-xs font-bold transition-all flex items-center space-x-1.5 ${
              activeTab === 'menu'
                ? 'bg-white text-gray-900 shadow-sm'
                : 'text-gray-600 hover:text-gray-900'
            }`}
          >
            <UtensilsCrossed className="w-3.5 h-3.5" />
            <span>Menu & Stock</span>
          </button>
          <button
            onClick={() => setActiveTab('analytics')}
            className={`px-4 py-2 rounded-xl text-xs font-bold transition-all flex items-center space-x-1.5 ${
              activeTab === 'analytics'
                ? 'bg-white text-gray-900 shadow-sm'
                : 'text-gray-600 hover:text-gray-900'
            }`}
          >
            <Sparkles className="w-3.5 h-3.5 text-purple-600" />
            <span>Gemini Analytics</span>
          </button>
        </div>
      </div>

      {/* 1. ORDERS QUEUE TAB */}
      {activeTab === 'orders' && (
        <div className="space-y-6 animate-in fade-in">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex items-center space-x-2">
              <span className="text-xs font-bold text-gray-500 uppercase">Filter Status:</span>
              <select
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value)}
                className="bg-white border border-gray-200 text-xs font-bold rounded-xl px-3 py-2 focus:outline-none focus:ring-2 focus:ring-orange-500"
              >
                <option value="">All Statuses</option>
                <option value="PENDING_PAYMENT">Pending Payment</option>
                <option value="CONFIRMED">Confirmed</option>
                <option value="PREPARING">Preparing</option>
                <option value="READY">Ready</option>
                <option value="COMPLETED">Completed</option>
                <option value="CANCELLED">Cancelled</option>
              </select>
            </div>
            <span className="text-xs font-semibold text-gray-400">
              Showing {filteredOrders.length} order(s)
            </span>
          </div>

          {ordersLoading ? (
            <div className="py-24 flex flex-col items-center justify-center space-y-3">
              <Loader2 className="w-8 h-8 animate-spin text-orange-600" />
              <p className="text-sm font-medium text-gray-500">Loading live orders...</p>
            </div>
          ) : filteredOrders.length === 0 ? (
            <div className="py-20 text-center bg-white rounded-3xl border border-gray-100 p-8">
              <Clock className="w-12 h-12 text-gray-300 mx-auto mb-2" />
              <p className="text-sm font-bold text-gray-700">No orders in this queue</p>
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
              {filteredOrders.map((order) => {
                const isUpdating = updatingOrderId === order.id

                return (
                  <div
                    key={order.id}
                    className="bg-white p-6 rounded-3xl border border-gray-100 shadow-sm space-y-4 flex flex-col justify-between"
                  >
                    <div className="space-y-3">
                      <div className="flex items-start justify-between">
                        <div>
                          <span className="font-mono text-xs font-bold text-gray-400">
                            #{order.id.slice(0, 8)}
                          </span>
                          <h4 className="font-bold text-gray-900 text-sm mt-0.5">
                            ₹{order.total_amount.toFixed(2)}
                          </h4>
                        </div>
                        <span
                          className={`px-2.5 py-1 rounded-full text-[10px] font-black uppercase tracking-wider ${
                            order.status === 'READY'
                              ? 'bg-emerald-100 text-emerald-800 animate-pulse'
                              : order.status === 'PREPARING'
                              ? 'bg-orange-100 text-orange-800'
                              : order.status === 'CONFIRMED'
                              ? 'bg-blue-100 text-blue-800'
                              : 'bg-gray-100 text-gray-700'
                          }`}
                        >
                          {order.status.replace('_', ' ')}
                        </span>
                      </div>

                      <div className="bg-gray-50 p-3 rounded-2xl space-y-1.5 text-xs text-gray-600">
                        {order.items?.map((it) => (
                          <div key={it.id} className="flex justify-between">
                            <span>
                              {it.quantity}x {it.menu_item?.name || 'Item'}
                            </span>
                            <span className="font-semibold text-gray-900">
                              ₹{(it.price_at_order * it.quantity).toFixed(2)}
                            </span>
                          </div>
                        ))}
                      </div>

                      {order.special_instructions && (
                        <p className="text-[11px] text-amber-700 bg-amber-50 p-2 rounded-xl border border-amber-100">
                          <strong>Note:</strong> {order.special_instructions}
                        </p>
                      )}
                    </div>

                    {/* Status Advance Action Buttons */}
                    <div className="pt-2 border-t border-gray-100 flex items-center justify-between gap-2">
                      {order.status === 'CONFIRMED' && (
                        <button
                          onClick={() => handleUpdateStatus(order.id, 'PREPARING')}
                          disabled={isUpdating}
                          className="w-full py-2 bg-orange-600 hover:bg-orange-700 text-white rounded-xl text-xs font-bold transition-all disabled:opacity-50"
                        >
                          Start Preparing
                        </button>
                      )}
                      {order.status === 'PREPARING' && (
                        <button
                          onClick={() => handleUpdateStatus(order.id, 'READY')}
                          disabled={isUpdating}
                          className="w-full py-2 bg-emerald-600 hover:bg-emerald-700 text-white rounded-xl text-xs font-bold transition-all disabled:opacity-50"
                        >
                          Mark Ready
                        </button>
                      )}
                      {order.status === 'READY' && (
                        <button
                          onClick={() => handleUpdateStatus(order.id, 'COMPLETED')}
                          disabled={isUpdating}
                          className="w-full py-2 bg-gray-900 hover:bg-black text-white rounded-xl text-xs font-bold transition-all disabled:opacity-50"
                        >
                          Complete Order
                        </button>
                      )}
                      {order.status === 'COMPLETED' && (
                        <span className="text-xs text-gray-400 font-bold flex items-center mx-auto">
                          <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600 mr-1" /> Completed
                        </span>
                      )}
                      {order.status === 'PENDING_PAYMENT' && (
                        <button
                          onClick={() => handleUpdateStatus(order.id, 'CANCELLED')}
                          disabled={isUpdating}
                          className="w-full py-2 bg-red-50 hover:bg-red-100 text-red-700 rounded-xl text-xs font-bold transition-all disabled:opacity-50"
                        >
                          Cancel Order
                        </button>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}

      {/* 2. MENU & STOCK MANAGEMENT TAB */}
      {activeTab === 'menu' && (
        <div className="space-y-6 animate-in fade-in">
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-black text-gray-900">Cafeteria Items Catalog</h3>
            <button
              onClick={() => {
                setEditingItem(null)
                setItemForm({
                  name: '',
                  description: '',
                  price: 50,
                  category: 'Fast Food',
                  image_url: '',
                  is_available: true,
                  stock_quantity: 50,
                })
                setIsNewItemModal(true)
              }}
              className="px-4 py-2.5 bg-orange-600 hover:bg-orange-700 text-white rounded-xl text-xs font-bold flex items-center space-x-1.5 shadow-md shadow-orange-200"
            >
              <Plus className="w-4 h-4" />
              <span>Add New Item</span>
            </button>
          </div>

          {menuLoading ? (
            <div className="py-24 flex flex-col items-center justify-center space-y-3">
              <Loader2 className="w-8 h-8 animate-spin text-orange-600" />
              <p className="text-sm font-medium text-gray-500">Loading catalog...</p>
            </div>
          ) : (
            <div className="bg-white rounded-3xl border border-gray-100 shadow-sm overflow-hidden">
              <table className="w-full text-left border-collapse">
                <thead>
                  <tr className="bg-gray-50 border-b border-gray-100 text-[11px] font-black uppercase tracking-wider text-gray-500">
                    <th className="p-4 pl-6">Item</th>
                    <th className="p-4">Category</th>
                    <th className="p-4">Price</th>
                    <th className="p-4">Inventory Stock</th>
                    <th className="p-4">Status</th>
                    <th className="p-4 pr-6 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100 text-xs">
                  {menuItems.map((item) => (
                    <tr key={item.id} className="hover:bg-gray-50/50 transition-colors">
                      <td className="p-4 pl-6 font-bold text-gray-900 flex items-center space-x-3">
                        {item.image_url ? (
                          <img
                            src={item.image_url}
                            alt=""
                            className="w-10 h-10 rounded-xl object-cover"
                          />
                        ) : (
                          <div className="w-10 h-10 rounded-xl bg-orange-50 text-orange-600 flex items-center justify-center">
                            <UtensilsCrossed className="w-5 h-5" />
                          </div>
                        )}
                        <div>
                          <p>{item.name}</p>
                          <p className="text-[10px] text-gray-400 font-normal line-clamp-1">
                            {item.description}
                          </p>
                        </div>
                      </td>
                      <td className="p-4 text-gray-600 font-medium">{item.category}</td>
                      <td className="p-4 font-black text-gray-900">₹{item.price.toFixed(2)}</td>
                      <td className="p-4">
                        <span className="font-bold text-gray-800">
                          {item.stock_quantity ?? 'N/A'}
                        </span>{' '}
                        units
                      </td>
                      <td className="p-4">
                        <span
                          className={`px-2 py-0.5 rounded-full text-[10px] font-bold ${
                            item.is_available
                              ? 'bg-emerald-50 text-emerald-700'
                              : 'bg-red-50 text-red-700'
                          }`}
                        >
                          {item.is_available ? 'Available' : 'Unavailable'}
                        </span>
                      </td>
                      <td className="p-4 pr-6 text-right space-x-2">
                        <button
                          onClick={() => {
                            setEditingItem(item)
                            setItemForm({
                              name: item.name,
                              description: item.description || '',
                              price: item.price,
                              category: item.category || 'General',
                              image_url: item.image_url || '',
                              is_available: item.is_available,
                              stock_quantity: item.stock_quantity ?? 50,
                            })
                            setIsNewItemModal(true)
                          }}
                          className="p-1.5 text-gray-500 hover:text-orange-600 hover:bg-orange-50 rounded-lg transition-colors"
                        >
                          <Edit2 className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => handleDeleteMenuItem(item.id)}
                          className="p-1.5 text-gray-500 hover:text-red-600 hover:bg-red-50 rounded-lg transition-colors"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* Modal for Add / Edit Item */}
          {isNewItemModal && (
            <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4 animate-in fade-in">
              <div className="bg-white w-full max-w-lg rounded-3xl p-6 sm:p-8 shadow-2xl border border-gray-100 space-y-6">
                <div className="flex justify-between items-center pb-4 border-b border-gray-100">
                  <h3 className="text-lg font-black text-gray-900">
                    {editingItem ? 'Edit Menu Item' : 'Create New Menu Item'}
                  </h3>
                  <button
                    onClick={() => setIsNewItemModal(false)}
                    className="text-gray-400 hover:text-gray-600 text-xs font-bold"
                  >
                    Cancel
                  </button>
                </div>

                <form onSubmit={handleSaveMenuItem} className="space-y-4 text-xs">
                  <div>
                    <label className="block font-bold uppercase text-gray-600 mb-1">Item Name</label>
                    <input
                      type="text"
                      required
                      value={itemForm.name}
                      onChange={(e) => setItemForm({ ...itemForm, name: e.target.value })}
                      className="w-full px-3.5 py-2.5 rounded-xl border border-gray-200 focus:outline-none focus:ring-2 focus:ring-orange-500"
                    />
                  </div>

                  <div>
                    <label className="block font-bold uppercase text-gray-600 mb-1">Description</label>
                    <textarea
                      rows={2}
                      value={itemForm.description}
                      onChange={(e) => setItemForm({ ...itemForm, description: e.target.value })}
                      className="w-full px-3.5 py-2.5 rounded-xl border border-gray-200 focus:outline-none focus:ring-2 focus:ring-orange-500"
                    />
                  </div>

                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="block font-bold uppercase text-gray-600 mb-1">Price (₹)</label>
                      <input
                        type="number"
                        step="0.5"
                        required
                        value={itemForm.price}
                        onChange={(e) =>
                          setItemForm({ ...itemForm, price: parseFloat(e.target.value) || 0 })
                        }
                        className="w-full px-3.5 py-2.5 rounded-xl border border-gray-200 focus:outline-none focus:ring-2 focus:ring-orange-500"
                      />
                    </div>
                    <div>
                      <label className="block font-bold uppercase text-gray-600 mb-1">Category</label>
                      <input
                        type="text"
                        required
                        value={itemForm.category}
                        onChange={(e) => setItemForm({ ...itemForm, category: e.target.value })}
                        className="w-full px-3.5 py-2.5 rounded-xl border border-gray-200 focus:outline-none focus:ring-2 focus:ring-orange-500"
                      />
                    </div>
                  </div>

                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="block font-bold uppercase text-gray-600 mb-1">
                        Stock Quantity
                      </label>
                      <input
                        type="number"
                        required
                        value={itemForm.stock_quantity}
                        onChange={(e) =>
                          setItemForm({
                            ...itemForm,
                            stock_quantity: parseInt(e.target.value, 10) || 0,
                          })
                        }
                        className="w-full px-3.5 py-2.5 rounded-xl border border-gray-200 focus:outline-none focus:ring-2 focus:ring-orange-500"
                      />
                    </div>
                    <div>
                      <label className="block font-bold uppercase text-gray-600 mb-1">Image URL</label>
                      <input
                        type="text"
                        value={itemForm.image_url}
                        onChange={(e) => setItemForm({ ...itemForm, image_url: e.target.value })}
                        className="w-full px-3.5 py-2.5 rounded-xl border border-gray-200 focus:outline-none focus:ring-2 focus:ring-orange-500"
                      />
                    </div>
                  </div>

                  <div className="flex items-center space-x-2 pt-2">
                    <input
                      type="checkbox"
                      id="is_available"
                      checked={itemForm.is_available}
                      onChange={(e) =>
                        setItemForm({ ...itemForm, is_available: e.target.checked })
                      }
                      className="rounded text-orange-600 focus:ring-orange-500"
                    />
                    <label htmlFor="is_available" className="font-bold text-gray-700">
                      Available for purchase
                    </label>
                  </div>

                  <div className="pt-4 flex justify-end space-x-2">
                    <button
                      type="button"
                      onClick={() => setIsNewItemModal(false)}
                      className="px-4 py-2 text-gray-600 hover:bg-gray-100 rounded-xl font-bold"
                    >
                      Cancel
                    </button>
                    <button
                      type="submit"
                      className="px-6 py-2 bg-orange-600 hover:bg-orange-700 text-white rounded-xl font-bold shadow-md shadow-orange-200"
                    >
                      Save Item
                    </button>
                  </div>
                </form>
              </div>
            </div>
          )}
        </div>
      )}

      {/* 3. GEMINI NATURAL LANGUAGE ANALYTICS TAB */}
      {activeTab === 'analytics' && (
        <div className="space-y-6 animate-in fade-in">
          <div className="bg-gradient-to-r from-purple-900 to-indigo-900 rounded-3xl p-8 text-white space-y-6 shadow-xl">
            <div className="space-y-2">
              <span className="inline-flex items-center px-3 py-1 rounded-full text-xs font-bold bg-white/10 text-purple-200">
                <Sparkles className="w-3.5 h-3.5 mr-1 text-purple-300" /> Safe Backend Query Routing
              </span>
              <h2 className="text-2xl sm:text-3xl font-black">
                Natural-Language Business Analytics
              </h2>
              <p className="text-xs text-purple-200 max-w-2xl leading-relaxed">
                Ask questions about sales, revenue, top dishes, and orders in plain English. Gemini
                translates questions safely into predefined parameterized backend queries without
                direct database access.
              </p>
            </div>

            {/* Input form */}
            <form onSubmit={handleAskAnalytics} className="flex gap-2">
              <input
                type="text"
                value={analyticsQuestion}
                onChange={(e) => setAnalyticsQuestion(e.target.value)}
                placeholder="e.g. What were our top 5 selling items this week?"
                className="flex-1 px-5 py-3.5 rounded-2xl bg-white/10 border border-white/20 text-white placeholder-purple-300 text-sm focus:outline-none focus:ring-2 focus:ring-purple-400 backdrop-blur-md"
              />
              <button
                type="submit"
                disabled={analyticsLoading || !analyticsQuestion.trim()}
                className="px-6 py-3.5 bg-white text-purple-900 hover:bg-purple-50 font-black rounded-2xl shadow-lg transition-all flex items-center space-x-2 text-sm disabled:opacity-50"
              >
                {analyticsLoading ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <>
                    <span>Ask AI</span>
                    <Send className="w-4 h-4" />
                  </>
                )}
              </button>
            </form>

            {/* Quick Sample Prompts */}
            <div className="flex flex-wrap gap-2 text-xs">
              <span className="text-purple-300 font-bold self-center">Try asking:</span>
              {[
                'Top 5 selling items this week',
                'How much revenue did we generate yesterday?',
                'How many orders today?',
                'Which category sold the most?',
              ].map((q) => (
                <button
                  key={q}
                  type="button"
                  onClick={() => {
                    setAnalyticsQuestion(q)
                  }}
                  className="px-3 py-1.5 bg-white/10 hover:bg-white/20 rounded-xl text-purple-200 transition-colors"
                >
                  "{q}"
                </button>
              ))}
            </div>
          </div>

          {/* Results Output */}
          {analyticsError && (
            <div className="p-6 bg-red-50 text-red-700 rounded-3xl border border-red-100 flex items-center space-x-3">
              <AlertCircle className="w-6 h-6 text-red-500 shrink-0" />
              <div>
                <p className="font-bold text-sm">Analytics Query Notice</p>
                <p className="text-xs">{analyticsError}</p>
              </div>
            </div>
          )}

          {analyticsResult && (
            <div className="bg-white rounded-3xl border border-gray-100 p-6 sm:p-8 shadow-sm space-y-6 animate-in slide-in-from-bottom-2">
              <div className="flex items-start justify-between border-b border-gray-100 pb-4">
                <div>
                  <span className="text-[11px] font-bold text-purple-600 uppercase tracking-wider">
                    Query Operation: {analyticsResult.operation}
                  </span>
                  <h3 className="text-xl font-black text-gray-900 mt-1">
                    "{analyticsResult.question}"
                  </h3>
                </div>
                <span className="text-xs bg-purple-50 text-purple-700 px-3 py-1 rounded-full font-bold">
                  Gemini Verified
                </span>
              </div>

              {/* Natural Language Answer */}
              {analyticsResult.answer && (
                <div className="p-4 bg-purple-50/50 rounded-2xl border border-purple-100 text-sm text-purple-950 leading-relaxed font-medium">
                  {analyticsResult.answer}
                </div>
              )}

              {/* Data Table */}
              {Array.isArray(analyticsResult.data) && analyticsResult.data.length > 0 && (
                <div className="border border-gray-100 rounded-2xl overflow-hidden">
                  <table className="w-full text-left text-xs">
                    <thead className="bg-gray-50 border-b border-gray-100 font-bold uppercase text-gray-500">
                      <tr>
                        {Object.keys(analyticsResult.data[0]).map((col) => (
                          <th key={col} className="p-3">
                            {col.replace('_', ' ')}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-gray-100">
                      {analyticsResult.data.map((row, idx) => (
                        <tr key={idx} className="hover:bg-gray-50/50">
                          {Object.values(row).map((val, colIdx) => (
                            <td key={colIdx} className="p-3 font-semibold text-gray-800">
                              {typeof val === 'number'
                                ? val.toLocaleString()
                                : String(val ?? '')}
                            </td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* 4. HISTORICAL DEMAND INSIGHTS TAB (NON-ML) */}
      {activeTab === 'trends' && (
        <div className="space-y-6 animate-in fade-in">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
            <div>
              <h3 className="text-lg font-black text-gray-900">Period-over-Period Demand Insights</h3>
              <p className="text-xs text-gray-500">
                Lightweight statistical comparison against previous cafeteria operating periods
              </p>
            </div>

            <div className="flex items-center space-x-2">
              <span className="text-xs font-bold text-gray-400 uppercase">Period:</span>
              <div className="bg-gray-100 p-1 rounded-xl flex space-x-1">
                {['TODAY', 'THIS_WEEK', 'THIS_MONTH'].map((p) => (
                  <button
                    key={p}
                    onClick={() => setTrendPeriod(p)}
                    className={`px-3 py-1.5 rounded-lg text-xs font-bold transition-colors ${
                      trendPeriod === p
                        ? 'bg-white text-gray-900 shadow-sm'
                        : 'text-gray-600 hover:text-gray-900'
                    }`}
                  >
                    {p.replace('_', ' ')}
                  </button>
                ))}
              </div>
            </div>
          </div>

          {trendsLoading ? (
            <div className="py-24 flex flex-col items-center justify-center space-y-3">
              <Loader2 className="w-8 h-8 animate-spin text-emerald-600" />
              <p className="text-sm font-medium text-gray-500">Calculating period trends...</p>
            </div>
          ) : !trendData ? (
            <div className="py-20 text-center bg-white rounded-3xl border border-gray-100 p-8">
              <TrendingUp className="w-12 h-12 text-gray-300 mx-auto mb-2" />
              <p className="text-sm font-bold text-gray-700">No trend data available</p>
            </div>
          ) : (
            <div className="space-y-6">
              {/* Summary Explanation */}
              {trendData.summary && (
                <div className="p-5 bg-emerald-50 rounded-3xl border border-emerald-100 text-xs sm:text-sm text-emerald-950 font-medium leading-relaxed">
                  <div className="flex items-center space-x-2 text-emerald-800 font-bold uppercase tracking-wider text-[11px] mb-2">
                    <Sparkles className="w-4 h-4" />
                    <span>Demand Analysis Summary</span>
                  </div>
                  {trendData.summary}
                </div>
              )}

              {/* Items Table */}
              <div className="bg-white rounded-3xl border border-gray-100 shadow-sm overflow-hidden">
                <table className="w-full text-left border-collapse">
                  <thead>
                    <tr className="bg-gray-50 border-b border-gray-100 text-[11px] font-black uppercase tracking-wider text-gray-500">
                      <th className="p-4 pl-6">Menu Item</th>
                      <th className="p-4">Current Units</th>
                      <th className="p-4">Previous Units</th>
                      <th className="p-4">Change</th>
                      <th className="p-4 pr-6">Trend Direction</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-100 text-xs">
                    {trendData.items?.map((item) => {
                      const isUp = item.trend_direction === 'UP'
                      const isDown = item.trend_direction === 'DOWN'

                      return (
                        <tr key={item.item_id} className="hover:bg-gray-50/50">
                          <td className="p-4 pl-6 font-bold text-gray-900">{item.item_name}</td>
                          <td className="p-4 font-black">{item.current_quantity}</td>
                          <td className="p-4 text-gray-500">{item.previous_quantity}</td>
                          <td className="p-4 font-bold">
                            {item.percentage_change > 0 ? '+' : ''}
                            {item.percentage_change.toFixed(1)}%
                          </td>
                          <td className="p-4 pr-6">
                            <span
                              className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-[10px] font-bold ${
                                isUp
                                  ? 'bg-emerald-100 text-emerald-800'
                                  : isDown
                                  ? 'bg-red-100 text-red-800'
                                  : 'bg-gray-100 text-gray-700'
                              }`}
                            >
                              {item.trend_direction}
                            </span>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
