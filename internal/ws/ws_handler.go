package ws

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In production, perform origin checking. Allow all for local dev.
		return true
	},
}

// readPump pumps messages from the WebSocket connection to the hub.
func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.ForceDisconnect(c)
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		// Ingestion-only: We discard all inbound messages from clients
		// as operators only receive push data.
	}
}

// writePump pumps messages from the hub's broadcast queue to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ServeWS handles WebSocket upgrading and client connection setup.
func ServeWS(hub *Hub, pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "Unauthorized: Missing token in query parameter", http.StatusUnauthorized)
			return
		}

		ctx := r.Context()
		var tenantID uuid.UUID
		var err error

		// For the multi-tenancy visual simulator, we allow direct tenant ID UUID connections.
		// Otherwise, we perform a standard API Key SHA-256 resolution.
		parsedUUID, err := uuid.Parse(token)
		if err == nil {
			tenantID = parsedUUID
		} else {
			// Resolve as API Key hash
			hash := sha256.Sum256([]byte(token))
			keyHash := hex.EncodeToString(hash[:])

			query := `
				SELECT tenant_id 
				FROM tenant_api_keys 
				WHERE key_hash = $1 AND (expires_at IS NULL OR expires_at > NOW())
			`
			err = pgPool.QueryRow(ctx, query, keyHash).Scan(&tenantID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
					return
				}
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}

		// Upgrade HTTP connection to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}

		client := &Client{
			ID:       uuid.New(),
			TenantID: tenantID,
			Conn:     conn,
			Send:     make(chan []byte, 256),
		}

		hub.register <- client

		// Spin up read and write loops
		go client.writePump()
		go client.readPump(hub)
	}
}

// StartGlobalPubSubMultiplexer runs a single global subscription using PSUBSCRIBE
// that reads all events across all tenants and routes them to their specific websocket hubs.
// This SRE pattern consumes exactly ONE Redis connection regardless of the number of active clients.
func StartGlobalPubSubMultiplexer(ctx context.Context, redisClient *redis.Client, hub *Hub) {
	const globPattern = "noc:pubsub:tenant:*"
	pubsub := redisClient.PSubscribe(ctx, globPattern)
	defer pubsub.Close()

	log.Printf("SRE Pub/Sub Multiplexer active: listening on pattern '%s'", globPattern)
	ch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			// msg.Channel is "noc:pubsub:tenant:<tenant_uuid>"
			const prefix = "noc:pubsub:tenant:"
			if len(msg.Channel) > len(prefix) {
				tenantIDStr := msg.Channel[len(prefix):]
				tenantID, err := uuid.Parse(tenantIDStr)
				if err == nil {
					// Safely multicast to WebSocket clients in this tenant
					hub.BroadcastToTenant(tenantID, []byte(msg.Payload))
				}
			}
		}
	}
}
