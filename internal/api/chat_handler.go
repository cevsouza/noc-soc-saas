package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

// HandleGetIncidentComments returns the chat timeline / comments for a specific incident.
func HandleGetIncidentComments(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		incidentIDStr := r.URL.Query().Get("incident_id")
		if incidentIDStr == "" {
			http.Error(w, "Missing incident_id parameter", http.StatusBadRequest)
			return
		}
		incidentID, err := uuid.Parse(incidentIDStr)
		if err != nil {
			http.Error(w, "Invalid incident_id format", http.StatusBadRequest)
			return
		}

		type CommentResponse struct {
			ID        uuid.UUID `json:"id"`
			Author    string    `json:"author"`
			Comment   string    `json:"comment"`
			CreatedAt time.Time `json:"created_at"`
		}

		list := make([]CommentResponse, 0)

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			query := `
				SELECT id, author, comment, created_at 
				FROM incident_comments 
				WHERE incident_id = $1 AND tenant_id = $2 
				ORDER BY created_at ASC
			`
			rows, err := tx.Query(ctx, query, incidentID, tenantID)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var c CommentResponse
				if err := rows.Scan(&c.ID, &c.Author, &c.Comment, &c.CreatedAt); err == nil {
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
	}
}
