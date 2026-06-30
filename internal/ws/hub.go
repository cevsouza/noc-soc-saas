package ws

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Client represents a single active operator WebSocket connection.
type Client struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Conn     *websocket.Conn
	Send     chan []byte // Buffered channel for outbound messages
}

// Hub orchestrates WebSocket client connections grouped by Tenant ID in a thread-safe manner.
type Hub struct {
	// Group clients by tenant ID for fast lookup and strict RLS/Tenant multi-cast isolation
	tenants    map[uuid.UUID]map[*Client]bool
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		tenants:    make(map[uuid.UUID]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run listens on channels for client registration and unregistration.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			// Close all active connections on shutdown
			h.mu.Lock()
			for _, clients := range h.tenants {
				for client := range clients {
					client.Conn.Close()
				}
			}
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			if h.tenants[client.TenantID] == nil {
				h.tenants[client.TenantID] = make(map[*Client]bool)
			}
			h.tenants[client.TenantID][client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.tenants[client.TenantID]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					close(client.Send)
					if len(clients) == 0 {
						delete(h.tenants, client.TenantID)
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

// BroadcastToTenant sends a message to all active clients of a specific tenant only.
// This is the frontend-level enforcement of multi-tenancy logical isolation.
func (h *Hub) BroadcastToTenant(tenantID uuid.UUID, message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	clients, ok := h.tenants[tenantID]
	if !ok {
		return
	}

	for client := range clients {
		select {
		case client.Send <- message:
		default:
			// If client's write channel is blocked, drop the connection immediately
			// to protect system resource allocation from "slow-client attacks" or stale websockets.
			go h.ForceDisconnect(client)
		}
	}
}

func (h *Hub) ForceDisconnect(client *Client) {
	h.unregister <- client
	client.Conn.Close()
}
