package ws

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 1024

	// Outbound message buffer size per client.
	sendBufferSize = 256
)

// Client is a middleman between the websocket connection and the Hub.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	userID   string
	userRole string
	send     chan []byte
	closed   bool
}

// NewClient creates a new Client instance.
func NewClient(hub *Hub, conn *websocket.Conn, userID, userRole string) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		userID:   userID,
		userRole: userRole,
		send:     make(chan []byte, sendBufferSize),
	}
}

// UserID returns the authenticated user's ID.
func (c *Client) UserID() string {
	return c.userID
}

// UserRole returns the authenticated user's role.
func (c *Client) UserRole() string {
	return c.userRole
}

// ReadPump pumps messages from the websocket connection to the hub.
// It detects disconnections and handles ping/pong heartbeats.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WS Client] Read error: %v", err)
			}
			break
		}
	}
}

// WritePump pumps messages from the client's send channel to the websocket connection.
// It executes as a dedicated single writer goroutine per connection to prevent concurrent writes.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(message); err != nil {
				return
			}

			// Batch any queued messages currently in buffer into the single frame
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Send enqueues a message into the client's send buffer. Returns false if the buffer is full.
func (c *Client) Send(data []byte) bool {
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}
