package service

import (
	"strings"
	"testing"
)

func TestBuildSMTPMessage_ContainsHeaders(t *testing.T) {
	msg := buildSMTPMessage(
		"noreply@multica.ai",
		"user@example.com",
		"Hello",
		"<p>body</p>",
	)

	checks := []string{
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/html; charset=\"UTF-8\"\r\n",
		"From: noreply@multica.ai\r\n",
		"To: user@example.com\r\n",
		"Subject: Hello\r\n",
		"\r\n",
		"<p>body</p>",
	}
	for _, want := range checks {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q", want)
		}
	}
}

func TestBuildSMTPMessage_HeaderBodySeparator(t *testing.T) {
	msg := buildSMTPMessage("a@b.com", "c@d.com", "Subj", "<b>hi</b>")
	// Headers and body must be separated by a blank line (CRLF CRLF).
	if !strings.Contains(msg, "\r\n\r\n") {
		t.Error("message missing blank-line separator between headers and body")
	}
}

func TestNewSMTPEmailService_Defaults(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_FROM", "")

	svc := NewSMTPEmailService()

	if svc.port != "587" {
		t.Errorf("default port: got %q, want %q", svc.port, "587")
	}
	if svc.fromEmail != "noreply@multica.ai" {
		t.Errorf("default from: got %q, want %q", svc.fromEmail, "noreply@multica.ai")
	}
	if svc.host != "smtp.example.com" {
		t.Errorf("host: got %q, want %q", svc.host, "smtp.example.com")
	}
}

func TestNewSMTPEmailService_CustomValues(t *testing.T) {
	t.Setenv("SMTP_HOST", "mail.corp.com")
	t.Setenv("SMTP_PORT", "465")
	t.Setenv("SMTP_USERNAME", "user")
	t.Setenv("SMTP_PASSWORD", "secret")
	t.Setenv("SMTP_FROM", "no-reply@corp.com")

	svc := NewSMTPEmailService()

	if svc.host != "mail.corp.com" {
		t.Errorf("host: got %q", svc.host)
	}
	if svc.port != "465" {
		t.Errorf("port: got %q", svc.port)
	}
	if svc.username != "user" {
		t.Errorf("username: got %q", svc.username)
	}
	if svc.password != "secret" {
		t.Errorf("password: got %q", svc.password)
	}
	if svc.fromEmail != "no-reply@corp.com" {
		t.Errorf("from: got %q", svc.fromEmail)
	}
}

func TestParseTLSMode(t *testing.T) {
	tests := []struct {
		input string
		want  tlsMode
	}{
		{"", tlsModeSTARTTLS},
		{"starttls", tlsModeSTARTTLS},
		{"STARTTLS", tlsModeSTARTTLS},
		{"tls", tlsModeImplicit},
		{"TLS", tlsModeImplicit},
		{"implicit", tlsModeImplicit},
		{"IMPLICIT", tlsModeImplicit},
		{"smtps", tlsModeImplicit},
		{"SMTPS", tlsModeImplicit},
		{"unknown", tlsModeSTARTTLS},
	}
	for _, tt := range tests {
		got := parseTLSMode(tt.input)
		if got != tt.want {
			t.Errorf("parseTLSMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNewSMTPEmailService_TLSModeImplicit(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_TLS_MODE", "tls")

	svc := NewSMTPEmailService()

	if svc.tlsMode != tlsModeImplicit {
		t.Errorf("expected tlsModeImplicit, got %v", svc.tlsMode)
	}
}

func TestNewSMTPEmailService_TLSModeDefault(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_TLS_MODE", "")

	svc := NewSMTPEmailService()

	if svc.tlsMode != tlsModeSTARTTLS {
		t.Errorf("expected tlsModeSTARTTLS, got %v", svc.tlsMode)
	}
}

// TestSMTPEmailService_DevMode verifies that both methods short-circuit
// and return nil (logging only) when SMTP_HOST is not set.
func TestSMTPEmailService_DevMode(t *testing.T) {
	svc := &SMTPEmailService{} // host is empty string → dev mode

	if err := svc.SendVerificationCode("u@example.com", "123456"); err != nil {
		t.Errorf("SendVerificationCode dev mode: unexpected error: %v", err)
	}
	if err := svc.SendInvitationEmail("u@example.com", "Alice", "Acme", "inv-id"); err != nil {
		t.Errorf("SendInvitationEmail dev mode: unexpected error: %v", err)
	}
}

