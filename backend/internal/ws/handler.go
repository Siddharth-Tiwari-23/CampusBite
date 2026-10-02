package ws

import (
	"log"
	"net/http"
	"strings"

	"campusbite/internal/auth"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var defaultUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for API & dev access
		return true
	},
}

// WSHandler handles WebSocket upgrade requests and connection lifecycle.
type WSHandler struct {
	hub          *Hub
	tokenService *auth.TokenService
	upgrader     websocket.Upgrader
}

// NewWSHandler creates a new WSHandler instance.
func NewWSHandler(hub *Hub, tokenService *auth.TokenService) *WSHandler {
	return &WSHandler{
		hub:          hub,
		tokenService: tokenService,
		upgrader:     defaultUpgrader,
	}
}

// HandleWS upgrades HTTP connection to WebSocket after verifying JWT credentials.
// GET /api/v1/ws
func (h *WSHandler) HandleWS(c *gin.Context) {
	// Extract token from query parameter or Authorization header
	tokenString := strings.TrimSpace(c.Query("token"))
	if tokenString == "" {
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			tokenString = strings.TrimSpace(authHeader[7:])
		}
	}
	if tokenString == "" {
		tokenString = strings.TrimSpace(c.GetHeader("Sec-WebSocket-Protocol"))
	}

	if tokenString == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication token"})
		return
	}

	// Validate JWT token
	claims, err := h.tokenService.ValidateToken(tokenString)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WSHandler] Upgrade failed for user %s: %v", claims.UserID, err)
		return
	}

	// Initialize and register client
	client := NewClient(h.hub, conn, claims.UserID, claims.Role)
	h.hub.Register(client)

	// Start pump loops
	go client.WritePump()
	client.ReadPump() // blocks until connection closes
}
