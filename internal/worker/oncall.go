package worker

import (
	"context"
	"fmt"
	"strings"

	"noc-api/internal/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// On-call routing (B5 slice 2). When an incident breaches SLA, the page should tell responders WHO is
// on point right now — not just that a breach happened. This resolves the tenant's currently-on-call
// person(s) (the shift covering NOW() across all of the tenant's schedules) and formats a compact
// suffix that gets appended to the escalation summary, so it surfaces unchanged in every channel
// (Slack/Teams/e-mail/PagerDuty/Opsgenie) without touching the notifier signatures.
//
// v1 routes by name + e-mail (the contact we already hold). Direct SMS/phone paging is a documented
// future extension — it needs a per-user contact field with an edit UI, and (for e-mail-to-person)
// SMTP/Resend configured in production.

// onCallPerson is a currently-on-call assignee.
type onCallPerson struct {
	Name  string
	Email string
}

// currentOnCall returns the distinct people whose shift covers NOW() across all of the tenant's
// on-call schedules. Runs under the tenant RLS context (oncall_shifts is tenant-scoped; users global).
func (wp *WorkerPool) currentOnCall(ctx context.Context, tenantID uuid.UUID) ([]onCallPerson, error) {
	tenantCtx := db.WithTenantID(ctx, tenantID)
	var people []onCallPerson
	err := db.ExecuteInTenantTx(tenantCtx, wp.pgPool, func(tx pgx.Tx) error {
		rows, e := tx.Query(tenantCtx, `
			SELECT DISTINCT u.name, u.email
			FROM oncall_shifts sh
			JOIN users u ON u.id = sh.user_id
			WHERE sh.tenant_id = $1 AND sh.starts_at <= NOW() AND sh.ends_at > NOW()
			ORDER BY u.name
		`, tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var p onCallPerson
			if e := rows.Scan(&p.Name, &p.Email); e != nil {
				return e
			}
			people = append(people, p)
		}
		return rows.Err()
	})
	return people, err
}

// formatOnCallSuffix renders the on-call people into a compact page suffix (pure, unit-tested). Empty
// when nobody is on-call. Caps at 3 names to keep channel messages readable.
func formatOnCallSuffix(people []onCallPerson) string {
	if len(people) == 0 {
		return ""
	}
	const maxNames = 3
	parts := make([]string, 0, maxNames)
	for i, p := range people {
		if i >= maxNames {
			parts = append(parts, fmt.Sprintf("+%d", len(people)-maxNames))
			break
		}
		if p.Email != "" {
			parts = append(parts, fmt.Sprintf("%s <%s>", p.Name, p.Email))
		} else {
			parts = append(parts, p.Name)
		}
	}
	return " · Plantão: " + strings.Join(parts, ", ")
}
