package api

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
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

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

func validateEmail(email string) error {
	email = strings.TrimSpace(email)
	if !emailRegex.MatchString(email) {
		return errors.New("formato de e-mail inválido")
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return errors.New("formato de e-mail inválido")
	}
	domain := parts[1]

	// DNS MX Record lookup
	mx, err := net.LookupMX(domain)
	if err == nil && len(mx) > 0 {
		return nil
	}

	// Fallback DNS A/AAAA lookup
	ips, err := net.LookupIP(domain)
	if err == nil && len(ips) > 0 {
		return nil
	}

	return errors.New("o domínio do e-mail não é válido ou não possui registros DNS ativos")
}

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

		if err := validateEmail(req.Email); err != nil {
			http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
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

		// Access control: auto-promote and auto-verify configured initial administrator emails
		// (INITIAL_ADMIN_EMAILS env var) to bootstrap the platform without lockouts.
		role := model.RoleOperator
		if security.IsInitialAdminEmail(req.Email) {
			role = model.RoleAdmin
			isVerified = true
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
			ORDER BY t.name
			LIMIT 1
		`
		err = pgPool.QueryRow(ctx, queryTenant, userID).Scan(&tenantID, &tenantName, &tenantRoleStr)
		if err != nil {
			// No tenant membership. A platform admin (GlobalRole==admin) legitimately sees every
			// tenant, so we fall back to the first active tenant as their "home". A non-admin with
			// zero grants, however, has no authorized tenant at all — rather than silently dropping
			// them into an arbitrary tenant they aren't a member of (the old behavior), we refuse
			// the login so an admin must explicitly grant access via /api/v1/admin/access first.
			// (Fase 5 fatia 1 decision.)
			if globalRole != string(model.RoleAdmin) {
				http.Error(w, "Forbidden: nenhum tenant autorizado para esta conta. Contate um administrador para liberar seu acesso.", http.StatusForbidden)
				return
			}
			err = pgPool.QueryRow(ctx, "SELECT id, name FROM tenants WHERE status = 'active' LIMIT 1").Scan(&tenantID, &tenantName)
			if err != nil {
				// Bootstrap default tenant
				tenantID = uuid.New()
				tenantName = "ITFácil NOC"
				_, _ = pgPool.Exec(ctx, "INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, 'itfacil-noc', 'active')", tenantID, tenantName)
			}
			tenantRoleStr = globalRole
		}

		if globalRole == string(model.RoleAdmin) {
			tenantRoleStr = string(model.RoleAdmin)
		}

		// Issue JWT signed token.
		// Role is scoped to this tenant; GlobalRole is platform-wide (MSP-level) and is the
		// only claim that may authorize cross-tenant actions (e.g. tenant_id=all).
		claims := &middleware.JWTClaims{
			UserID:     userID,
			TenantID:   tenantID,
			Role:       model.UserRole(tenantRoleStr),
			GlobalRole: model.UserRole(globalRole),
			Email:      req.Email,
			Exp:        time.Now().Add(24 * time.Hour).Unix(), // 24 Hours valid
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
	// TenantIDs are the tenants the new user is granted access to, created as tenant_users rows
	// in the same transaction as the user. For an operator/viewer this is what makes the account
	// usable (a non-admin with zero tenant grants is blocked at login — see HandleLogin). Empty
	// is allowed (e.g. when creating another platform admin, who sees all tenants regardless).
	TenantIDs []string `json:"tenant_ids,omitempty"`
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

		if err := validateEmail(req.Email); err != nil {
			http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Validate role — accepts the granular tenant roles (read_only, analyst_l1/l2/l3,
		// tenant_admin) as well as the legacy admin/operator/viewer. Platform roles
		// (platform_admin, mssp_analyst) are not assignable here: this field sets the tenant-scoped
		// role, and only the legacy "admin" additionally implies platform admin for backward
		// compatibility (see model.IsPlatformAdmin).
		if !model.IsValidTenantRole(req.Role) {
			http.Error(w, "Bad Request: Invalid role specified", http.StatusBadRequest)
			return
		}

		pwdHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to process password", http.StatusInternalServerError)
			return
		}

		verificationToken := uuid.New().String()

		// Mirror HandleRegister: if SMTP isn't configured, no verification email can ever arrive,
		// so auto-verify the account instead of leaving it permanently unable to log in. When SMTP
		// IS configured, keep requiring email verification.
		smtpConfigured := os.Getenv("SMTP_HOST") != "" && os.Getenv("SMTP_PORT") != "" &&
			os.Getenv("SMTP_USERNAME") != "" && os.Getenv("SMTP_PASSWORD") != "" && os.Getenv("SMTP_SENDER") != ""
		isVerified := !smtpConfigured

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

		// Parse and de-duplicate the requested tenant IDs up front so an invalid one fails the
		// request before we create the user.
		tenantIDs, perr := parseTenantIDList(req.TenantIDs)
		if perr != nil {
			http.Error(w, "Bad Request: "+perr.Error(), http.StatusBadRequest)
			return
		}

		var userID uuid.UUID
		queryInsertUser := `
			INSERT INTO users (email, name, password_hash, is_verified, verification_token, global_role)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`
		err = tx.QueryRow(ctx, queryInsertUser, req.Email, req.Name, string(pwdHash), isVerified, verificationToken, string(req.Role)).Scan(&userID)
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to create user", http.StatusInternalServerError)
			return
		}

		// Bind the new user to each requested tenant in the same transaction, with the tenant-scoped
		// role matching the user's role. Any invalid/nonexistent tenant rolls back the whole
		// creation (foreign key on tenant_users.tenant_id), so we never leave a half-created user.
		for _, tid := range tenantIDs {
			var tenantExists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1)", tid).Scan(&tenantExists); err != nil {
				http.Error(w, "Internal Server Error: Failed to verify tenant", http.StatusInternalServerError)
				return
			}
			if !tenantExists {
				http.Error(w, "Bad Request: one of the provided tenant_ids does not exist", http.StatusBadRequest)
				return
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO tenant_users (tenant_id, user_id, role, created_at)
				VALUES ($1, $2, $3, NOW())
				ON CONFLICT (tenant_id, user_id) DO NOTHING
			`, tid, userID, string(req.Role)); err != nil {
				http.Error(w, "Internal Server Error: Failed to bind user to tenant", http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "Internal Server Error: Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		// Only send a verification email when the account actually needs verifying (SMTP configured).
		if !isVerified {
			go func() {
				_ = security.SendVerificationEmail(req.Email, req.Name, verificationToken)
			}()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if isVerified {
			_, _ = w.Write([]byte(`{"message":"Usuário cadastrado com sucesso e ativado automaticamente (SMTP não configurado)."}`))
		} else {
			_, _ = w.Write([]byte(`{"message":"Usuário cadastrado com sucesso pelo administrador. E-mail de verificação enviado."}`))
		}
	}
}

type UserListResponse struct {
	ID         uuid.UUID      `json:"id"`
	Email      string         `json:"email"`
	Name       string         `json:"name"`
	GlobalRole model.UserRole `json:"global_role"`
	IsVerified bool           `json:"is_verified"`
	CreatedAt  time.Time      `json:"created_at"`
}

// HandleGetUsers returns all registered users in the platform (Admin only)
func HandleGetUsers(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rows, err := pgPool.Query(ctx, "SELECT id, email, name, global_role, is_verified, created_at FROM users ORDER BY email")
		if err != nil {
			http.Error(w, "Failed to query users", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		list := make([]UserListResponse, 0)
		for rows.Next() {
			var u UserListResponse
			if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.GlobalRole, &u.IsVerified, &u.CreatedAt); err != nil {
				http.Error(w, "Failed to scan user details", http.StatusInternalServerError)
				return
			}
			list = append(list, u)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleDeleteUser deletes a user from the platform (Admin only)
func HandleDeleteUser(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := r.URL.Query().Get("id")
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			http.Error(w, "Invalid user ID format", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		claims, ok := middleware.ClaimsFromContext(ctx)
		if ok && claims.UserID == userID {
			http.Error(w, "Conflict: Cannot delete your own active session user", http.StatusConflict)
			return
		}

		tx, err := pgPool.Begin(ctx)
		if err != nil {
			http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(ctx)

		// Delete from tenant_users mapping first
		_, err = tx.Exec(ctx, "DELETE FROM tenant_users WHERE user_id = $1", userID)
		if err != nil {
			http.Error(w, "Failed to delete user associations: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Delete from main users table
		_, err = tx.Exec(ctx, "DELETE FROM users WHERE id = $1", userID)
		if err != nil {
			http.Error(w, "Failed to delete user: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","message":"Usuário excluído com sucesso"}`))
	}
}
