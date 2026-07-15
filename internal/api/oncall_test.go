package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateCreateShift(t *testing.T) {
	sched := uuid.New()
	user := uuid.New()
	now := time.Now()
	later := now.Add(8 * time.Hour)

	cases := []struct {
		name    string
		req     CreateShiftRequest
		wantErr bool
	}{
		{"valid", CreateShiftRequest{ScheduleID: sched, UserID: user, StartsAt: now, EndsAt: later}, false},
		{"missing schedule", CreateShiftRequest{UserID: user, StartsAt: now, EndsAt: later}, true},
		{"missing user", CreateShiftRequest{ScheduleID: sched, StartsAt: now, EndsAt: later}, true},
		{"zero start", CreateShiftRequest{ScheduleID: sched, UserID: user, EndsAt: later}, true},
		{"zero end", CreateShiftRequest{ScheduleID: sched, UserID: user, StartsAt: now}, true},
		{"end before start", CreateShiftRequest{ScheduleID: sched, UserID: user, StartsAt: later, EndsAt: now}, true},
		{"end equals start", CreateShiftRequest{ScheduleID: sched, UserID: user, StartsAt: now, EndsAt: now}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateCreateShift(c.req)
			if (err != nil) != c.wantErr {
				t.Fatalf("validateCreateShift(%s) err=%v, wantErr=%v", c.name, err, c.wantErr)
			}
		})
	}
}
