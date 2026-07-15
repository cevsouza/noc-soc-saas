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

// On-call scheduling (B5 slice 1). A SCHEDULE is a named rotation; a SHIFT assigns a user to cover a
// window [starts_at, ends_at). "Who is on-call now" for a schedule is the shift whose window contains
// NOW() (latest starts_at wins on overlap). This slice is management + visibility; person-level
// routing/escalation is a future slice. All queries run under the tenant RLS context.

// OncallSchedule is a rotation plus its current on-call assignee (nil if nobody is covering now).
type OncallSchedule struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	CreatedAt   time.Time  `json:"created_at"`
	OnCallUser  *uuid.UUID `json:"oncall_user_id,omitempty"`
	OnCallName  string     `json:"oncall_name,omitempty"`
	OnCallEmail string     `json:"oncall_email,omitempty"`
	OnCallUntil *time.Time `json:"oncall_until,omitempty"`
}

// OncallShift is one assignment window within a schedule, with the assignee's display fields.
type OncallShift struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	UserName  string    `json:"user_name"`
	UserEmail string    `json:"user_email"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
}

// CreateScheduleRequest is the POST /oncall/schedules body.
type CreateScheduleRequest struct {
	Name string `json:"name"`
}

// CreateShiftRequest is the POST /oncall/shifts body.
type CreateShiftRequest struct {
	ScheduleID uuid.UUID `json:"schedule_id"`
	UserID     uuid.UUID `json:"user_id"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
}

// validateCreateShift checks a shift request without touching the DB (unit-testable).
func validateCreateShift(req CreateShiftRequest) error {
	if req.ScheduleID == uuid.Nil {
		return fmt.Errorf("schedule_id is required")
	}
	if req.UserID == uuid.Nil {
		return fmt.Errorf("user_id is required")
	}
	if req.StartsAt.IsZero() || req.EndsAt.IsZero() {
		return fmt.Errorf("starts_at and ends_at are required")
	}
	if !req.EndsAt.After(req.StartsAt) {
		return fmt.Errorf("ends_at must be after starts_at")
	}
	return nil
}

// HandleGetOncallSchedules lists the tenant's schedules, each with its current on-call assignee.
func HandleGetOncallSchedules(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		list := make([]OncallSchedule, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			// LATERAL join picks the single shift covering NOW() (latest start on overlap) per schedule.
			rows, e := tx.Query(ctx, `
				SELECT s.id, s.name, s.created_at,
				       cur.user_id, COALESCE(cur.user_name, ''), COALESCE(cur.user_email, ''), cur.ends_at
				FROM oncall_schedules s
				LEFT JOIN LATERAL (
					SELECT sh.user_id, u.name AS user_name, u.email AS user_email, sh.ends_at
					FROM oncall_shifts sh
					JOIN users u ON u.id = sh.user_id
					WHERE sh.schedule_id = s.id AND sh.tenant_id = $1
					  AND sh.starts_at <= NOW() AND sh.ends_at > NOW()
					ORDER BY sh.starts_at DESC
					LIMIT 1
				) cur ON true
				WHERE s.tenant_id = $1
				ORDER BY s.name
			`, tenantID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var s OncallSchedule
				if e := rows.Scan(&s.ID, &s.Name, &s.CreatedAt, &s.OnCallUser, &s.OnCallName, &s.OnCallEmail, &s.OnCallUntil); e != nil {
					return e
				}
				list = append(list, s)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to query on-call schedules", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleMutateOncallSchedules creates (POST) or deletes (DELETE ?id=) a schedule. Deleting a schedule
// cascades its shifts (FK ON DELETE CASCADE). Gated to tenant admins at the route level.
func HandleMutateOncallSchedules(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)
		var actorID uuid.UUID
		if claims != nil {
			actorID = claims.UserID
		}

		switch r.Method {
		case http.MethodPost:
			var req CreateScheduleRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request payload", http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(req.Name) == "" {
				http.Error(w, "Bad Request: name is required", http.StatusBadRequest)
				return
			}
			var newID uuid.UUID
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `
					INSERT INTO oncall_schedules (tenant_id, name) VALUES ($1, $2) RETURNING id
				`, tenantID, strings.TrimSpace(req.Name)).Scan(&newID)
			})
			if err != nil {
				http.Error(w, "Failed to create schedule", http.StatusInternalServerError)
				return
			}
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: "oncall.schedule.create", Resource: req.Name, IPAddress: r.RemoteAddr})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": newID.String()})

		case http.MethodDelete:
			id, perr := uuid.Parse(r.URL.Query().Get("id"))
			if perr != nil {
				http.Error(w, "Bad Request: valid id is required", http.StatusBadRequest)
				return
			}
			var affected int64
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				res, e := tx.Exec(ctx, `DELETE FROM oncall_schedules WHERE id = $1 AND tenant_id = $2`, id, tenantID)
				if e != nil {
					return e
				}
				affected = res.RowsAffected()
				return nil
			})
			if err != nil {
				http.Error(w, "Failed to delete schedule", http.StatusInternalServerError)
				return
			}
			if affected == 0 {
				http.Error(w, "Schedule not found", http.StatusNotFound)
				return
			}
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: "oncall.schedule.delete", Resource: id.String(), IPAddress: r.RemoteAddr})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	}
}

// HandleGetOncallShifts lists the shifts of one schedule (?schedule_id=), newest window first.
func HandleGetOncallShifts(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		scheduleID, perr := uuid.Parse(r.URL.Query().Get("schedule_id"))
		if perr != nil {
			http.Error(w, "Bad Request: valid schedule_id is required", http.StatusBadRequest)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		list := make([]OncallShift, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `
				SELECT sh.id, sh.user_id, COALESCE(u.name, ''), COALESCE(u.email, ''), sh.starts_at, sh.ends_at
				FROM oncall_shifts sh
				JOIN users u ON u.id = sh.user_id
				WHERE sh.schedule_id = $1 AND sh.tenant_id = $2
				ORDER BY sh.starts_at DESC
			`, scheduleID, tenantID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var s OncallShift
				if e := rows.Scan(&s.ID, &s.UserID, &s.UserName, &s.UserEmail, &s.StartsAt, &s.EndsAt); e != nil {
					return e
				}
				list = append(list, s)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to query shifts", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleMutateOncallShifts adds (POST) or removes (DELETE ?id=) a shift. Gated to tenant admins.
func HandleMutateOncallShifts(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)
		var actorID uuid.UUID
		if claims != nil {
			actorID = claims.UserID
		}

		switch r.Method {
		case http.MethodPost:
			var req CreateShiftRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request payload", http.StatusBadRequest)
				return
			}
			if verr := validateCreateShift(req); verr != nil {
				http.Error(w, "Bad Request: "+verr.Error(), http.StatusBadRequest)
				return
			}
			var newID uuid.UUID
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				// Confirm the schedule belongs to this tenant (RLS scopes the SELECT) before inserting —
				// the shift's FK to oncall_schedules bypasses RLS, so validate membership explicitly.
				var exists bool
				if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM oncall_schedules WHERE id = $1 AND tenant_id = $2)`, req.ScheduleID, tenantID).Scan(&exists); e != nil {
					return e
				}
				if !exists {
					return errScheduleNotFound
				}
				return tx.QueryRow(ctx, `
					INSERT INTO oncall_shifts (tenant_id, schedule_id, user_id, starts_at, ends_at)
					VALUES ($1, $2, $3, $4, $5) RETURNING id
				`, tenantID, req.ScheduleID, req.UserID, req.StartsAt, req.EndsAt).Scan(&newID)
			})
			if err == errScheduleNotFound {
				http.Error(w, "Schedule not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, "Failed to create shift", http.StatusInternalServerError)
				return
			}
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: "oncall.shift.create", Resource: req.ScheduleID.String(), IPAddress: r.RemoteAddr})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": newID.String()})

		case http.MethodDelete:
			id, perr := uuid.Parse(r.URL.Query().Get("id"))
			if perr != nil {
				http.Error(w, "Bad Request: valid id is required", http.StatusBadRequest)
				return
			}
			var affected int64
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				res, e := tx.Exec(ctx, `DELETE FROM oncall_shifts WHERE id = $1 AND tenant_id = $2`, id, tenantID)
				if e != nil {
					return e
				}
				affected = res.RowsAffected()
				return nil
			})
			if err != nil {
				http.Error(w, "Failed to delete shift", http.StatusInternalServerError)
				return
			}
			if affected == 0 {
				http.Error(w, "Shift not found", http.StatusNotFound)
				return
			}
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: "oncall.shift.delete", Resource: id.String(), IPAddress: r.RemoteAddr})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	}
}

// errScheduleNotFound signals a shift referencing a schedule outside the caller's tenant.
var errScheduleNotFound = fmt.Errorf("schedule not found")
