package ws

import (
	"encoding/json"
	"log"
	"sync"
)

// Hub maintains the set of active authenticated clients and broadcasts messages.
type Hub struct {
	mu     sync.RWMutex
	users  map[string]map[*Client]bool
	admins map[*Client]bool
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		users:  make(map[string]map[*Client]bool),
		admins: make(map[*Client]bool),
	}
}

// Register adds a client connection to the hub under their authenticated user ID and role.
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.users[c.userID] == nil {
		h.users[c.userID] = make(map[*Client]bool)
	}
	h.users[c.userID][c] = true

	if c.userRole == "ADMIN" {
		h.admins[c] = true
	}
}

// Unregister removes a client connection from the hub and closes its send channel.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.users[c.userID]; ok {
		if _, present := clients[c]; present {
			delete(clients, c)
			if len(clients) == 0 {
				delete(h.users, c.userID)
			}
			if c.userRole == "ADMIN" {
				delete(h.admins, c)
			}
			close(c.send)
		}
	}
}

// SendToUser delivers an event strictly to all active connections belonging to a specific authenticated user.
func (h *Hub) SendToUser(userID string, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[WS Hub] Failed to marshal event: %v", err)
		return
	}

	h.mu.RLock()
	clientsMap, exists := h.users[userID]
	if !exists || len(clientsMap) == 0 {
		h.mu.RUnlock()
		return
	}

	// Make a snapshot of clients to avoid holding the lock during send
	clients := make([]*Client, 0, len(clientsMap))
	for client := range clientsMap {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		if !client.Send(data) {
			// Outbound buffer full: unregister connection asynchronously
			go h.Unregister(client)
		}
	}
}

// BroadcastToAdmins sends an event to all authenticated admin connections.
func (h *Hub) BroadcastToAdmins(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[WS Hub] Failed to marshal admin event: %v", err)
		return
	}

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.admins))
	for client := range h.admins {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		if !client.Send(data) {
			go h.Unregister(client)
		}
	}
}

// Broadcast sends an event to all active connections across all users.
func (h *Hub) Broadcast(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[WS Hub] Failed to marshal broadcast event: %v", err)
		return
	}

	h.mu.RLock()
	var allClients []*Client
	for _, clientMap := range h.users {
		for client := range clientMap {
			allClients = append(allClients, client)
		}
	}
	h.mu.RUnlock()

	for _, client := range allClients {
		if !client.Send(data) {
			go h.Unregister(client)
		}
	}
}

// GetActiveUserCount returns the number of distinct authenticated users currently connected.
func (h *Hub) GetActiveUserCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.users)
}

// GetAdminCount returns the number of active admin connections.
func (h *Hub) GetAdminCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.admins)
}

// IsUserConnected checks if a specific user has at least one active connection.
func (h *Hub) IsUserConnected(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients, ok := h.users[userID]
	return ok && len(clients) > 0
}
