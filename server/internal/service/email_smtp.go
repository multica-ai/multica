package service

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"os"
	"strings"
)

// tlsMode controls how the SMTP connection is secured.
type tlsMode int

const (
	// tlsModeSTARTTLS upgrades a plain TCP connection to TLS via the STARTTLS
	// command. Standard for port 587 (submission).
	tlsModeSTARTTLS tlsMode = iota
	// tlsModeImplicit opens a TLS connection from the start (Implicit TLS /
	// SMTPS). Required for port 465.
	tlsModeImplicit
)

// parseTLSMode converts the SMTP_TLS_MODE environment variable value to a
// tlsMode constant. Accepts "tls" / "implicit" for Implicit TLS; everything
// else (including empty) defaults to STARTTLS.
func parseTLSMode(s string) tlsMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tls", "implicit", "smtps":
		return tlsModeImplicit
	default:
		return tlsModeSTARTTLS
	}
}

// SMTPEmailService sends emails via an SMTP relay.
//
// Required environment variables:
//
//	SMTP_HOST        — SMTP server hostname (e.g. smtp.example.com)
//	SMTP_PORT        — SMTP server port (default: 587)
//	SMTP_USERNAME    — SMTP auth username
//	SMTP_PASSWORD    — SMTP auth password
//	SMTP_FROM        — envelope / From header address (default: noreply@multica.ai)
//	SMTP_TLS_MODE    — TLS mode: "" / "starttls" (default) or "tls" / "implicit" / "smtps"
//	                   Use "tls" for port 465 (Implicit TLS / SMTPS).
type SMTPEmailService struct {
	host      string
	port      string
	username  string
	password  string
	fromEmail string
	tlsMode   tlsMode
}

// NewSMTPEmailService reads SMTP configuration from environment variables and
// returns a ready-to-use SMTPEmailService.
func NewSMTPEmailService() *SMTPEmailService {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "noreply@multica.ai"
	}
	return &SMTPEmailService{
		host:      host,
		port:      port,
		username:  os.Getenv("SMTP_USERNAME"),
		password:  os.Getenv("SMTP_PASSWORD"),
		fromEmail: from,
		tlsMode:   parseTLSMode(os.Getenv("SMTP_TLS_MODE")),
	}
}

// SendVerificationCode sends a one-time login code via SMTP.
func (s *SMTPEmailService) SendVerificationCode(to, code string) error {
	if s.host == "" {
		fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
		return nil
	}

	subject := "Your Multica verification code"
	body := fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
			<h2>Your verification code</h2>
			<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
			<p>This code expires in 10 minutes.</p>
			<p style="color: #666; font-size: 14px;">If you didn't request this code, you can safely ignore this email.</p>
		</div>`, code)

	return callWithTimeout(func() error { return s.send(to, subject, body) })
}

// SendInvitationEmail sends a workspace invitation email via SMTP.
func (s *SMTPEmailService) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://app.multica.ai"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)

	if s.host == "" {
		fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s \u2014 %s\n", to, inviterName, workspaceName, inviteURL)
		return nil
	}

	subject, htmlBody := invitationBody(inviterName, workspaceName, inviteURL)
	return callWithTimeout(func() error { return s.send(to, subject, htmlBody) })
}

// send constructs an RFC 2822 message and delivers it using the configured TLS
// mode: STARTTLS (default, port 587) or Implicit TLS (SMTPS, port 465).
func (s *SMTPEmailService) send(to, subject, htmlBody string) error {
	msg := []byte(buildSMTPMessage(s.fromEmail, to, subject, htmlBody))
	if s.tlsMode == tlsModeImplicit {
		return s.sendImplicitTLS(to, msg)
	}
	return s.sendSTARTTLS(to, msg)
}

// sendSTARTTLS delivers via plain TCP upgraded to TLS with the STARTTLS
// command. Standard for port 587.
func (s *SMTPEmailService) sendSTARTTLS(to string, msg []byte) error {
	addr := s.host + ":" + s.port
	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	return smtp.SendMail(addr, auth, s.fromEmail, []string{to}, msg)
}

// sendImplicitTLS delivers via a TLS connection opened from the start (SMTPS).
// Required for servers that expect Implicit TLS on port 465.
func (s *SMTPEmailService) sendImplicitTLS(to string, msg []byte) error {
	addr := s.host + ":" + s.port
	tlsCfg := &tls.Config{ServerName: s.host}

	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("smtp implicit tls dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer client.Close()

	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(s.fromEmail); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		w.Close()
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return client.Quit()
}

// buildSMTPMessage assembles a minimal RFC 2822 / MIME email with an HTML body.
// Separated so the output is unit-testable without an SMTP server.
func buildSMTPMessage(from, to, subject, htmlBody string) string {
	var b strings.Builder
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	return b.String()
}
