package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"noc-api/internal/middleware"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SetConsolePreferenceRequest is the body of PUT /api/v1/users/me/console.
type SetConsolePreferenceRequest struct {
	Console string `json:"console"`
}

// validConsole reports whether c is an allowed landing-console preference (B9). Pure and unit-tested.
func validConsole(c string) bool {
	switch strings.TrimSpace(c) {
	case "all", "noc", "soc":
		return true
	default:
		return false
	}
}

// HandleSetConsolePreference lets any authenticated user set their own landing-console preference
// (all / noc / soc). Convenience only — it changes where the user lands after login, not what they
// can access. Updates users.default_console for the caller (claims.UserID); no tenant scoping needed
// since the users table is global.
func HandleSetConsolePreference(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var req SetConsolePreferenceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		console := strings.TrimSpace(req.Console)
		if !validConsole(console) {
			http.Error(w, "console must be one of: all, noc, soc", http.StatusBadRequest)
			return
		}
		if _, err := pgPool.Exec(r.Context(), `UPDATE users SET default_console = $1 WHERE id = $2`, console, claims.UserID); err != nil {
			http.Error(w, "failed to save preference", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"default_console": console})
	}
}
