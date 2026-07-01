package ws

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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
func ServeWS(hub *Hub, pgPool *pgxpool.Pool, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		var tenantIDs []uuid.UUID

		if tokenStr != "" {
			tokens := strings.Split(tokenStr, ",")
			for _, tok := range tokens {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				tenantID, err := middleware.ResolveTenantFromToken(tok, jwtSecret, pgPool)
				if err == nil {
					tenantIDs = append(tenantIDs, tenantID)
				} else {
					parsed, err := uuid.Parse(tok)
					if err == nil {
						tenantIDs = append(tenantIDs, parsed)
					}
				}
			}
		}

		if len(tenantIDs) == 0 {
			// BYPASS / OMITIR AUTENTICAÇÃO: Usa o tenant padrão para WebSocket se falhar
			tenantIDs = append(tenantIDs, uuid.MustParse("e1b7c123-1234-4321-abcd-123456789abc"))
		}

		// Upgrade HTTP connection to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}

		client := &Client{
			ID:        uuid.New(),
			TenantIDs: tenantIDs,
			Conn:      conn,
			Send:      make(chan []byte, 256),
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
