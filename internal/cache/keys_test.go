package cache

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestTenantKey(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	if got := TenantKey(id, "heartbeat", "zabbix"); got != "tenant:11111111-1111-1111-1111-111111111111:heartbeat:zabbix" {
		t.Fatalf("TenantKey = %q", got)
	}
	if got := TenantKey(id); got != "tenant:11111111-1111-1111-1111-111111111111" {
		t.Fatalf("TenantKey (no parts) = %q", got)
	}
	if got := TenantScanPattern(id); got != "tenant:11111111-1111-1111-1111-111111111111:*" {
		t.Fatalf("TenantScanPattern = %q", got)
	}
}

func TestWipeTenantRemovesOnlyThatTenant(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx := context.Background()

	victim := uuid.New()
	bystander := uuid.New()

	// victim keys under the uniform prefix
	rc.Set(ctx, TenantKey(victim, "heartbeat", "zabbix"), "1", 0)
	rc.Set(ctx, TenantKey(victim, "suppression", "count"), "5", 0)
	rc.Set(ctx, TenantKey(victim, "ingest_breaker", "open"), "1", 0)
	// keys that must survive
	rc.Set(ctx, TenantKey(bystander, "heartbeat", "zabbix"), "1", 0)
	rc.Set(ctx, "noc:queue:alerts:normalized", "x", 0)

	removed, err := WipeTenant(ctx, rc, victim)
	if err != nil {
		t.Fatalf("WipeTenant error: %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	if n, _ := rc.Exists(ctx, TenantKey(victim, "heartbeat", "zabbix")).Result(); n != 0 {
		t.Fatal("victim key survived")
	}
	if n, _ := rc.Exists(ctx, TenantKey(bystander, "heartbeat", "zabbix")).Result(); n != 1 {
		t.Fatal("bystander key was wrongly deleted")
	}
	if n, _ := rc.Exists(ctx, "noc:queue:alerts:normalized").Result(); n != 1 {
		t.Fatal("shared queue key was wrongly deleted")
	}
}

func TestGetWithLegacyFallback(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx := context.Background()

	// Only the legacy key exists → fallback returns it.
	rc.Set(ctx, "legacy", "42", 0)
	if v, err := GetWithLegacyFallback(ctx, rc, "new", "legacy"); err != nil || v != "42" {
		t.Fatalf("fallback: got %q,%v want 42,nil", v, err)
	}
	// New key present → new key wins.
	rc.Set(ctx, "new", "99", 0)
	if v, _ := GetWithLegacyFallback(ctx, rc, "new", "legacy"); v != "99" {
		t.Fatalf("new key should win, got %q", v)
	}
}
