package ws

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/middleware"
	"noc-api/internal/model"

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
// SECURITY: a valid, non-expired JWT is always required. Requesting to subscribe to
// tenants other than the caller's own (via ?tenants=) requires either platform-wide
// admin privileges (GlobalRole) or explicit membership in tenant_users — unauthorized
// tenant IDs in the list are silently dropped rather than granted. There is no longer
// a fallback that subscribes to every active tenant when no parameters are supplied.
func ServeWS(hub *Hub, pgPool *pgxpool.Pool, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		tenantsStr := r.URL.Query().Get("tenants")

		if tokenStr == "" {
			http.Error(w, "Unauthorized: missing token", http.StatusUnauthorized)
			return
		}

		operatorClaims, err := middleware.VerifyJWT(tokenStr, jwtSecret)
		if err != nil {
			http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
			return
		}
		if time.Now().Unix() > operatorClaims.Exp {
			http.Error(w, "Unauthorized: token expired", http.StatusUnauthorized)
			return
		}

		var tenantIDs []uuid.UUID
		if tenantsStr != "" {
			requested := strings.Split(tenantsStr, ",")
			for _, tok := range requested {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				parsed, err := uuid.Parse(tok)
				if err != nil {
					continue
				}
				if parsed == operatorClaims.TenantID || model.IsPlatformAdmin(operatorClaims.GlobalRole) {
					tenantIDs = append(tenantIDs, parsed)
					continue
				}
				member, err := isWSTenantMember(r.Context(), pgPool, operatorClaims.UserID, parsed)
				if err == nil && member {
					tenantIDs = append(tenantIDs, parsed)
				} else {
					log.Printf("[WS Security] User %s denied subscription to unauthorized tenant %s", operatorClaims.UserID, parsed)
				}
			}
			if len(tenantIDs) == 0 {
				http.Error(w, "Forbidden: not authorized for any of the requested tenants", http.StatusForbidden)
				return
			}
		} else {
			tenantIDs = append(tenantIDs, operatorClaims.TenantID)
		}

		// Upgrade HTTP connection to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}

		client := &Client{
			ID:          uuid.New(),
			TenantIDs:   tenantIDs,
			Conn:        conn,
			Send:        make(chan []byte, 256),
			UserID:      operatorClaims.UserID,
			Email:       operatorClaims.Email,
			Name:        strings.Split(operatorClaims.Email, "@")[0],
			Role:        string(operatorClaims.Role),
			ConnectedAt: time.Now(),
		}

		hub.register <- client

		// Spin up read and write loops
		go client.writePump()
		go client.readPump(hub)
	}
}

func isWSTenantMember(ctx context.Context, pgPool *pgxpool.Pool, userID, tenantID uuid.UUID) (bool, error) {
	var exists bool
	err := pgPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_users WHERE user_id = $1 AND tenant_id = $2)", userID, tenantID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// StartGlobalPubSubMultiplexer runs a single global subscription using PSUBSCRIBE
// that reads all events across all tenants and routes them to their specific websocket hubs.
// This SRE pattern consumes exactly ONE Redis connection regardless of the number of active clients.
func StartGlobalPubSubMultiplexer(ctx context.Context, redisClient *redis.Client, hub *Hub) {
	const globPattern = "noc:pubsub:tenant:*"

	for {
		select {
		case <-ctx.Done():
			return
		default:
			log.Printf("SRE Pub/Sub Multiplexer active: subscribing to pattern '%s'...", globPattern)
			pubsub := redisClient.PSubscribe(ctx, globPattern)
			ch := pubsub.Channel()

			func() {
				defer pubsub.Close()
				for {
					select {
					case <-ctx.Done():
						return
					case msg, ok := <-ch:
						if !ok || msg == nil {
							log.Printf("[Pub/Sub Warn] Redis Pub/Sub channel closed or nil. Reconnecting in 5s...")
							time.Sleep(5 * time.Second)
							return
						}

						const prefix = "noc:pubsub:tenant:"
						if len(msg.Channel) > len(prefix) {
							tenantIDStr := msg.Channel[len(prefix):]
							tenantID, err := uuid.Parse(tenantIDStr)
							if err == nil {
								hub.BroadcastToTenant(tenantID, []byte(msg.Payload))
							}
						}
					}
				}
			}()
		}
	}
}
