package security

import (
	"os"
	"strings"
)

// InitialAdminEmails returns the set of e-mails that should be auto-promoted to platform
// admin and auto-verified on registration, read from the INITIAL_ADMIN_EMAILS environment
// variable (comma-separated). This bootstraps the very first administrator account(s)
// without hard-coding personal e-mails in source control. If the variable is unset or empty,
// no e-mail is auto-promoted — the safe default.
func InitialAdminEmails() []string {
	raw := os.Getenv("INITIAL_ADMIN_EMAILS")
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	emails := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			emails = append(emails, p)
		}
	}
	return emails
}

// IsInitialAdminEmail reports whether the given e-mail (already trimmed/lowercased by the
// caller) is present in INITIAL_ADMIN_EMAILS.
func IsInitialAdminEmail(email string) bool {
	for _, e := range InitialAdminEmails() {
		if e == email {
			return true
		}
	}
	return false
}
