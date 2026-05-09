package service

import (
	"testing"
)

// TestResendEmailService_DevMode verifies that both methods short-circuit and
// return nil (logging only) when no Resend API key is configured.
func TestResendEmailService_DevMode(t *testing.T) {
	svc := &ResendEmailService{} // client is nil → dev mode

	if err := svc.SendVerificationCode("u@example.com", "123456"); err != nil {
		t.Errorf("SendVerificationCode dev mode: unexpected error: %v", err)
	}
	if err := svc.SendInvitationEmail("u@example.com", "Alice", "Acme", "inv-id"); err != nil {
		t.Errorf("SendInvitationEmail dev mode: unexpected error: %v", err)
	}
}

func TestNewResendEmailService_DefaultFrom(t *testing.T) {
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("RESEND_FROM_EMAIL", "")

	svc := NewResendEmailService()

	if svc.fromEmail != "noreply@multica.ai" {
		t.Errorf("default from: got %q, want %q", svc.fromEmail, "noreply@multica.ai")
	}
	if svc.client != nil {
		t.Error("client should be nil when RESEND_API_KEY is unset")
	}
}

func TestNewResendEmailService_CustomFrom(t *testing.T) {
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("RESEND_FROM_EMAIL", "no-reply@corp.com")

	svc := NewResendEmailService()

	if svc.fromEmail != "no-reply@corp.com" {
		t.Errorf("from: got %q, want %q", svc.fromEmail, "no-reply@corp.com")
	}
}

// TestNewEmailService_* tests verify that NewEmailService routes to the correct
// provider based on the EMAIL_PROVIDER environment variable.

func TestNewEmailService_DefaultIsResend(t *testing.T) {
	t.Setenv("EMAIL_PROVIDER", "")
	svc := NewEmailService()
	if _, ok := svc.(*ResendEmailService); !ok {
		t.Errorf("expected *ResendEmailService, got %T", svc)
	}
}

func TestNewEmailService_ResendExplicit(t *testing.T) {
	t.Setenv("EMAIL_PROVIDER", "resend")
	svc := NewEmailService()
	if _, ok := svc.(*ResendEmailService); !ok {
		t.Errorf("expected *ResendEmailService, got %T", svc)
	}
}

func TestNewEmailService_SMTPProvider(t *testing.T) {
	t.Setenv("EMAIL_PROVIDER", "smtp")
	svc := NewEmailService()
	if _, ok := svc.(*SMTPEmailService); !ok {
		t.Errorf("expected *SMTPEmailService, got %T", svc)
	}
}
