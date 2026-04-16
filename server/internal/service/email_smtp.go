package service

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"strings"
)

type smtpSender struct {
	host      string
	port      string
	username  string
	password  string
	fromEmail string
}

func newSMTPSender() *smtpSender {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	username := os.Getenv("SMTP_USERNAME")
	password := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM_EMAIL")
	if from == "" {
		from = os.Getenv("RESEND_FROM_EMAIL")
	}
	if from == "" {
		from = "noreply@multica.ai"
	}

	if host == "" {
		slog.Warn("smtp: SMTP_HOST not set, falling back to dev mode")
	}

	return &smtpSender{
		host:      host,
		port:      port,
		username:  username,
		password:  password,
		fromEmail: from,
	}
}

func (s *smtpSender) SendVerificationCode(to, code string) error {
	if s.host == "" {
		fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
		return nil
	}

	subject := "Your Multica verification code"
	return s.send(to, subject, verificationHTML(code))
}

func (s *smtpSender) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	link := inviteURL(invitationID)

	if s.host == "" {
		fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, link)
		return nil
	}

	subject := invitationSubject(inviterName, workspaceName)
	return s.send(to, subject, invitationHTML(inviterName, workspaceName, link))
}

func (s *smtpSender) send(to, subject, htmlBody string) error {
	addr := net.JoinHostPort(s.host, s.port)

	// Build the RFC 2822 message with priority headers and HTML content type.
	var msg strings.Builder
	fmt.Fprintf(&msg, "From: %s\r\n", s.fromEmail)
	fmt.Fprintf(&msg, "To: %s\r\n", to)
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("X-Priority: 1\r\n")
	msg.WriteString("Importance: high\r\n")
	msg.WriteString("X-Mailer: Multica\r\n")
	msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer client.Close()

	// Upgrade to TLS (STARTTLS) if the server supports it.
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}

	// Authenticate if credentials are provided.
	if s.username != "" && s.password != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(s.fromEmail); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := fmt.Fprint(w, msg.String()); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close body: %w", err)
	}

	return client.Quit()
}
