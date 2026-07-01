package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CreateShiftHandoverRequest struct {
	ShiftSummary       string `json:"shift_summary"`
	PendingAlertsCount int    `json:"pending_alerts_count"`
}

type AckShiftHandoverRequest struct {
	HandoverID uuid.UUID `json:"handover_id"`
}

// HandleCreateShiftHandover registers outgoing operator notes for shift change.
func HandleCreateShiftHandover(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: User claims missing", http.StatusUnauthorized)
			return
		}

		var req CreateShiftHandoverRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		if req.ShiftSummary == "" {
			http.Error(w, "Shift summary notes cannot be empty", http.StatusBadRequest)
			return
		}

		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			// 1. Fetch msp_id of the tenant
			var mspID uuid.UUID
			queryMSP := `SELECT msp_id FROM tenants WHERE id = $1`
			err := tx.QueryRow(ctx, queryMSP, tenantID).Scan(&mspID)
			if err != nil {
				return fmt.Errorf("failed to fetch msp_id: %w", err)
			}

			// 2. Insert shift handover
			queryInsert := `
				INSERT INTO shift_handovers (msp_id, outgoing_operator_id, shift_summary, pending_alerts_count, status)
				VALUES ($1, $2, $3, $4, 'pending')
			`
			_, err = tx.Exec(ctx, queryInsert, mspID, claims.UserID, req.ShiftSummary, req.PendingAlertsCount)
			return err
		})

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"success","message":"Shift handover created successfully"}`))
	}
}

// HandleGetCurrentShiftHandover fetches latest active handover notes for the operator entering.
func HandleGetCurrentShiftHandover(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var handover model.ShiftHandover
		var mspID uuid.UUID

		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			// 1. Get msp_id
			queryMSP := `SELECT msp_id FROM tenants WHERE id = $1`
			err := tx.QueryRow(ctx, queryMSP, tenantID).Scan(&mspID)
			if err != nil {
				return err
			}

			// 2. Query latest pending handover for this MSP
			queryHandover := `
				SELECT h.id, h.msp_id, h.outgoing_operator_id, h.shift_summary, h.pending_alerts_count, h.status, h.created_at, u.name
				FROM shift_handovers h
				JOIN users u ON h.outgoing_operator_id = u.id
				WHERE h.status = 'pending' AND h.msp_id = $1
				ORDER BY h.created_at DESC
				LIMIT 1
			`
			return tx.QueryRow(ctx, queryHandover, mspID).Scan(
				&handover.ID,
				&handover.MSPID,
				&handover.OutgoingOperatorID,
				&handover.ShiftSummary,
				&handover.PendingAlertsCount,
				&handover.Status,
				&handover.CreatedAt,
				&handover.OutgoingOperatorName,
			)
		})

		if err != nil {
			if err == pgx.ErrNoRows {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"none"}`))
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(handover)
	}
}

// HandleAcknowledgeShiftHandover confirms active handover and unlocks cockpit dashboard.
func HandleAcknowledgeShiftHandover(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: User claims missing", http.StatusUnauthorized)
			return
		}

		var req AckShiftHandoverRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			queryUpdate := `
				UPDATE shift_handovers 
				SET status = 'acknowledged', incoming_operator_id = $1, acknowledged_at = NOW() 
				WHERE id = $2 AND status = 'pending'
			`
			res, err := tx.Exec(ctx, queryUpdate, claims.UserID, req.HandoverID)
			if err != nil {
				return err
			}
			if res.RowsAffected() == 0 {
				return fmt.Errorf("shift handover not found, already acknowledged, or permission denied")
			}
			return nil
		})

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","message":"Shift handover acknowledged successfully. Dashboard unlocked."}`))
	}
}
