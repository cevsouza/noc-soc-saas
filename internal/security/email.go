package security

import (
	"fmt"
	"net/smtp"
	"os"
)

// SendVerificationEmail sends a clean HTML verification email to the user.
func SendVerificationEmail(toEmail, name, token string) error {
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_USERNAME")
	smtpPass := os.Getenv("SMTP_PASSWORD")
	smtpSender := os.Getenv("SMTP_SENDER")

	// If SMTP parameters are missing, log a warning and skip (essential for local dev/testing without SMTP credentials)
	if smtpHost == "" || smtpPort == "" || smtpUser == "" || smtpPass == "" || smtpSender == "" {
		fmt.Printf("[SMTP WARNING] SMTP configuration missing. Verification token for %s is: %s\n", toEmail, token)
		return nil
	}

	publicAPIURL := os.Getenv("PUBLIC_API_URL")
	if publicAPIURL == "" {
		publicAPIURL = "http://localhost:8080"
	}

	verificationLink := fmt.Sprintf("%s/api/v1/auth/verify?token=%s", publicAPIURL, token)

	subject := "Subject: Verifique seu e-mail - NOC SaaS\r\n"
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\r\n\r\n"
	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: sans-serif; background-color: #0b0f19; color: #f3f4f6; padding: 20px; }
        .card { background-color: #111827; border: 1px solid #1f2937; border-radius: 12px; padding: 30px; max-width: 500px; margin: 0 auto; box-shadow: 0 4px 15px rgba(0,0,0,0.5); }
        h2 { color: #3b82f6; margin-top: 0; }
        p { color: #9ca3af; line-height: 1.5; }
        .btn { display: inline-block; background: linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%); color: #ffffff !important; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: bold; margin: 20px 0; text-align: center; }
        .footer { margin-top: 20px; font-size: 11px; color: #4b5563; border-top: 1px solid #1f2937; padding-top: 15px; }
    </style>
</head>
<body>
    <div class="card">
        <h2>Verifique sua conta no NOC SaaS</h2>
        <p>Olá %s,</p>
        <p>Obrigado por se registrar no NOC SaaS. Por favor, clique no botão abaixo para verificar o seu endereço de e-mail e ativar a sua conta:</p>
        <a href="%s" style="color: #ffffff;" class="btn">Verificar E-mail</a>
        <p>Se você não realizou esse cadastro, ignore este e-mail.</p>
        <div class="footer">
            NOC SaaS Inc. - Monitoramento Inteligente & Auto-Cura de Redes
        </div>
    </div>
</body>
</html>`, name, verificationLink)

	msg := []byte("From: " + smtpSender + "\r\n" +
		"To: " + toEmail + "\r\n" +
		subject +
		mime +
		body)

	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)
	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)

	err := smtp.SendMail(addr, auth, smtpSender, []string{toEmail}, msg)
	if err != nil {
		return fmt.Errorf("failed to send verification email: %w", err)
	}

	return nil
}
