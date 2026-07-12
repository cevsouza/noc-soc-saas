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

