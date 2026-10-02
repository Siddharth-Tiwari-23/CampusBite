import React, { useState, useEffect } from 'react'
import {
  Search,
  Plus,
  Check,
  Loader2,
  AlertCircle,
  UtensilsCrossed,
} from 'lucide-react'
import { menuService } from '../services/menuService'
import { useCart } from '../context/CartContext'
import { useAuth } from '../context/AuthContext'

function getCategoryForItem(item) {
  if (item.category && item.category !== 'Cafeteria' && item.category !== 'Fast Food' && item.category !== 'General') {
    return item.category
  }
  const name = (item.name || '').toLowerCase()
  if (name.includes('momo')) return 'Momos'
  if (name.includes('paratha')) return 'Paratha'
  if (name.includes('coffee') || name.includes('chai') || name.includes('tea') || name.includes('beverage')) return 'Beverages'
  if (name.includes('samosa') || name.includes('fries') || name.includes('snack')) return 'Snacks'
  if (name.includes('pasta')) return 'Pasta'
  if (name.includes('pizza')) return 'Pizza'
  if (name.includes('roll') || name.includes('wrap')) return 'Rolls'
  return 'Specials'
}

function FoodImage({ src, alt }) {
  const [error, setError] = useState(false)
  const [loaded, setLoaded] = useState(false)

  const fallbackUrl = 'https://images.unsplash.com/photo-1546069901-ba9599a7e63c?w=600&auto=format&fit=crop&q=80'

  return (
    <div className="relative w-full aspect-[16/10] bg-gray-100 rounded-xl overflow-hidden">
      {!loaded && !error && (
        <div className="absolute inset-0 bg-gray-200/80 animate-pulse" />
      )}
      <img
        src={error || !src ? fallbackUrl : src}
        alt={alt}
        onLoad={() => setLoaded(true)}
        onError={() => setError(true)}
        className={`w-full h-full object-cover transition-opacity duration-300 ${
          loaded ? 'opacity-100' : 'opacity-0'
        }`}
        loading="lazy"
      />
    </div>
  )
}

export function MenuPage({ onNavigateToCart }) {
  const { isAuthenticated } = useAuth()
  const { addToCart } = useCart()
  const [items, setItems] = useState([])
  const [categories, setCategories] = useState([])
  const [selectedCategory, setSelectedCategory] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState(null)
  const [addingId, setAddingId] = useState(null)
  const [justAddedId, setJustAddedId] = useState(null)

  const fetchMenu = async () => {
    try {
      setIsLoading(true)
      setError(null)
      const data = await menuService.getMenu()
      const rawList = Array.isArray(data) ? data : (data?.items || [])

      // Filter to strictly available products only
      const activeList = rawList.filter((item) => item.is_available === true)

      const enriched = activeList.map((item) => ({
        ...item,
        category: getCategoryForItem(item),
      }))

      setItems(enriched)

      // Derive distinct useful categories
      const distinct = Array.from(new Set(enriched.map((item) => item.category).filter(Boolean)))
      setCategories(distinct)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load menu items')
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    fetchMenu()
  }, [])

  const handleAddToCart = async (item) => {
    if (!isAuthenticated) {
      alert('Please sign in to add items to your tray.')
      return
    }

    try {
      setAddingId(item.id)
      await addToCart(item.id, 1)
      setJustAddedId(item.id)
      setTimeout(() => setJustAddedId(null), 1200)
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to add item to tray')
    } finally {
      setAddingId(null)
    }
  }

  const filteredItems = items.filter((item) => {
    const matchesCategory = !selectedCategory || item.category === selectedCategory
    const q = searchQuery.trim().toLowerCase()
    const matchesSearch =
      !q ||
      item.name.toLowerCase().includes(q) ||
      (item.category && item.category.toLowerCase().includes(q))
    return matchesCategory && matchesSearch
  })

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6 space-y-6">
      {/* Search & Category Filter Row */}
      <div className="flex flex-col md:flex-row gap-3 justify-between items-stretch md:items-center">
        {/* Search Input */}
        <div className="relative flex-1 max-w-sm">
          <Search className="w-4 h-4 absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search food items..."
            className="w-full pl-10 pr-4 py-2 bg-white border border-gray-200 rounded-xl text-xs sm:text-sm text-gray-900 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-orange-500 focus:border-transparent transition-all shadow-sm"
          />
        </div>

        {/* Category Pills */}
        <div className="flex items-center gap-1.5 overflow-x-auto pb-1 md:pb-0 scrollbar-none">
          <button
            onClick={() => setSelectedCategory('')}
            className={`px-3.5 py-1.5 rounded-full text-xs font-semibold whitespace-nowrap transition-all ${
              selectedCategory === ''
                ? 'bg-slate-900 text-white shadow-sm'
                : 'bg-white text-gray-600 hover:bg-gray-50 border border-gray-200'
            }`}
          >
            All Items
          </button>
          {categories.map((cat) => (
            <button
              key={cat}
              onClick={() => setSelectedCategory(cat)}
              className={`px-3.5 py-1.5 rounded-full text-xs font-semibold whitespace-nowrap transition-all ${
                selectedCategory === cat
                  ? 'bg-slate-900 text-white shadow-sm'
                  : 'bg-white text-gray-600 hover:bg-gray-50 border border-gray-200'
              }`}
            >
              {cat}
            </button>
          ))}
        </div>
      </div>

      {/* Main Grid / Content */}
      {isLoading ? (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4 sm:gap-5">
          {Array.from({ length: 10 }).map((_, idx) => (
            <div
              key={idx}
              className="bg-white rounded-2xl border border-gray-100 p-3 space-y-3 animate-pulse shadow-sm"
            >
              <div className="w-full aspect-[16/10] bg-gray-200 rounded-xl" />
              <div className="h-4 bg-gray-200 rounded w-3/4" />
              <div className="h-4 bg-gray-200 rounded w-1/3" />
              <div className="h-8 bg-gray-200 rounded-xl w-full" />
            </div>
          ))}
        </div>
      ) : error ? (
        <div className="p-4 bg-red-50 text-red-700 rounded-2xl border border-red-100 flex items-center space-x-3 max-w-lg mx-auto">
          <AlertCircle className="w-5 h-5 text-red-500 shrink-0" />
          <div className="text-xs">
            <p className="font-bold">Failed to load menu</p>
            <p className="text-red-600">{error}</p>
          </div>
        </div>
      ) : filteredItems.length === 0 ? (
        <div className="py-20 text-center bg-white rounded-2xl border border-gray-100 p-8 shadow-sm max-w-md mx-auto">
          <UtensilsCrossed className="w-10 h-10 text-gray-300 mx-auto mb-2" />
          <h3 className="text-sm font-bold text-gray-800">No menu items available right now.</h3>
          <p className="text-xs text-gray-400 mt-1">
            Try adjusting your search or selecting another category.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4 sm:gap-5">
          {filteredItems.map((item) => {
            const isAdding = addingId === item.id
            const isJustAdded = justAddedId === item.id

            return (
              <div
                key={item.id}
                className="bg-white rounded-2xl border border-gray-100 shadow-[0_2px_8px_rgba(15,23,42,0.06)] hover:shadow-md hover:-translate-y-0.5 transition-all duration-200 flex flex-col justify-between p-3"
              >
                <div className="space-y-2.5">
                  {/* Food Image */}
                  <FoodImage src={item.image_url} alt={item.name} />

                  {/* Name and Price */}
                  <div className="space-y-0.5">
                    <h3 className="font-semibold text-gray-900 text-xs sm:text-sm line-clamp-1" title={item.name}>
                      {item.name}
                    </h3>
                    <p className="text-sm sm:text-base font-bold text-orange-600">
                      ₹{item.price}
                    </p>
                  </div>
                </div>

                {/* Action Button */}
                <div className="pt-3">
                  <button
                    onClick={() => handleAddToCart(item)}
                    disabled={isAdding}
                    className={`w-full py-2 px-3 rounded-xl text-xs font-semibold transition-all flex items-center justify-center space-x-1 shadow-sm ${
                      isJustAdded
                        ? 'bg-emerald-600 text-white'
                        : 'bg-orange-600 hover:bg-orange-700 text-white active:scale-98'
                    }`}
                  >
                    {isAdding ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : isJustAdded ? (
                      <>
                        <Check className="w-3.5 h-3.5" />
                        <span>Added</span>
                      </>
                    ) : (
                      <>
                        <Plus className="w-3.5 h-3.5" />
                        <span>Add to Tray</span>
                      </>
                    )}
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
