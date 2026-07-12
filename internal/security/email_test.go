package security

import (
	"fmt"
	"net/smtp"
	"os"
	"testing"
)

func setSMTPEnv(t *testing.T) {
	t.Helper()
	vars := map[string]string{
		"SMTP_HOST":     "smtp.example.com",
		"SMTP_PORT":     "587",
		"SMTP_USERNAME": "user@example.com",
		"SMTP_PASSWORD": "secret",
		"SMTP_SENDER":   "noreply@example.com",
	}
	for k, v := range vars {
		old, existed := os.LookupEnv(k)
		os.Setenv(k, v)
		t.Cleanup(func() {
			if existed {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		})
	}
}

func clearSMTPEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_SENDER"} {
		old, existed := os.LookupEnv(k)
		os.Unsetenv(k)
		t.Cleanup(func() {
			if existed {
				os.Setenv(k, old)
			}
		})
	}
}

func TestSendMailSkipsWhenSMTPUnconfigured(t *testing.T) {
	clearSMTPEnv(t)

	called := false
	old := sendMailFunc
	sendMailFunc = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		called = true
		return nil
	}
	defer func() { sendMailFunc = old }()

	if err := SendMail("dest@example.com", "Assunto", "<p>corpo</p>"); err != nil {
		t.Fatalf("expected nil error when SMTP unconfigured, got %v", err)
	}
	if called {
		t.Error("expected sendMailFunc NOT to be called when SMTP is unconfigured")
	}
}

func TestSendMailBuildsExpectedMessage(t *testing.T) {
	setSMTPEnv(t)

	var capturedMsg []byte
	var capturedTo []string
	old := sendMailFunc
	sendMailFunc = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		capturedMsg = msg
		capturedTo = to
		return nil
	}
	defer func() { sendMailFunc = old }()

	if err := SendMail("dest@example.com", "Assunto de Teste", "<p>corpo</p>"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "From: noreply@example.com\r\n" +
		"To: dest@example.com\r\n" +
		"Subject: Assunto de Teste\r\n" +
		"MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\r\n\r\n" +
		"<p>corpo</p>"

	if string(capturedMsg) != expected {
		t.Errorf("message mismatch:\ngot:  %q\nwant: %q", capturedMsg, expected)
	}
	if len(capturedTo) != 1 || capturedTo[0] != "dest@example.com" {
		t.Errorf("expected to=[dest@example.com], got %v", capturedTo)
	}
}

// TestSendVerificationEmailUnchanged confirms the SendVerificationEmail -> SendMail refactor
// produces byte-identical output to the original inline implementation for the same inputs —
// the original built: "From: X\r\nTo: Y\r\n" + "Subject: Verifique seu e-mail - NOC SaaS\r\n" +
// "MIME-version: ...\r\n\r\n" + body. SendMail("Verifique seu e-mail - NOC SaaS", body) must
// produce exactly the same bytes.
func TestSendVerificationEmailUnchanged(t *testing.T) {
	setSMTPEnv(t)
	os.Setenv("PUBLIC_API_URL", "https://api.example.com")
	t.Cleanup(func() { os.Unsetenv("PUBLIC_API_URL") })

	var capturedMsg []byte
	old := sendMailFunc
	sendMailFunc = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		capturedMsg = msg
		return nil
	}
	defer func() { sendMailFunc = old }()

	if err := SendVerificationEmail("dest@example.com", "Fulano", "tok123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	verificationLink := "https://api.example.com/api/v1/auth/verify?token=tok123"
	expectedBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: sans-serif; background-color: #0b0f19; color: #f3f4f6; padding: 20px; }
        .card { background-color: #111827; border: 1px solid #1f2937; border-radius: 12px; padding: 30px; max-width: 500px; margin: 0 auto; box-shadow: 0 4px 15px rgba(0,0,0,0.5); }
        h2 { color: #3b82f6; margin-top: 0; }
        p { color: #9ca3af; line-height: 1.5; }
        .btn { display: inline-block; background: linear-gradient(135deg, #3b82f6 0%%, #1d4ed8 100%%); color: #ffffff !important; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: bold; margin: 20px 0; text-align: center; }
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
</html>`, "Fulano", verificationLink)

	expected := "From: noreply@example.com\r\n" +
		"To: dest@example.com\r\n" +
		"Subject: Verifique seu e-mail - NOC SaaS\r\n" +
		"MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\r\n\r\n" +
		expectedBody

	if string(capturedMsg) != expected {
		t.Errorf("SendVerificationEmail message drifted from the pre-refactor byte layout:\ngot:  %q\nwant: %q", capturedMsg, expected)
	}
}
