package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"time"

	"campusbite/internal/middleware"
	"campusbite/internal/models"
	"campusbite/internal/repository"
	"campusbite/internal/service"
	"campusbite/internal/ws"

	"github.com/gin-gonic/gin"
)

// PaymentHandler handles payment initiation, client verification, and webhook notifications.
type PaymentHandler struct {
	paymentRepo     *repository.PaymentRepository
	orderRepo       *repository.OrderRepository
	webhookRepo     *repository.WebhookRepository
	razorpayService service.RazorpayService
	hub             *ws.Hub
}

// NewPaymentHandler creates a new PaymentHandler.
func NewPaymentHandler(
	paymentRepo *repository.PaymentRepository,
	orderRepo *repository.OrderRepository,
	webhookRepo *repository.WebhookRepository,
	razorpayService service.RazorpayService,
	hub ...*ws.Hub,
) *PaymentHandler {
	var wsHub *ws.Hub
	if len(hub) > 0 {
		wsHub = hub[0]
	}
	return &PaymentHandler{
		paymentRepo:     paymentRepo,
		orderRepo:       orderRepo,
		webhookRepo:     webhookRepo,
		razorpayService: razorpayService,
		hub:             wsHub,
	}
}

// CreatePaymentOrder initiates a Razorpay order for an existing pending order.
// POST /api/v1/orders/:id/payment
func (h *PaymentHandler) CreatePaymentOrder(c *gin.Context) {
	orderID := c.Param("id")
	if !isValidUUID(orderID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order ID format"})
		return
	}

	userID, ok := middleware.GetUserID(c)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	role, _ := middleware.GetUserRole(c)

	// Fetch order details
	order, err := h.orderRepo.GetByID(c.Request.Context(), orderID)
	if err != nil {
		if errors.Is(err, repository.ErrOrderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve order"})
		return
	}

	// Ownership validation: only owner or ADMIN may pay
	if role != models.RoleAdmin && order.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: you do not have permission to pay for this order"})
		return
	}

	// Order status validation
	if order.Status != models.OrderStatusPending {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot initiate payment for order with status: " + order.Status})
		return
	}

	// Verify that active inventory reservations exist and have not expired
	if len(order.Reservations) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order has no active inventory reservations"})
		return
	}

	now := time.Now()
	for _, res := range order.Reservations {
		if res.Status != models.ReservationStatusActive || res.ExpiresAt.Before(now) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "order inventory reservation has expired or is no longer active"})
			return
		}
	}

	// Amount in paise (e.g. ₹500.00 -> 50000 paise)
	amountInPaise := int64(math.Round(order.TotalAmount * 100))

	// Call Razorpay API to generate provider order ID
	razorpayOrderID, err := h.razorpayService.CreateRazorpayOrder(c.Request.Context(), amountInPaise, order.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create payment order with provider: " + err.Error()})
		return
	}

	// Persist pending payment record in PostgreSQL
	payment, err := h.paymentRepo.CreateOrGetPayment(c.Request.Context(), order.ID, razorpayOrderID, order.TotalAmount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record payment: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.CreatePaymentOrderResponse{
		PaymentID:       payment.ID,
		OrderID:         order.ID,
		RazorpayOrderID: razorpayOrderID,
		Amount:          amountInPaise,
		Currency:        "INR",
		KeyID:           h.razorpayService.GetKeyID(),
	})
}

// VerifyPayment processes client-submitted payment verification.
// POST /api/v1/payments/verify
func (h *PaymentHandler) VerifyPayment(c *gin.Context) {
	var req models.VerifyPaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// HMAC-SHA256 signature verification
	if !h.razorpayService.VerifyPaymentSignature(req.RazorpayOrderID, req.RazorpayPaymentID, req.RazorpaySignature) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payment signature"})
		return
	}

	// Transactionally confirm payment and order
	payment, order, err := h.paymentRepo.ConfirmPaymentSuccess(c.Request.Context(), req.RazorpayOrderID, req.RazorpayPaymentID)
	if err != nil {
		if errors.Is(err, repository.ErrPaymentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "payment record not found"})
			return
		}
		if errors.Is(err, repository.ErrPaymentConflict) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to confirm payment: " + err.Error()})
		return
	}

	// Notify order owner over WebSocket in real time after successful database confirmation
	if h.hub != nil {
		h.hub.SendToUser(order.UserID, ws.NewOrderStatusUpdatedEvent(order.ID, models.OrderStatusConfirmed))
	}

	c.JSON(http.StatusOK, models.VerifyPaymentResponse{
		Message:   "Payment verified and order confirmed successfully",
		PaymentID: payment.ID,
		OrderID:   order.ID,
		Status:    models.PaymentStatusSuccess,
	})
}

// HandleWebhook processes asynchronous webhook notifications from Razorpay.
// POST /api/v1/payments/webhook
func (h *PaymentHandler) HandleWebhook(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read webhook payload"})
		return
	}
	// Restore body for any subsequent readers if needed
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	signatureHeader := c.GetHeader("X-Razorpay-Signature")
	if !h.razorpayService.VerifyWebhookSignature(bodyBytes, signatureHeader) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook signature"})
		return
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json payload"})
		return
	}

	var payload models.RazorpayWebhookPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid razorpay webhook format"})
		return
	}

	// Determine unique provider event ID
	var providerEventID string
	if idVal, ok := rawMap["id"].(string); ok && idVal != "" {
		providerEventID = idVal
	} else if eventIDVal, ok := rawMap["event_id"].(string); ok && eventIDVal != "" {
		providerEventID = eventIDVal
	} else {
		// Fallback: derive deterministic sha256 of body if no explicit event id was provided
		h := sha256.New()
		h.Write(bodyBytes)
		providerEventID = "evt_" + hex.EncodeToString(h.Sum(nil))[:24]
	}

	// Deduplication check via database
	isDuplicate, err := h.webhookRepo.RecordAndCheckWebhookEvent(c.Request.Context(), providerEventID, payload.Event, bodyBytes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record webhook event: " + err.Error()})
		return
	}

	if isDuplicate {
		c.JSON(http.StatusOK, gin.H{"status": "already_processed", "message": "duplicate webhook event ignored"})
		return
	}

	// Process event based on type
	switch payload.Event {
	case "payment.captured", "order.paid":
		var providerOrderID, providerPaymentID string
		if payload.Payload.Payment != nil {
			providerOrderID = payload.Payload.Payment.Entity.OrderID
			providerPaymentID = payload.Payload.Payment.Entity.ID
		} else if payload.Payload.Order != nil {
			providerOrderID = payload.Payload.Order.Entity.ID
		}

		if providerOrderID != "" {
			_, order, err := h.paymentRepo.ConfirmPaymentSuccess(c.Request.Context(), providerOrderID, providerPaymentID)
			if err != nil && !errors.Is(err, repository.ErrPaymentNotFound) {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to confirm payment on webhook: " + err.Error()})
				return
			}
			if order != nil && h.hub != nil {
				h.hub.SendToUser(order.UserID, ws.NewOrderStatusUpdatedEvent(order.ID, models.OrderStatusConfirmed))
			}
		}

	case "payment.failed":
		var providerOrderID, providerPaymentID, reason string
		if payload.Payload.Payment != nil {
			providerOrderID = payload.Payload.Payment.Entity.OrderID
			providerPaymentID = payload.Payload.Payment.Entity.ID
			reason = payload.Payload.Payment.Entity.ErrorReason
		}

		if providerOrderID != "" {
			err := h.paymentRepo.HandlePaymentFailure(c.Request.Context(), providerOrderID, providerPaymentID, reason)
			if err != nil && !errors.Is(err, repository.ErrPaymentNotFound) {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to handle payment failure on webhook: " + err.Error()})
				return
			}
			// Look up payment to retrieve order owner for event delivery
			if h.hub != nil {
				if payment, err := h.paymentRepo.GetPaymentByProviderOrderID(c.Request.Context(), providerOrderID); err == nil && payment != nil {
					if order, err := h.orderRepo.GetByID(c.Request.Context(), payment.OrderID); err == nil && order != nil {
						h.hub.SendToUser(order.UserID, ws.NewOrderStatusUpdatedEvent(order.ID, models.OrderStatusCancelled))
					}
				}
			}
		}
	}

	_ = h.webhookRepo.MarkWebhookProcessed(c.Request.Context(), providerEventID)
	c.JSON(http.StatusOK, gin.H{"status": "processed"})
}
