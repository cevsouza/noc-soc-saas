package api

import (
	"testing"

	"github.com/google/uuid"
)

func TestAggregateAnalystRows(t *testing.T) {
	u1 := uuid.New()
	u2 := uuid.New()
	rows := []analystActionRow{
		{UserID: u1, Name: "Ana", Email: "ana@x", Action: "runbook.approve", Count: 2},
		{UserID: u1, Name: "Ana", Email: "ana@x", Action: "response.reject", Count: 5},
		{UserID: u2, Name: "Bruno", Email: "bruno@x", Action: "vault.secret.save", Count: 3},
	}
	analysts, total := aggregateAnalystRows(rows)

	if total != 10 {
		t.Fatalf("total = %d, want 10", total)
	}
	if len(analysts) != 2 {
		t.Fatalf("len(analysts) = %d, want 2", len(analysts))
	}
	// Ana (7) must sort before Bruno (3).
	if analysts[0].UserID != u1 || analysts[0].TotalActions != 7 {
		t.Errorf("analysts[0] = %+v, want Ana with 7", analysts[0])
	}
	if analysts[1].UserID != u2 || analysts[1].TotalActions != 3 {
		t.Errorf("analysts[1] = %+v, want Bruno with 3", analysts[1])
	}
	// Ana's by_action must be sorted most-frequent first (response.reject 5 > runbook.approve 2).
	if len(analysts[0].ByAction) != 2 || analysts[0].ByAction[0].Action != "response.reject" {
		t.Errorf("Ana by_action not sorted desc: %+v", analysts[0].ByAction)
	}
}

func TestAggregateAnalystRowsEmpty(t *testing.T) {
	analysts, total := aggregateAnalystRows(nil)
	if total != 0 || len(analysts) != 0 {
		t.Errorf("empty input = (%d, %d analysts), want (0, 0)", total, len(analysts))
	}
}
