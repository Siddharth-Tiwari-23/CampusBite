import React, { useState } from 'react'
import { ShieldCheck, Loader2, AlertCircle, X } from 'lucide-react'
import { paymentService } from '../services/paymentService'

export function PaymentModal({
  isOpen,
  onClose,
  paymentDetails,
  onSuccess,
}) {
  const [isProcessing, setIsProcessing] = useState(false)
  const [error, setError] = useState(null)

  if (!isOpen || !paymentDetails) return null

  const handleSimulatePayment = async () => {
    setIsProcessing(true)
    setError(null)

    try {
      // In Razorpay Test/Sandbox mode, we generate a mock payment ID and signature
      const mockPaymentId = `pay_test_${Date.now()}`
      const mockSignature = `sig_test_${Date.now()}`

      await paymentService.verifyPayment({
        razorpay_order_id: paymentDetails.razorpay_order_id,
        razorpay_payment_id: mockPaymentId,
        razorpay_signature: mockSignature,
      })

      setIsProcessing(false)
      onSuccess(paymentDetails.order_id)
    } catch (err) {
      setIsProcessing(false)
      setError(err instanceof Error ? err.message : 'Payment verification failed')
    }
  }

  const formattedAmount = (paymentDetails.amount / 100).toFixed(2)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 animate-in fade-in">
      <div className="bg-white w-full max-w-md rounded-2xl shadow-2xl overflow-hidden border border-gray-100 animate-in zoom-in-95">
        {/* Modal Header */}
        <div className="bg-gradient-to-r from-orange-600 to-amber-600 p-6 text-white relative">
          <button
            onClick={onClose}
            disabled={isProcessing}
            className="absolute top-4 right-4 text-white/80 hover:text-white p-1 rounded-full hover:bg-white/10 transition-colors disabled:opacity-50"
          >
            <X className="w-5 h-5" />
          </button>
          <div className="flex items-center space-x-2 text-orange-200 text-xs font-bold uppercase tracking-wider mb-1">
            <ShieldCheck className="w-4 h-4" />
            <span>Razorpay Sandbox Gateway</span>
          </div>
          <h3 className="text-xl font-black">CampusBite Checkout</h3>
          <p className="text-sm text-orange-100 mt-1">
            Order ID: <span className="font-mono">{paymentDetails.order_id.slice(0, 8)}...</span>
          </p>
        </div>

        {/* Modal Body */}
        <div className="p-6 space-y-6">
          <div className="bg-gray-50 rounded-xl p-4 border border-gray-100 flex justify-between items-center">
            <div>
              <span className="text-xs text-gray-500 uppercase font-semibold">Total Amount</span>
              <p className="text-2xl font-black text-gray-900">₹{formattedAmount}</p>
            </div>
            <div className="text-right">
              <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-amber-100 text-amber-800">
                Test Mode
              </span>
              <p className="text-[10px] text-gray-400 mt-1 font-mono">
                Key: {paymentDetails.key_id?.slice(0, 10)}...
              </p>
            </div>
          </div>

          {error && (
            <div className="p-3 bg-red-50 text-red-700 text-sm rounded-xl flex items-start space-x-2 border border-red-100">
              <AlertCircle className="w-4 h-4 mt-0.5 shrink-0 text-red-500" />
              <span>{error}</span>
            </div>
          )}

          <div className="space-y-3">
            <p className="text-xs text-gray-500 text-center">
              This is a sandbox test payment environment. No real funds will be deducted.
            </p>

            <button
              onClick={handleSimulatePayment}
              disabled={isProcessing}
              className="w-full py-3.5 px-4 bg-emerald-600 hover:bg-emerald-700 text-white font-bold rounded-xl shadow-lg shadow-emerald-200 transition-all flex items-center justify-center space-x-2 disabled:opacity-50"
            >
              {isProcessing ? (
                <>
                  <Loader2 className="w-5 h-5 animate-spin" />
                  <span>Verifying Transaction...</span>
                </>
              ) : (
                <span>Pay ₹{formattedAmount} (Authorize Test)</span>
              )}
            </button>

            <button
              onClick={onClose}
              disabled={isProcessing}
              className="w-full py-2.5 text-xs text-gray-500 hover:text-gray-700 font-medium transition-colors"
            >
              Cancel Payment
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
