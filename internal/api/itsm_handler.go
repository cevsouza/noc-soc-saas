package api

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ITSMSyncRequest struct {
	IncidentID   uuid.UUID `json:"incident_id"`
	CreatedAt    time.Time `json:"created_at"`
	ITSMPlatform string    `json:"itsm_platform"` // "jira" or "servicenow"
}

type ITSMSyncResponse struct {
	TicketRef string `json:"ticket_ref"`
}

// HandleSyncITSM simulates a bidirectional ticket creation and association with Jira/ServiceNow.
func HandleSyncITSM(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req ITSMSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		if req.ITSMPlatform == "" {
			req.ITSMPlatform = "jira"
		}

		var ticketRef string
		// Generate simulated ticket reference
		rand.Seed(time.Now().UnixNano())
		num := rand.Intn(9000) + 1000
		if req.ITSMPlatform == "servicenow" {
			ticketRef = fmt.Sprintf("INC%d", num)
		} else {
			ticketRef = fmt.Sprintf("JIRA-%d", num)
		}

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			// 1. Update the alert ticket reference
			queryUpdate := `
				UPDATE alerts 
				SET itsm_ticket_ref = $1, updated_at = NOW() 
				WHERE id = $2 AND created_at = $3 AND tenant_id = $4
			`
			res, err := tx.Exec(ctx, queryUpdate, ticketRef, req.IncidentID, req.CreatedAt, tenantID)
			if err != nil {
				return fmt.Errorf("failed to update ticket ref: %w", err)
			}
			if res.RowsAffected() == 0 {
				return fmt.Errorf("incident not found or not owned by tenant")
			}

			// 2. Insert audit log comment in the timeline
			queryComment := `
				INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
				VALUES ($1, $2, 'Sistema', $3)
			`
			commentText := fmt.Sprintf("🎫 **Integração ITSM**: Chamado associado automaticamente no **%s** com a referência `%s`.", req.ITSMPlatform, ticketRef)
			_, err = tx.Exec(ctx, queryComment, req.IncidentID, tenantID, commentText)
			return err
		})

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ITSMSyncResponse{TicketRef: ticketRef})
	}
}
