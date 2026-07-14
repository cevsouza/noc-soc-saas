package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CommentResponse is one incident_comments row as returned by the incident timeline endpoints.
type CommentResponse struct {
	ID        uuid.UUID `json:"id"`
	Author    string    `json:"author"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// AddCommentRequest is the body of POST /api/v1/incidents/comments (a plain investigation note).
type AddCommentRequest struct {
	IncidentID uuid.UUID `json:"incident_id"`
	Comment    string    `json:"comment"`
}

type IncidentChatRequest struct {
	IncidentID uuid.UUID `json:"incident_id"`
	CreatedAt  time.Time `json:"created_at"`
	Prompt     string    `json:"prompt"`
}

type IncidentChatResponse struct {
	Response string `json:"response"`
}

// HandleIncidentChat receives questions from operators about a specific incident,
// queries Gemini with context and message history, saves both to incident_comments and returns response.
func HandleIncidentChat(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req IncidentChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		if req.Prompt == "" {
			http.Error(w, "Prompt cannot be empty", http.StatusBadRequest)
			return
		}

		var geminiResponse string

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			// 1. Fetch incident details for context
			var title, payloadBytes, aiAnalysisBytes []byte
			queryIncident := `
				SELECT summary, payload, ai_analysis 
				FROM alerts 
				WHERE id = $1 AND created_at = $2 AND tenant_id = $3
			`
			err := tx.QueryRow(ctx, queryIncident, req.IncidentID, req.CreatedAt, tenantID).Scan(&title, &payloadBytes, &aiAnalysisBytes)
			if err != nil {
				return fmt.Errorf("failed to fetch incident: %w", err)
			}

			// 2. Fetch comments history
			queryHistory := `
				SELECT author, comment 
				FROM incident_comments 
				WHERE incident_id = $1 AND tenant_id = $2 
				ORDER BY created_at ASC 
				LIMIT 20
			`
			rows, err := tx.Query(ctx, queryHistory, req.IncidentID, tenantID)
			if err != nil {
				return fmt.Errorf("failed to fetch comments history: %w", err)
			}
			defer rows.Close()

			historyStr := ""
			for rows.Next() {
				var author, comment string
				if err := rows.Scan(&author, &comment); err == nil {
					historyStr += fmt.Sprintf("[%s]: %s\n", author, comment)
				}
			}

			// 3. Save operator prompt to database timeline
			queryInsertComment := `
				INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
				VALUES ($1, $2, 'Operador', $3)
			`
			_, err = tx.Exec(ctx, queryInsertComment, req.IncidentID, tenantID, req.Prompt)
			if err != nil {
				return fmt.Errorf("failed to save operator prompt: %w", err)
			}

			// 4. Call Gemini API
			response, err := ChatWithIncident(ctx, string(title), "Servidor Remoto", string(payloadBytes), historyStr, req.Prompt)
			if err != nil {
				return fmt.Errorf("failed to chat with Gemini: %w", err)
			}
			geminiResponse = response

			// 5. Save Gemini response to database timeline
			_, err = tx.Exec(ctx, queryInsertComment, req.IncidentID, tenantID, "🤖 Co-Pilot AI: "+geminiResponse)
			if err != nil {
				return fmt.Errorf("failed to save AI response: %w", err)
			}

			return nil
		})

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(IncidentChatResponse{Response: geminiResponse})
	}
}

// HandleIncidentComments serves an incident's investigation timeline (incident_comments). Single
// route, method-dispatched (avoids a second ServeMux registration on the same path):
//   - GET  ?incident_id=  → list the comments (ascending).
//   - POST {incident_id, comment} → append a plain investigation note authored by the current user.
//
// B1 fatia 2: notes live on the real incident (the grouped investigation), the same id the worker
// now stamps on SOAR/approval artifacts — so a human's notes and the automation's audit trail share
// one timeline. Any authenticated member of the tenant may read and post (collaborative, low-risk).
func HandleIncidentComments(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		switch r.Method {
		case http.MethodGet:
			incidentIDStr := r.URL.Query().Get("incident_id")
			if incidentIDStr == "" {
				http.Error(w, "Missing incident_id parameter", http.StatusBadRequest)
				return
			}
			incidentID, perr := uuid.Parse(incidentIDStr)
			if perr != nil {
				http.Error(w, "Invalid incident_id format", http.StatusBadRequest)
				return
			}

			list := make([]CommentResponse, 0)
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				rows, e := tx.Query(ctx, `
					SELECT id, author, comment, created_at
					FROM incident_comments
					WHERE incident_id = $1 AND tenant_id = $2
					ORDER BY created_at ASC
				`, incidentID, tenantID)
				if e != nil {
					return e
				}
				defer rows.Close()
				for rows.Next() {
					var c CommentResponse
					if e := rows.Scan(&c.ID, &c.Author, &c.Comment, &c.CreatedAt); e == nil {
						list = append(list, c)
					}
				}
				return rows.Err()
			})
			if err != nil {
				http.Error(w, "Failed to query comments", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)

		case http.MethodPost:
			var req AddCommentRequest
			if derr := json.NewDecoder(r.Body).Decode(&req); derr != nil || req.IncidentID == uuid.Nil {
				http.Error(w, "Invalid payload: incident_id and comment are required", http.StatusBadRequest)
				return
			}
			req.Comment = strings.TrimSpace(req.Comment)
			if req.Comment == "" {
				http.Error(w, "Bad Request: comment cannot be empty", http.StatusBadRequest)
				return
			}
			if len(req.Comment) > 4000 {
				http.Error(w, "Bad Request: comment too long (max 4000)", http.StatusBadRequest)
				return
			}

			claims, _ := middleware.ClaimsFromContext(r.Context())
			author := "Operador"
			var actorID uuid.UUID
			if claims != nil {
				actorID = claims.UserID
				if claims.Email != "" {
					author = claims.Email
				}
			}

			var created CommentResponse
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				// Guard: only comment on an incident that belongs to this tenant (defends against a
				// forged/foreign incident_id — the bare column has no FK to incidents).
				var exists bool
				if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM incidents WHERE id = $1 AND tenant_id = $2)`, req.IncidentID, tenantID).Scan(&exists); e != nil {
					return e
				}
				if !exists {
					return errIncidentNotFound
				}
				return tx.QueryRow(ctx, `
					INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
					VALUES ($1, $2, $3, $4)
					RETURNING id, author, comment, created_at
				`, req.IncidentID, tenantID, author, req.Comment).Scan(&created.ID, &created.Author, &created.Comment, &created.CreatedAt)
			})
			if err == errIncidentNotFound {
				http.Error(w, "Incident not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, "Failed to save comment", http.StatusInternalServerError)
				return
			}
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: "incident.comment", Resource: req.IncidentID.String(), IPAddress: r.RemoteAddr})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(created)

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	}
}

// errIncidentNotFound signals a POST against an incident that isn't this tenant's.
var errIncidentNotFound = fmt.Errorf("incident not found")
