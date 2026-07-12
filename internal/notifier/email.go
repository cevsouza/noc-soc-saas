package notifier

import (
	"context"
	"fmt"

	"noc-api/internal/model"
	"noc-api/internal/security"
)

// EmailNotifier escalates critical/fatal alerts by email via the platform's shared SMTP
// credentials (internal/security.SendMail). Unlike the other notifiers, the "secret" passed
// into Notify isn't really confidential — it's the tenant's configured recipient address(es) —
// but it's stored via the same vault mechanism as everything else for UI consistency (see
// escalationVaultKeys in internal/worker/worker.go).
type EmailNotifier struct{}

func NewEmailNotifier() *EmailNotifier {
	return &EmailNotifier{}
}

func (n *EmailNotifier) IntegrationType() string { return "email" }

func (n *EmailNotifier) Notify(ctx context.Context, recipientAddr string, alert *model.Alert) error {
	subject := fmt.Sprintf("[NOC SaaS] Alerta %s: %s", alert.Severity, alert.Summary)
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: sans-serif; background-color: #0b0f19; color: #f3f4f6; padding: 20px; }
        .card { background-color: #111827; border: 1px solid #1f2937; border-radius: 12px; padding: 30px; max-width: 500px; margin: 0 auto; box-shadow: 0 4px 15px rgba(0,0,0,0.5); }
        h2 { color: #ef4444; margin-top: 0; }
        p { color: #9ca3af; line-height: 1.5; }
        .fact { margin: 4px 0; }
        .fact b { color: #e5e7eb; }
        .footer { margin-top: 20px; font-size: 11px; color: #4b5563; border-top: 1px solid #1f2937; padding-top: 15px; }
    </style>
</head>
<body>
    <div class="card">
        <h2>Alerta %s no NOC SaaS</h2>
        <p>%s</p>
        <div class="fact"><b>Tenant:</b> %s</div>
        <div class="fact"><b>Tipo de Evento:</b> %s</div>
        <div class="fact"><b>Horário:</b> %s</div>
        <div class="footer">
            NOC SaaS Inc. - Monitoramento Inteligente &amp; Auto-Cura de Redes
        </div>
    </div>
</body>
</html>`, alert.Severity, alert.Summary, alert.TenantID.String(), alert.EventType, alert.CreatedAt.Format("2006-01-02 15:04:05"))

	return security.SendMail(recipientAddr, subject, body)
}
