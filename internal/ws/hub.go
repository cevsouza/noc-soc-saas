package ws

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Client represents a single active operator WebSocket connection.
type Client struct {
	ID          uuid.UUID
	TenantIDs   []uuid.UUID
	Conn        *websocket.Conn
	Send        chan []byte // Buffered channel for outbound messages
	UserID      uuid.UUID
	Email       string
	Name        string
	Role        string
	ConnectedAt time.Time
	once        sync.Once
}

// Hub orchestrates WebSocket client connections grouped by Tenant ID in a thread-safe manner.
type Hub struct {
	// Group clients by tenant ID for fast lookup and strict RLS/Tenant multi-cast isolation
	tenants    map[uuid.UUID]map[*Client]bool
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

type ActiveUserDTO struct {
	SessionID   string    `json:"session_id"`
	UserID      string    `json:"user_id"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	Role        string    `json:"role"`
	ConnectedAt time.Time `json:"connected_at"`
}

func (h *Hub) GetActiveUsers() []ActiveUserDTO {
	h.mu.RLock()
	defer h.mu.RUnlock()

	uniqueClients := make(map[*Client]bool)
	for _, clients := range h.tenants {
		for client := range clients {
			uniqueClients[client] = true
		}
	}

	result := make([]ActiveUserDTO, 0, len(uniqueClients))
	for client := range uniqueClients {
		if client.Email != "" {
			result = append(result, ActiveUserDTO{
				SessionID:   client.ID.String(),
				UserID:      client.UserID.String(),
				Email:       client.Email,
				Name:        client.Name,
				Role:        client.Role,
				ConnectedAt: client.ConnectedAt,
			})
		}
	}
	return result
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
			for _, tenantID := range client.TenantIDs {
				if h.tenants[tenantID] == nil {
					h.tenants[tenantID] = make(map[*Client]bool)
				}
				h.tenants[tenantID][client] = true
			}
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			for _, tenantID := range client.TenantIDs {
				if clients, ok := h.tenants[tenantID]; ok {
					delete(clients, client)
					if len(clients) == 0 {
						delete(h.tenants, tenantID)
					}
				}
			}
			close(client.Send)
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
	client.once.Do(func() {
		h.unregister <- client
		client.Conn.Close()
	})
}
