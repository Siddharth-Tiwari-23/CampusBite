import React, { useState } from 'react'
import {
  ShoppingBag,
  Bell,
  LogOut,
  UtensilsCrossed,
  Clock,
  Shield,
  Wifi,
  WifiOff,
  CheckCircle,
} from 'lucide-react'
import { useAuth } from '../context/AuthContext'
import { useCart } from '../context/CartContext'
import { useNotifications } from '../context/NotificationContext'
import { useWebSocket } from '../context/WebSocketContext'

export function Navbar({ activeTab, onSelectTab }) {
  const { user, isAuthenticated, isAdmin, logout } = useAuth()
  const { totalItemCount } = useCart()
  const { notifications, unreadCount, markAsRead, markAllAsRead } = useNotifications()
  const { isConnected, isConnecting } = useWebSocket()
  const [showNotifications, setShowNotifications] = useState(false)

  return (
    <header className="sticky top-0 z-40 bg-white/95 backdrop-blur border-b border-gray-200 shadow-sm">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex items-center justify-between h-16">
          {/* Brand Logo */}
          <div
            className="flex items-center space-x-3 cursor-pointer"
            onClick={() => onSelectTab('menu')}
          >
            <div className="w-10 h-10 rounded-xl bg-orange-600 flex items-center justify-center text-white shadow-md shadow-orange-200">
              <UtensilsCrossed className="w-6 h-6" />
            </div>
            <div>
              <span className="text-xl font-black bg-gradient-to-r from-orange-600 to-amber-600 bg-clip-text text-transparent">
                CampusBite
              </span>
              <div className="flex items-center space-x-1.5 text-xs text-gray-500 font-medium">
                <span>Cafeteria Ordering</span>
                <span className="inline-block w-1 h-1 rounded-full bg-gray-300"></span>
                {isConnected ? (
                  <span className="flex items-center text-emerald-600">
                    <Wifi className="w-3 h-3 mr-0.5" /> Live
                  </span>
                ) : isConnecting ? (
                  <span className="flex items-center text-amber-500">
                    <Wifi className="w-3 h-3 mr-0.5 animate-pulse" /> Connecting...
                  </span>
                ) : (
                  <span className="flex items-center text-gray-400">
                    <WifiOff className="w-3 h-3 mr-0.5" /> Offline
                  </span>
                )}
              </div>
            </div>
          </div>

          {/* Navigation Links */}
          {isAuthenticated && (
            <nav className="hidden md:flex items-center space-x-1">
              <button
                onClick={() => onSelectTab('menu')}
                className={`px-3 py-2 rounded-lg text-sm font-medium transition-colors ${
                  activeTab === 'menu'
                    ? 'bg-orange-50 text-orange-700'
                    : 'text-gray-700 hover:bg-gray-100'
                }`}
              >
                Menu Catalog
              </button>
              <button
                onClick={() => onSelectTab('orders')}
                className={`px-3 py-2 rounded-lg text-sm font-medium transition-colors flex items-center space-x-1 ${
                  activeTab === 'orders'
                    ? 'bg-orange-50 text-orange-700'
                    : 'text-gray-700 hover:bg-gray-100'
                }`}
              >
                <Clock className="w-4 h-4 mr-1" />
                My Orders
              </button>
              {isAdmin && (
                <button
                  onClick={() => onSelectTab('admin')}
                  className={`px-3 py-2 rounded-lg text-sm font-medium transition-colors flex items-center space-x-1 ${
                    activeTab === 'admin'
                      ? 'bg-orange-50 text-orange-700'
                      : 'text-gray-700 hover:bg-gray-100'
                  }`}
                >
                  <Shield className="w-4 h-4 mr-1 text-orange-600" />
                  Admin Dashboard
                </button>
              )}
            </nav>
          )}

          {/* Right Actions */}
          <div className="flex items-center space-x-3">
            {isAuthenticated ? (
              <>
                {/* Cart Button */}
                <button
                  onClick={() => onSelectTab('cart')}
                  className={`relative p-2 rounded-lg text-gray-700 hover:bg-gray-100 transition-colors ${
                    activeTab === 'cart' ? 'bg-orange-50 text-orange-600' : ''
                  }`}
                  title="Your Tray"
                >
                  <ShoppingBag className="w-5 h-5" />
                  {totalItemCount > 0 && (
                    <span className="absolute -top-1 -right-1 bg-orange-600 text-white text-xs font-bold w-5 h-5 rounded-full flex items-center justify-center animate-bounce">
                      {totalItemCount}
                    </span>
                  )}
                </button>

                {/* Notifications Dropdown */}
                <div className="relative">
                  <button
                    onClick={() => setShowNotifications(!showNotifications)}
                    className="relative p-2 rounded-lg text-gray-700 hover:bg-gray-100 transition-colors"
                    title="Notifications"
                  >
                    <Bell className="w-5 h-5" />
                    {unreadCount > 0 && (
                      <span className="absolute -top-1 -right-1 bg-red-500 text-white text-xs font-bold w-5 h-5 rounded-full flex items-center justify-center">
                        {unreadCount}
                      </span>
                    )}
                  </button>

                  {/* Dropdown Card */}
                  {showNotifications && (
                    <div className="absolute right-0 mt-2 w-80 sm:w-96 bg-white rounded-2xl shadow-xl border border-gray-100 py-2 z-50 animate-in fade-in slide-in-from-top-2">
                      <div className="px-4 py-2 border-b border-gray-100 flex items-center justify-between">
                        <div className="flex items-center space-x-2">
                          <span className="font-semibold text-gray-800 text-sm">Notifications</span>
                          {unreadCount > 0 && (
                            <span className="bg-orange-100 text-orange-700 text-xs px-2 py-0.5 rounded-full font-medium">
                              {unreadCount} new
                            </span>
                          )}
                        </div>
                        {unreadCount > 0 && (
                          <button
                            onClick={() => markAllAsRead()}
                            className="text-xs text-orange-600 hover:underline flex items-center"
                          >
                            <CheckCircle className="w-3 h-3 mr-1" /> Mark all read
                          </button>
                        )}
                      </div>

                      <div className="max-h-80 overflow-y-auto divide-y divide-gray-50">
                        {notifications.length === 0 ? (
                          <div className="py-8 text-center text-sm text-gray-400">
                            No notifications yet
                          </div>
                        ) : (
                          notifications.map((notif) => (
                            <div
                              key={notif.id}
                              onClick={() => {
                                if (!notif.is_read) markAsRead(notif.id)
                              }}
                              className={`p-3 text-sm hover:bg-gray-50 cursor-pointer transition-colors ${
                                !notif.is_read ? 'bg-orange-50/40 font-medium' : 'text-gray-600'
                              }`}
                            >
                              <div className="flex items-start justify-between">
                                <span className="font-semibold text-gray-900 text-xs">
                                  {notif.title}
                                </span>
                                <span className="text-[10px] text-gray-400">
                                  {new Date(notif.created_at).toLocaleTimeString([], {
                                    hour: '2-digit',
                                    minute: '2-digit',
                                  })}
                                </span>
                              </div>
                              <p className="text-xs text-gray-600 mt-1 leading-relaxed">
                                {notif.message}
                              </p>
                            </div>
                          ))
                        )}
                      </div>
                    </div>
                  )}
                </div>

                {/* User Pill & Logout */}
                <div className="flex items-center space-x-2 border-l border-gray-200 pl-3">
                  <div className="hidden sm:block text-right">
                    <p className="text-xs font-bold text-gray-900 leading-none">{user?.name}</p>
                    <span
                      className={`text-[10px] uppercase font-bold tracking-wider ${
                        isAdmin ? 'text-purple-600' : 'text-emerald-600'
                      }`}
                    >
                      {user?.role}
                    </span>
                  </div>
                  <button
                    onClick={logout}
                    className="p-2 text-gray-500 hover:text-red-600 hover:bg-red-50 rounded-lg transition-colors"
                    title="Log Out"
                  >
                    <LogOut className="w-4 h-4" />
                  </button>
                </div>
              </>
            ) : (
              <button
                onClick={() => onSelectTab('login')}
                className="px-4 py-2 bg-orange-600 hover:bg-orange-700 text-white rounded-lg text-sm font-semibold shadow-sm shadow-orange-200 transition-all"
              >
                Sign In
              </button>
            )}
          </div>
        </div>
      </div>
    </header>
  )
}
