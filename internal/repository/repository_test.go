package repository_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/model"
	"noc-api/internal/repository"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// getTestPool initialized the database pool using environment variables or defaults.
//
// IMPORTANT: DB_USER/DB_PASSWORD should point at the non-superuser "noc_app_runtime" role
// (see internal/db/connection.go's SetupAppRuntimeRole and migration 000012), not at the
// migration-owner superuser. FORCE ROW LEVEL SECURITY still exempts a superuser connection
// from RLS, so running these tests against one wouldn't prove anything about tenant
// isolation — it would just silently skip the actual security check while still "passing".
// See .github/workflows/ci.yml for the reference setup: migrations run once as the admin
// user (provisioning noc_app_runtime), then `go test` itself connects as noc_app_runtime.
func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	portStr := os.Getenv("DB_PORT")
	port := 5432
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}
	user := os.Getenv("DB_USER")
	if user == "" {
		user = "postgres"
	}
	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		password = "postgres"
	}
	dbname := os.Getenv("DB_NAME")
	if dbname == "" {
		dbname = "noc_test"
	}
	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {
		sslmode = "disable"
	}

	cfg := db.Config{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		DBName:   dbname,
		SSLMode:  sslmode,
	}

	ctx := context.Background()
	pool, err := db.NewConnectionPool(ctx, cfg)
	if err != nil {
		t.Skipf("Skipping integration test: PostgreSQL connection failed: %v", err)
	}

	// NewConnectionPool never dials eagerly (pgxpool defers the actual TCP connection), so it
	// "succeeds" even when Postgres isn't reachable — without this Ping, the real connection
	// error only surfaces on the first live query as a t.Fatalf, not the intended graceful skip.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Skipping integration test: PostgreSQL ping failed: %v", err)
	}

	return pool
}

// TestTenantIsolation validates that Row-Level Security (RLS) is strictly enforced
// across different tenants for both Devices and partitioned Alerts.
func TestTenantIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// 1. Setup Test Tenants (Global Setup - Bypass RLS)
	// We run this outside of tenant-specific contexts to register the tenants.
	tenantAID := uuid.New()
	tenantBID := uuid.New()

	_, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Tenant A', 'tenant-a', 'active')", tenantAID)
	if err != nil {
		t.Fatalf("failed to insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantAID)

	_, err = pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Tenant B', 'tenant-b', 'active')", tenantBID)
	if err != nil {
		t.Fatalf("failed to insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantBID)

	deviceRepo := repository.NewPostgresDeviceRepository()
	alertRepo := repository.NewPostgresAlertRepository()

	// 2. Validate Tenant A Operations
	ctxA := db.WithTenantID(ctx, tenantAID)

	var deviceA *model.Device
	var alertA *model.Alert
	alertTime := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC) // Targets 2026-05 partition

	err = db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		// Create Device in Tenant A
		deviceA = &model.Device{
			Name:      "Router-A-01",
			IPAddress: "192.168.1.1",
			Type:      "router",
			Status:    model.DeviceOnline,
			Metadata:  map[string]interface{}{"firmware": "1.0.4"},
		}
		if err := deviceRepo.Create(ctxA, tx, deviceA); err != nil {
			return err
		}

		// Create Alert in Tenant A
		alertA = &model.Alert{
			DeviceID:  &deviceA.ID,
			EventType: "cpu",
			Severity:  model.SeverityWarning,
			Status:    model.AlertTriggered,
			Summary:   "CPU utilization high",
			Payload:   map[string]interface{}{"value": 85},
			CreatedAt: alertTime,
		}
		if err := alertRepo.Create(ctxA, tx, alertA); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		t.Fatalf("Tenant A setup transaction failed: %v", err)
	}

	// 3. Validate Tenant B Operations & Cross-tenant Isolation
	ctxB := db.WithTenantID(ctx, tenantBID)

	err = db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		// Create Device in Tenant B
		deviceB := &model.Device{
			Name:      "Switch-B-01",
			IPAddress: "10.0.0.1",
			Type:      "switch",
			Status:    model.DeviceOnline,
			Metadata:  map[string]interface{}{"vlan_count": 24},
		}
		if err := deviceRepo.Create(ctxB, tx, deviceB); err != nil {
			return err
		}

		// ASSERTION: Tenant B cannot list Tenant A's devices
		devices, err := deviceRepo.List(ctxB, tx)
		if err != nil {
			return err
		}

		for _, d := range devices {
			if d.ID == deviceA.ID {
				t.Errorf("SECURITY BREACH: Tenant B listed Tenant A's device")
			}
		}

		// ASSERTION: Tenant B cannot read Tenant A's device by ID
		_, err = deviceRepo.GetByID(ctxB, tx, deviceA.ID)
		if err == nil {
			t.Errorf("SECURITY BREACH: Tenant B successfully fetched Tenant A's device by ID")
		}

		// ASSERTION: Tenant B cannot update Tenant A's device
		deviceA.Name = "Spoofed-Name"
		err = deviceRepo.Update(ctxB, tx, deviceA)
		if err == nil {
			t.Errorf("SECURITY BREACH: Tenant B updated Tenant A's device")
		}

		// ASSERTION: Tenant B cannot delete Tenant A's device
		err = deviceRepo.Delete(ctxB, tx, deviceA.ID)
		if err == nil {
			t.Errorf("SECURITY BREACH: Tenant B deleted Tenant A's device")
		}

		// ASSERTION: Tenant B cannot query Tenant A's alert
		_, err = alertRepo.GetByID(ctxB, tx, alertA.ID, alertTime)
		if err == nil {
			t.Errorf("SECURITY BREACH: Tenant B successfully fetched Tenant A's alert by ID")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("Tenant B transaction failed: %v", err)
	}

	// 4. Validate Tenant A Listing & Specific Actions
	err = db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		// Assert Tenant A only lists its own device
		devices, err := deviceRepo.List(ctxA, tx)
		if err != nil {
			return err
		}
		if len(devices) != 1 || devices[0].ID != deviceA.ID {
			t.Errorf("Tenant A device list incorrect: expected [Router-A-01], got %v", devices)
		}

		// Assert Tenant A can fetch its own alert
		fetchedAlert, err := alertRepo.GetByID(ctxA, tx, alertA.ID, alertTime)
		if err != nil {
			return err
		}
		if fetchedAlert.ID != alertA.ID || fetchedAlert.Summary != "CPU utilization high" {
			t.Errorf("Tenant A fetched alert is incorrect: %+v", fetchedAlert)
		}

		// Resolve Alert
		err = alertRepo.UpdateStatus(ctxA, tx, alertA.ID, alertTime, model.AlertResolved)
		if err != nil {
			return err
		}

		resolvedAlert, err := alertRepo.GetByID(ctxA, tx, alertA.ID, alertTime)
		if err != nil {
			return err
		}
		if resolvedAlert.Status != model.AlertResolved || resolvedAlert.ResolvedAt == nil {
			t.Errorf("Alert resolution state failed to persist correctly")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("Tenant A validation transaction failed: %v", err)
	}
}

func TestTenantVault(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	// Setup Master Key for encryption test
	masterKeyStr := "12345678901234567890123456789012" // 32 bytes
	t.Setenv("VAULT_MASTER_KEY", masterKeyStr)
	masterKey, err := security.GetMasterKey()
	if err != nil {
		t.Fatalf("failed to retrieve master key: %v", err)
	}

	tenantAID := uuid.New()
	tenantBID := uuid.New()

	_, err = pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Tenant A Vault', 'tenant-a-vault', 'active')", tenantAID)
	if err != nil {
		t.Fatalf("failed to insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantAID)

	_, err = pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Tenant B Vault', 'tenant-b-vault', 'active')", tenantBID)
	if err != nil {
		t.Fatalf("failed to insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantBID)

	vaultRepo := repository.NewPostgresVaultRepository()

	// 1. Tenant A writes a secret
	ctxA := db.WithTenantID(ctx, tenantAID)
	plainText := []byte("super-secret-ssh-key")
	cipherText, nonce, err := security.Encrypt(plainText, masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt secret: %v", err)
	}

	err = db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		secret := &model.VaultSecret{
			SecretKey:      "ssh_private_key",
			EncryptedValue: cipherText,
			Nonce:          nonce,
			Description:    "SSH private key for jumpbox",
		}
		return vaultRepo.CreateSecret(ctxA, tx, secret)
	})
	if err != nil {
		t.Fatalf("Tenant A failed to store secret: %v", err)
	}

	// 2. Tenant B tries to fetch Tenant A's secret (should fail / not found)
	ctxB := db.WithTenantID(ctx, tenantBID)
	err = db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		_, err = vaultRepo.GetSecretByKey(ctxB, tx, "ssh_private_key")
		if err == nil {
			t.Errorf("SECURITY BREACH: Tenant B successfully fetched Tenant A's secret")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tenant B transaction failed: %v", err)
	}

	// 3. Tenant A fetches its own secret and decrypts it
	err = db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		secret, err := vaultRepo.GetSecretByKey(ctxA, tx, "ssh_private_key")
		if err != nil {
			return err
		}

		decrypted, err := security.Decrypt(secret.EncryptedValue, secret.Nonce, masterKey)
		if err != nil {
			return fmt.Errorf("failed to decrypt secret: %w", err)
		}

		if string(decrypted) != string(plainText) {
			t.Errorf("decrypted secret does not match original, expected '%s', got '%s'", string(plainText), string(decrypted))
		}

		// Update secret
		newPlainText := []byte("new-ssh-key-value")
		newCipherText, newNonce, err := security.Encrypt(newPlainText, masterKey)
		if err != nil {
			return err
		}

		secret.EncryptedValue = newCipherText
		secret.Nonce = newNonce
		err = vaultRepo.UpdateSecret(ctxA, tx, secret)
		if err != nil {
			return err
		}

		// Fetch updated secret
		updatedSecret, err := vaultRepo.GetSecretByKey(ctxA, tx, "ssh_private_key")
		if err != nil {
			return err
		}

		decryptedUpdated, err := security.Decrypt(updatedSecret.EncryptedValue, updatedSecret.Nonce, masterKey)
		if err != nil {
			return err
		}

		if string(decryptedUpdated) != string(newPlainText) {
			t.Errorf("decrypted updated secret does not match, got '%s'", string(decryptedUpdated))
		}

		// Delete secret
		err = vaultRepo.DeleteSecret(ctxA, tx, "ssh_private_key")
		if err != nil {
			return err
		}

		_, err = vaultRepo.GetSecretByKey(ctxA, tx, "ssh_private_key")
		if err == nil {
			t.Errorf("secret was not deleted")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("Tenant A verification transaction failed: %v", err)
	}
}

// TestAuditAppendOnly validates migration 000017: the application role can INSERT audit rows but
// can neither UPDATE nor DELETE them (write-once), so a compromised app cannot cover its tracks.
// Runs meaningfully only when connected as the non-superuser noc_app_runtime role (CI); a
// superuser/owner connection would be allowed to DELETE by design, so the DELETE assertion there
// is skipped. Cleanup can't remove the tenant (cascade delete of audit rows is blocked for the app
// role) — harmless on the ephemeral CI database.
func TestAuditAppendOnly(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantID := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Audit AppendOnly', 'audit-append-only', 'active')", tenantID); err != nil {
		t.Fatalf("failed to insert tenant: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantID) // may be blocked by the append-only trigger; ignored

	tctx := db.WithTenantID(ctx, tenantID)

	// INSERT must succeed.
	var auditID uuid.UUID
	if err := db.ExecuteInTenantTx(tctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(tctx, `
			INSERT INTO audit_logs (tenant_id, action, resource, details)
			VALUES ($1, 'test.action', 'test-resource', '{}')
			RETURNING id
		`, tenantID).Scan(&auditID)
	}); err != nil {
		t.Fatalf("INSERT into audit_logs should succeed: %v", err)
	}

	// UPDATE must be rejected (rows are immutable for everyone).
	updErr := db.ExecuteInTenantTx(tctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(tctx, `UPDATE audit_logs SET action = 'tampered' WHERE id = $1`, auditID)
		return err
	})
	if updErr == nil {
		t.Errorf("SECURITY: UPDATE on audit_logs should have been rejected (append-only)")
	}

	// DELETE must be rejected for the application role. If the test runs as a superuser/owner
	// (local default), DELETE is allowed by design — detect that and skip the assertion.
	var currentUser string
	_ = pool.QueryRow(ctx, "SELECT current_user").Scan(&currentUser)
	delErr := db.ExecuteInTenantTx(tctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(tctx, `DELETE FROM audit_logs WHERE id = $1`, auditID)
		return err
	})
	if currentUser == "noc_app_runtime" {
		if delErr == nil {
			t.Errorf("SECURITY: DELETE on audit_logs should have been rejected for the application role")
		}
	} else {
		t.Logf("connected as %q (not noc_app_runtime): DELETE is allowed by design, skipping DELETE assertion", currentUser)
	}
}

// TestTenantSLAIsolation is the cross-tenant leak test for the new tenant_sla table (migration
// 000018): tenant A writes an SLA override, tenant B must not be able to read it under RLS.
func TestTenantSLAIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'SLA A', 'sla-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'SLA B', 'sla-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	// Tenant A writes a custom SLA target for 'critical' inside its RLS context.
	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO tenant_sla (tenant_id, severity, mtta_target_minutes, mttr_target_minutes) VALUES ($1, 'critical', 7, 21)`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to write SLA: %v", err)
	}

	// Tenant B must see zero rows (RLS blocks cross-tenant reads).
	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM tenant_sla`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d tenant_sla row(s) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}

	// Tenant A can read its own row back.
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		var mttr float64
		if err := tx.QueryRow(ctxA, `SELECT mttr_target_minutes FROM tenant_sla WHERE severity = 'critical'`).Scan(&mttr); err != nil {
			return err
		}
		if mttr != 21 {
			t.Errorf("tenant A read back mttr=%v, want 21", mttr)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant A read-back failed: %v", err)
	}
}

// TestIncidentIsolation is the cross-tenant leak test for the incidents table (migration 000019):
// an incident created by tenant A must be invisible to tenant B under RLS.
func TestIncidentIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Inc A', 'inc-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Inc B', 'inc-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO incidents (tenant_id, fingerprint, title, severity) VALUES ($1, 'fp-abc', 'DB down', 'critical')`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create incident: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM incidents`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d incident(s) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}

// TestSuppressionRuleIsolation is the cross-tenant leak test for tenant_suppression_rules
// (migration 000021): a rule created by tenant A must be invisible to tenant B under RLS.
func TestSuppressionRuleIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Sup A', 'sup-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Sup B', 'sup-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO tenant_suppression_rules (tenant_id, name, match_field, match_value) VALUES ($1, 'maint', 'host', 'web-01')`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create suppression rule: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM tenant_suppression_rules`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d suppression rule(s) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}

// TestTenantRetentionIsolation is the mandatory cross-tenant leak test for the tenant_retention table
// (Fase 5). Tenant B must never see tenant A's retention policy under RLS.
func TestTenantRetentionIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Ret A', 'ret-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Ret B', 'ret-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO tenant_retention (tenant_id, alerts_retention_days) VALUES ($1, 90)`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create retention policy: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM tenant_retention`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d retention policy(ies) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}

// TestAgentIsolation is the mandatory cross-tenant leak test for the agent tables (enrollment tokens
// and agents). Tenant B must never see tenant A's agents or enrollment tokens under RLS.
func TestAgentIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Agt A', 'agt-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Agt B', 'agt-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctxA, `INSERT INTO agent_enrollment_tokens (tenant_id, token_hash, expires_at) VALUES ($1, 'hashA', NOW() + INTERVAL '1 hour')`, tenantA); err != nil {
			return err
		}
		_, err := tx.Exec(ctxA, `INSERT INTO agents (tenant_id, name) VALUES ($1, 'agentA')`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create agent rows: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var tokens, agents int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM agent_enrollment_tokens`).Scan(&tokens); err != nil {
			return err
		}
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM agents`).Scan(&agents); err != nil {
			return err
		}
		if tokens != 0 || agents != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d token(s) and %d agent(s) belonging to tenant A", tokens, agents)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}

// TestAgentSNMPIsolation is the mandatory cross-tenant leak test for agent_snmp_targets (slice 2).
func TestAgentSNMPIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Snmp A', 'snmp-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Snmp B', 'snmp-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO agent_snmp_targets (tenant_id, name, host, community_encrypted, community_nonce, checks) VALUES ($1, 'sw', '10.0.0.1', '\x00', '\x00', '[]')`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create snmp target: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM agent_snmp_targets`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d SNMP target(s) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}

// TestAgentMetricsIsolation is the mandatory cross-tenant leak test for agent_metrics (slice 3).
func TestAgentMetricsIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Met A', 'met-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Met B', 'met-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO agent_metrics (tenant_id, oid, label, value) VALUES ($1, '1.2.3', 'cpu', 42)`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create metric: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM agent_metrics`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d metric(s) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}

func TestDiscoveredDevicesIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Disc A', 'disc-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Disc B', 'disc-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO discovered_devices (tenant_id, ip, sysname, vendor, device_type) VALUES ($1, '10.0.0.1', 'gw', 'MikroTik', 'router')`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create discovered device: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM discovered_devices`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d discovered device(s) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}

func TestAssetsIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Asset A', 'asset-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Asset B', 'asset-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO assets (tenant_id, identifier, name, business_criticality) VALUES ($1, '10.0.0.1', 'gw', 'critical')`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create asset: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM assets`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d asset(s) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}

func TestDiscoveredLinksIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Link A', 'link-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Link B', 'link-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO discovered_links (tenant_id, local_ip, local_port, remote_chassis_id, remote_port_id) VALUES ($1, '10.0.0.2', '1', 'aa:bb', 'Gi0/1')`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create discovered link: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM discovered_links`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d discovered link(s) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}


// TestPlaybookIsolation is the mandatory cross-tenant leak test for the playbook tables (Backlog B7).
// Tenant B must never see tenant A's playbook under RLS.
func TestPlaybookIsolation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Pb A', 'pb-iso-a', 'active')", tenantA); err != nil {
		t.Fatalf("insert tenant A: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantA)
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'Pb B', 'pb-iso-b', 'active')", tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantB)

	ctxA := db.WithTenantID(ctx, tenantA)
	if err := db.ExecuteInTenantTx(ctxA, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctxA, `INSERT INTO playbooks (tenant_id, name, steps) VALUES ($1, 'contain-threat', '[{"type":"comment","text":"x"}]'::jsonb)`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("tenant A failed to create playbook: %v", err)
	}

	ctxB := db.WithTenantID(ctx, tenantB)
	if err := db.ExecuteInTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctxB, `SELECT COUNT(*) FROM playbooks`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("SECURITY BREACH: tenant B saw %d playbook(s) belonging to tenant A", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B transaction failed: %v", err)
	}
}
