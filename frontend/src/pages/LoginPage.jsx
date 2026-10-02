import React, { useState } from 'react'
import { UtensilsCrossed, AlertCircle, Loader2, ArrowRight } from 'lucide-react'
import { useAuth } from '../context/AuthContext'

export function LoginPage({ onSuccess }) {
  const { login, register } = useAuth()
  const [isRegisterMode, setIsRegisterMode] = useState(false)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const [role, setRole] = useState('STUDENT')
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState(null)

  const handleSubmit = async (e) => {
    e.preventDefault()
    setIsLoading(true)
    setError(null)

    try {
      if (isRegisterMode) {
        await register(name, email, password, role)
      } else {
        await login(email, password)
      }
      onSuccess?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Authentication failed')
    } finally {
      setIsLoading(false)
    }
  }

  const fillDemoAccount = (demoEmail, demoPassword) => {
    setIsRegisterMode(false)
    setEmail(demoEmail)
    setPassword(demoPassword)
  }

  return (
    <div className="min-h-[85vh] flex items-center justify-center px-4 sm:px-6 lg:px-8 py-12">
      <div className="max-w-md w-full space-y-8 bg-white p-8 sm:p-10 rounded-3xl shadow-xl border border-gray-100">
        <div className="text-center">
          <div className="mx-auto w-16 h-16 bg-orange-600 rounded-2xl flex items-center justify-center text-white shadow-lg shadow-orange-200 mb-4">
            <UtensilsCrossed className="w-9 h-9" />
          </div>
          <h2 className="text-3xl font-black text-gray-900 tracking-tight">
            {isRegisterMode ? 'Create Campus Account' : 'Welcome to CampusBite'}
          </h2>
          <p className="mt-2 text-sm text-gray-500">
            {isRegisterMode
              ? 'Join the fastest cafeteria queue experience'
              : 'Sign in to order fresh meals with zero wait time'}
          </p>
        </div>

        {error && (
          <div className="p-4 bg-red-50 text-red-700 text-sm rounded-2xl flex items-start space-x-2 border border-red-100 animate-in fade-in">
            <AlertCircle className="w-5 h-5 shrink-0 text-red-500 mt-0.5" />
            <span>{error}</span>
          </div>
        )}

        <form className="mt-8 space-y-4" onSubmit={handleSubmit}>
          {isRegisterMode && (
            <div>
              <label className="block text-xs font-bold uppercase tracking-wider text-gray-600 mb-1">
                Full Name
              </label>
              <input
                type="text"
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Alex Morgan"
                className="w-full px-4 py-3 rounded-xl border border-gray-200 focus:outline-none focus:ring-2 focus:ring-orange-500 focus:border-transparent transition-all text-sm"
              />
            </div>
          )}

          <div>
            <label className="block text-xs font-bold uppercase tracking-wider text-gray-600 mb-1">
              Email Address
            </label>
            <input
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="alex@campus.edu"
              className="w-full px-4 py-3 rounded-xl border border-gray-200 focus:outline-none focus:ring-2 focus:ring-orange-500 focus:border-transparent transition-all text-sm"
            />
          </div>

          <div>
            <label className="block text-xs font-bold uppercase tracking-wider text-gray-600 mb-1">
              Password
            </label>
            <input
              type="password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              className="w-full px-4 py-3 rounded-xl border border-gray-200 focus:outline-none focus:ring-2 focus:ring-orange-500 focus:border-transparent transition-all text-sm"
            />
          </div>

          {isRegisterMode && (
            <div>
              <label className="block text-xs font-bold uppercase tracking-wider text-gray-600 mb-1">
                Account Role
              </label>
              <div className="grid grid-cols-2 gap-2">
                <button
                  type="button"
                  onClick={() => setRole('STUDENT')}
                  className={`py-2 text-xs font-bold rounded-lg border transition-colors ${
                    role === 'STUDENT'
                      ? 'bg-orange-50 border-orange-500 text-orange-700'
                      : 'border-gray-200 text-gray-600 hover:bg-gray-50'
                  }`}
                >
                  Student
                </button>
                <button
                  type="button"
                  onClick={() => setRole('ADMIN')}
                  className={`py-2 text-xs font-bold rounded-lg border transition-colors ${
                    role === 'ADMIN'
                      ? 'bg-purple-50 border-purple-500 text-purple-700'
                      : 'border-gray-200 text-gray-600 hover:bg-gray-50'
                  }`}
                >
                  Admin / Staff
                </button>
              </div>
            </div>
          )}

          <button
            type="submit"
            disabled={isLoading}
            className="w-full mt-2 py-3.5 px-4 bg-orange-600 hover:bg-orange-700 text-white font-bold rounded-xl shadow-lg shadow-orange-200 transition-all flex items-center justify-center space-x-2 disabled:opacity-50"
          >
            {isLoading ? (
              <Loader2 className="w-5 h-5 animate-spin" />
            ) : (
              <>
                <span>{isRegisterMode ? 'Complete Registration' : 'Sign In to CampusBite'}</span>
                <ArrowRight className="w-4 h-4" />
              </>
            )}
          </button>
        </form>

        <div className="pt-2 text-center">
          <button
            onClick={() => {
              setIsRegisterMode(!isRegisterMode)
              setError(null)
            }}
            className="text-xs font-medium text-orange-600 hover:underline"
          >
            {isRegisterMode
              ? 'Already have an account? Sign in here'
              : "Don't have an account? Create one now"}
          </button>
        </div>

        {/* Quick Demo Logins */}
        <div className="pt-4 border-t border-gray-100">
          <p className="text-[11px] uppercase tracking-wider font-bold text-gray-400 text-center mb-2">
            Demo Credentials
          </p>
          <div className="grid grid-cols-2 gap-2 text-xs">
            <button
              onClick={() => fillDemoAccount('student@campusbite.com', 'password123')}
              className="p-2 rounded-xl bg-gray-50 hover:bg-orange-50 hover:text-orange-700 text-gray-600 border border-gray-200 text-left transition-colors"
            >
              <p className="font-bold">Student User</p>
              <p className="text-[10px] text-gray-400">student@campusbite.com</p>
            </button>
            <button
              onClick={() => fillDemoAccount('admin@campusbite.com', 'password123')}
              className="p-2 rounded-xl bg-gray-50 hover:bg-purple-50 hover:text-purple-700 text-gray-600 border border-gray-200 text-left transition-colors"
            >
              <p className="font-bold">Admin User</p>
              <p className="text-[10px] text-gray-400">admin@campusbite.com</p>
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
