import React, { useState } from 'react'
import { AuthProvider, useAuth } from './context/AuthContext'
import { CartProvider } from './context/CartContext'
import { NotificationProvider } from './context/NotificationContext'
import { WebSocketProvider } from './context/WebSocketContext'
import { Navbar } from './components/Navbar'
import { LoginPage } from './pages/LoginPage'
import { MenuPage } from './pages/MenuPage'
import { CartPage } from './pages/CartPage'
import { OrderHistoryPage } from './pages/OrderHistoryPage'
import { OrderDetailPage } from './pages/OrderDetailPage'
import { AdminDashboardPage } from './pages/AdminDashboardPage'
import { UtensilsCrossed } from 'lucide-react'

function AppContent() {
  const { isAuthenticated, isAdmin, isLoading } = useAuth()
  const [activeTab, setActiveTab] = useState('menu')
  const [selectedOrderId, setSelectedOrderId] = useState(null)

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="text-center space-y-3">
          <div className="w-12 h-12 bg-orange-600 rounded-2xl flex items-center justify-center text-white mx-auto animate-bounce shadow-lg shadow-orange-200">
            <UtensilsCrossed className="w-6 h-6" />
          </div>
          <p className="text-xs font-bold uppercase tracking-wider text-gray-400">
            Loading CampusBite...
          </p>
        </div>
      </div>
    )
  }

  const handleSelectTab = (tab) => {
    setSelectedOrderId(null)
    setActiveTab(tab)
  }

  const handleOrderCreated = (orderId) => {
    setSelectedOrderId(orderId)
    setActiveTab('order-detail')
  }

  const handleSelectOrder = (orderId) => {
    setSelectedOrderId(orderId)
    setActiveTab('order-detail')
  }

  return (
    <div className="min-h-screen flex flex-col bg-slate-50 text-slate-900">
      <Navbar activeTab={activeTab} onSelectTab={handleSelectTab} />

      <main className="flex-1 pb-16">
        {!isAuthenticated && activeTab === 'login' && (
          <LoginPage onSuccess={() => setActiveTab('menu')} />
        )}

        {(!isAuthenticated && activeTab !== 'login') || activeTab === 'menu' ? (
          <MenuPage onNavigateToCart={() => setActiveTab('cart')} />
        ) : null}

        {isAuthenticated && activeTab === 'cart' && (
          <CartPage
            onNavigateToMenu={() => setActiveTab('menu')}
            onOrderCreated={handleOrderCreated}
          />
        )}

        {isAuthenticated && activeTab === 'orders' && (
          <OrderHistoryPage
            onSelectOrder={handleSelectOrder}
            onNavigateToMenu={() => setActiveTab('menu')}
          />
        )}

        {isAuthenticated && activeTab === 'order-detail' && selectedOrderId && (
          <OrderDetailPage
            orderId={selectedOrderId}
            onBack={() => setActiveTab('orders')}
          />
        )}

        {isAuthenticated && isAdmin && activeTab === 'admin' && (
          <AdminDashboardPage />
        )}
      </main>

      <footer className="border-t border-gray-200 bg-white py-6 text-center text-xs text-gray-500">
        <div className="max-w-7xl mx-auto px-4 flex flex-col sm:flex-row items-center justify-between gap-2">
          <p>© {new Date().getFullYear()} CampusBite Cafeteria System</p>
          <p className="text-[11px] text-gray-400">All rights reserved.</p>
        </div>
      </footer>
    </div>
  )
}

export function App() {
  return (
    <AuthProvider>
      <CartProvider>
        <NotificationProvider>
          <WebSocketProvider>
            <AppContent />
          </WebSocketProvider>
        </NotificationProvider>
      </CartProvider>
    </AuthProvider>
  )
}

export default App
