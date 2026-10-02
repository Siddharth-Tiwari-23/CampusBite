package handlers

import (
	"errors"
	"net/http"

	"campusbite/internal/models"
	"campusbite/internal/service"

	"github.com/gin-gonic/gin"
)

// AnalyticsHandler handles admin natural-language business analytics requests.
type AnalyticsHandler struct {
	router *service.AnalyticsRouter
}

// NewAnalyticsHandler creates a new AnalyticsHandler.
func NewAnalyticsHandler(router *service.AnalyticsRouter) *AnalyticsHandler {
	return &AnalyticsHandler{
		router: router,
	}
}

// Query processes an admin natural language business analytics question.
// POST /api/v1/analytics/query
func (h *AnalyticsHandler) Query(c *gin.Context) {
	var req models.AnalyticsQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: 'question' is required"})
		return
	}

	res, err := h.router.ExecuteQuery(c.Request.Context(), req.Question)
	if err != nil {
		if errors.Is(err, service.ErrEmptyQuestion) || errors.Is(err, service.ErrQuestionTooLong) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, service.ErrUnsupportedIntent) || errors.Is(err, service.ErrUnsupportedPeriod) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unable to process analytics question: unsupported query or intent"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process analytics query"})
		return
	}

	c.JSON(http.StatusOK, res)
}

// Trends computes lightweight historical trend comparisons between current and previous periods.
// POST /api/v1/analytics/trends
func (h *AnalyticsHandler) Trends(c *gin.Context) {
	var req models.TrendAnalyticsRequest
	if c.Request.Body != nil && c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: 'period' must be a valid string"})
			return
		}
	}
	if req.Period == "" {
		req.Period = c.Query("period")
	}

	res, err := h.router.ExecuteTrendAnalysis(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrUnsupportedPeriod) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported trend period: allowed values are TODAY, THIS_WEEK, THIS_MONTH"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process trend analytics query"})
		return
	}

	c.JSON(http.StatusOK, res)
}
