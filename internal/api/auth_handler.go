package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"noc-api/internal/middleware"
	"noc-api/internal/model"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token  string         `json:"token"`
	User   UserDTO        `json:"user"`
	Tenant TenantDTO      `json:"tenant"`
}

type UserDTO struct {
	ID    uuid.UUID      `json:"id"`
	Email string         `json:"email"`
	Name  string         `json:"name"`
	Role  model.UserRole `json:"role"`
}

type TenantDTO struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// HandleRegister registers a new user, hashes password, and dispatches verification email.
func HandleRegister(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: Invalid JSON payload", http.StatusBadRequest)
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		req.Name = strings.TrimSpace(req.Name)

		if req.Email == "" || req.Password == "" || req.Name == "" {
			http.Error(w, "Bad Request: email, password, and name are required fields", http.StatusBadRequest)
			return
		}

		if len(req.Password) < 6 {
			http.Error(w, "Bad Request: password must be at least 6 characters long", http.StatusBadRequest)
			return
		}

		// Hash the password using bcrypt
		pwdHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to process password", http.StatusInternalServerError)
			return
		}

		verificationToken := uuid.New().String()

		smtpHost := os.Getenv("SMTP_HOST")
		smtpPort := os.Getenv("SMTP_PORT")
		smtpUser := os.Getenv("SMTP_USERNAME")
		smtpPass := os.Getenv("SMTP_PASSWORD")
		smtpSender := os.Getenv("SMTP_SENDER")

		isVerified := false
		if smtpHost == "" || smtpPort == "" || smtpUser == "" || smtpPass == "" || smtpSender == "" {
			isVerified = true
		}

		// Access control: Auto-promote specified initial administrator emails
		role := model.RoleOperator
		adminEmails := []string{
			"cadu.souza@itfacilservicos.com.br",
			"felipe.gomes@itfacilservicos.com.br",
			"cevsouza@hotmail.com",
		}
		for _, adminEmail := range adminEmails {
			if req.Email == adminEmail {
				role = model.RoleAdmin
				break
			}
		}

		ctx := r.Context()

		// Start transactional user creation
		tx, err := pgPool.Begin(ctx)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(ctx)

		// Check if user already exists
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)
		if err != nil {
			http.Error(w, "Internal Server Error: Database lookup failed", http.StatusInternalServerError)
			return
		}
		if exists {
			http.Error(w, "Conflict: An account with this email already exists", http.StatusConflict)
			return
		}

		var userID uuid.UUID
		queryInsertUser := `
			INSERT INTO users (email, name, password_hash, is_verified, verification_token, global_role)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`
		err = tx.QueryRow(ctx, queryInsertUser, req.Email, req.Name, string(pwdHash), isVerified, verificationToken, string(role)).Scan(&userID)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to create user account", http.StatusInternalServerError)
			return
		}

		// No tenant binding during user registration. Users are global.

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "Internal Server Error: Transaction commit failed", http.StatusInternalServerError)
			return
		}

		// Dispatch SMTP Verification Email (Asynchronous so API response remains fast)
		if !isVerified {
			go func() {
				err := security.SendVerificationEmail(req.Email, req.Name, verificationToken)
				if err != nil {
					log.Printf("[SMTP ERROR] Failed to send verification email to %s: %v", req.Email, err)
				}
			}()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if isVerified {
			_, _ = w.Write([]byte(`{"message":"Registro realizado com sucesso. Conta ativada automaticamente (SMTP não configurado).","auto_verified":true}`))
		} else {
			_, _ = w.Write([]byte(`{"message":"Registro realizado com sucesso. Verifique seu e-mail para ativar a conta.","auto_verified":false}`))
		}
	}
}

// HandleVerify handles checking email tokens, verifying the user, and redirecting them to Cockpit.
func HandleVerify(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "Bad Request: Missing token parameter", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		var userID uuid.UUID

		err := pgPool.QueryRow(ctx, "SELECT id FROM users WHERE verification_token = $1", token).Scan(&userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "Unauthorized: Invalid or expired verification token", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Update verification status in a transaction
		tx, err := pgPool.Begin(ctx)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(ctx)

		_, err = tx.Exec(ctx, "UPDATE users SET is_verified = TRUE, verification_token = NULL WHERE id = $1", userID)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to update user status", http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "Internal Server Error: Failed to commit verification status", http.StatusInternalServerError)
			return
		}

		// Resolve cockpit UI redirect target
		cockpitURL := os.Getenv("PUBLIC_COCKPIT_URL")
		if cockpitURL == "" {
			cockpitURL = "http://localhost:3000"
		}

		// Redirect browser to Cockpit with verified success parameter
		http.Redirect(w, r, cockpitURL+"/?verified=true", http.StatusSeeOther)
	}
}

// HandleLogin verifies credentials, asserts email verification, and issues JWT tokens.
func HandleLogin(pgPool *pgxpool.Pool, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: Invalid JSON payload", http.StatusBadRequest)
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))

		if req.Email == "" || req.Password == "" {
			http.Error(w, "Bad Request: Email and password are required fields", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		var userID uuid.UUID
		var name string
		var pwdHash string
		var isVerified bool
		var globalRole string

		queryUser := `
			SELECT id, name, password_hash, is_verified, global_role
			FROM users
			WHERE email = $1
		`
		err := pgPool.QueryRow(ctx, queryUser, req.Email).Scan(&userID, &name, &pwdHash, &isVerified, &globalRole)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "Unauthorized: Credenciais inválidas", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Validate password hash
		err = bcrypt.CompareHashAndPassword([]byte(pwdHash), []byte(req.Password))
		if err != nil {
			http.Error(w, "Unauthorized: Credenciais inválidas", http.StatusUnauthorized)
			return
		}

		// Force verification
		if !isVerified {
			http.Error(w, "Forbidden: Por favor, verifique seu e-mail para ativar a conta antes de fazer login.", http.StatusForbidden)
			return
		}

		// Query user's tenant role mapping
		var tenantID uuid.UUID
		var tenantName string
		var tenantRoleStr string

		queryTenant := `
			SELECT tu.tenant_id, t.name, tu.role
			FROM tenant_users tu
			JOIN tenants t ON t.id = tu.tenant_id
			WHERE tu.user_id = $1
			LIMIT 1
		`
		err = pgPool.QueryRow(ctx, queryTenant, userID).Scan(&tenantID, &tenantName, &tenantRoleStr)
		if err != nil {
			// Find first active tenant in database
			err = pgPool.QueryRow(ctx, "SELECT id, name FROM tenants WHERE status = 'active' LIMIT 1").Scan(&tenantID, &tenantName)
			if err != nil {
				// Bootstrap default tenant
				tenantID = uuid.New()
				tenantName = "ITFácil NOC"
				_, _ = pgPool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, 'itfacil-noc', 'active')", tenantID, tenantName)
			}
			tenantRoleStr = globalRole
		}

		// Issue JWT signed token
		claims := &middleware.JWTClaims{
			UserID:   userID,
			TenantID: tenantID,
			Role:     model.UserRole(tenantRoleStr),
			Email:    req.Email,
			Exp:      time.Now().Add(24 * time.Hour).Unix(), // 24 Hours valid
		}

		token, err := middleware.GenerateJWT(claims, jwtSecret)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to issue token", http.StatusInternalServerError)
			return
		}

		resp := AuthResponse{
			Token: token,
			User: UserDTO{
				ID:    userID,
				Email: req.Email,
				Name:  name,
				Role:  model.UserRole(tenantRoleStr),
			},
			Tenant: TenantDTO{
				ID:   tenantID,
				Name: tenantName,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

type AdminCreateUserRequest struct {
	Email    string         `json:"email"`
	Password string         `json:"password"`
	Name     string         `json:"name"`
	Role     model.UserRole `json:"role"`
	TenantID string         `json:"tenant_id,omitempty"`
}

// HandleAdminCreateUser allows admins to create/register new users (including other admins).
func HandleAdminCreateUser(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminCreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: Invalid JSON payload", http.StatusBadRequest)
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		req.Name = strings.TrimSpace(req.Name)

		if req.Email == "" || req.Password == "" || req.Name == "" || req.Role == "" {
			http.Error(w, "Bad Request: email, password, name, and role are required", http.StatusBadRequest)
			return
		}

		// Validate role
		if req.Role != model.RoleAdmin && req.Role != model.RoleOperator && req.Role != model.RoleViewer {
			http.Error(w, "Bad Request: Invalid role specified", http.StatusBadRequest)
			return
		}

		pwdHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to process password", http.StatusInternalServerError)
			return
		}

		verificationToken := uuid.New().String()
		ctx := r.Context()

		tx, err := pgPool.Begin(ctx)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(ctx)

		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)
		if err != nil {
			http.Error(w, "Internal Server Error: Database lookup failed", http.StatusInternalServerError)
			return
		}
		if exists {
			http.Error(w, "Conflict: User already exists", http.StatusConflict)
			return
		}

		var userID uuid.UUID
		queryInsertUser := `
			INSERT INTO users (email, name, password_hash, is_verified, verification_token, global_role)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`
		err = tx.QueryRow(ctx, queryInsertUser, req.Email, req.Name, string(pwdHash), false, verificationToken, string(req.Role)).Scan(&userID)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to create user", http.StatusInternalServerError)
			return
		}

		// No tenant binding for created user. Users are global.

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "Internal Server Error: Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		// Send verification email
		go func() {
			_ = security.SendVerificationEmail(req.Email, req.Name, verificationToken)
		}()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"message":"Usuário cadastrado com sucesso pelo administrador. E-mail de verificação enviado."}`))
	}
}
