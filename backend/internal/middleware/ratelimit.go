package middleware

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"campusbite/internal/cache"

	"github.com/gin-gonic/gin"
)

// RateLimit creates a Gin middleware that enforces atomic fixed-window rate limiting.
// If the user is authenticated, rate limiting is scoped by User ID (rate:user:<userID>:<action>).
// Otherwise, it is scoped by client IP (rate:ip:<clientIP>:<action>).
// If the cache backend is unavailable, it gracefully fails open to protect critical business flows.
func RateLimit(cacheService cache.CacheService, action string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cacheService == nil {
			c.Next()
			return
		}

		// Determine identity key
		var key string
		if userID, ok := GetUserID(c); ok && userID != "" {
			key = fmt.Sprintf("rate:user:%s:%s", userID, action)
		} else {
			key = fmt.Sprintf("rate:ip:%s:%s", c.ClientIP(), action)
		}

		allowed, remaining, retryAfter, err := cacheService.Allow(c.Request.Context(), key, limit, window)
		if err != nil {
			// Fail-open policy: Log cache error but do not block legitimate user requests
			log.Printf("[RateLimiter] Error evaluating rate limit for key '%s': %v (failing open)", key, err)
			c.Next()
			return
		}

		if !allowed {
			retrySec := int(retryAfter.Seconds())
			if retrySec <= 0 {
				retrySec = int(window.Seconds())
			}
			c.Header("Retry-After", strconv.Itoa(retrySec))
			c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
			c.Header("X-RateLimit-Remaining", "0")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests, please try again later",
			})
			return
		}

		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Next()
	}
}
