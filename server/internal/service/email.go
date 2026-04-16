package service

import (
	"fmt"
	"html"
	"log/slog"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/resend/resend-go/v2"
)

// maxSubjectFieldRunes bounds how much user-controlled text (workspace name,
// inviter name) can land in an email Subject. Prevents attackers from stuffing
// a full phishing pitch into a workspace name that gets sent from our domain.
const maxSubjectFieldRunes = 60

// EmailSender is the interface that all email providers implement.
type EmailSender interface {
	SendVerificationCode(to, code string) error
	SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error
}

// NewEmailSender creates an EmailSender based on the EMAIL_PROVIDER env var.
// Supported values: "" or "resend" (default), "ses", "smtp".
// When no credentials are configured the returned sender prints to stdout (dev mode).
func NewEmailSender() EmailSender {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("EMAIL_PROVIDER")))

	switch provider {
	case "ses":
		slog.Info("email provider configured", "provider", "ses")
		return newSESSender()
	case "smtp":
		slog.Info("email provider configured", "provider", "smtp")
		return newSMTPSender()
	default:
		slog.Info("email provider configured", "provider", "resend")
		return newResendSender()
	}
}

// ---------- shared HTML templates ----------

func verificationHTML(code string) string {
	return fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
			<h2>Your verification code</h2>
			<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
			<p>This code expires in 10 minutes.</p>
			<p style="color: #666; font-size: 14px;">If you didn't request this code, you can safely ignore this email.</p>
		</div>`, code)
}

func invitationHTML(inviterName, workspaceName, inviteURL string) string {
	safeWorkspace := html.EscapeString(workspaceName)
	safeInviter := html.EscapeString(inviterName)

	return fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 480px; margin: 0 auto;">
			<h2>You're invited to join %s</h2>
			<p><strong>%s</strong> invited you to collaborate in the <strong>%s</strong> workspace on Multica.</p>
			<p style="margin: 24px 0;">
				<a href="%s" style="display: inline-block; padding: 12px 24px; background: #000; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500;">Accept invitation</a>
			</p>
			<p style="color: #666; font-size: 14px;">You'll need to log in to accept or decline the invitation.</p>
		</div>`, safeWorkspace, safeInviter, safeWorkspace, inviteURL)
}

func invitationSubject(inviterName, workspaceName string) string {
	return fmt.Sprintf("%s invited you to %s on Multica",
		sanitizeSubjectField(inviterName),
		sanitizeSubjectField(workspaceName))
}

func inviteURL(invitationID string) string {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://app.multica.ai"
	}
	return fmt.Sprintf("%s/invite/%s", appURL, invitationID)
}

// ---------- Resend provider ----------

type resendSender struct {
	client    *resend.Client
	fromEmail string
}

func newResendSender() *resendSender {
	apiKey := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("RESEND_FROM_EMAIL")
	if from == "" {
		from = "noreply@multica.ai"
	}

	var client *resend.Client
	if apiKey != "" {
		client = resend.NewClient(apiKey)
	}

	return &resendSender{
		client:    client,
		fromEmail: from,
	}
}

func (s *resendSender) SendVerificationCode(to, code string) error {
	if s.client == nil {
		fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
		return nil
	}

	params := &resend.SendEmailRequest{
		From:    s.fromEmail,
		To:      []string{to},
		Subject: "Your Multica verification code",
		Html:    verificationHTML(code),
		Text:    fmt.Sprintf("Your Multica verification code is: %s\n\nThis code expires in 10 minutes.\nIf you didn't request this code, you can safely ignore this email.", code),
		Headers: map[string]string{"X-Priority": "1", "Importance": "high", "X-Mailer": "Multica"},
	}

	_, err := s.client.Emails.Send(params)
	return err
}

func (s *resendSender) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	link := inviteURL(invitationID)

	if s.client == nil {
		fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, link)
		return nil
	}

	params := buildInvitationParams(s.fromEmail, to, inviterName, workspaceName, link)
	_, err := s.client.Emails.Send(params)
	return err
}

// buildInvitationParams assembles the Resend request for an invitation email.
// Separated from SendInvitationEmail so the sanitization behavior is unit-testable
// without needing to mock the Resend SDK.
func buildInvitationParams(from, to, inviterName, workspaceName, inviteLink string) *resend.SendEmailRequest {
	return &resend.SendEmailRequest{
		From:    from,
		To:      []string{to},
		Subject: invitationSubject(inviterName, workspaceName),
		Html:    invitationHTML(inviterName, workspaceName, inviteLink),
		Text:    fmt.Sprintf("%s invited you to collaborate in the %s workspace on Multica.\n\nAccept the invitation: %s", inviterName, workspaceName, inviteLink),
		Headers: map[string]string{"X-Priority": "1", "Importance": "high", "X-Mailer": "Multica"},
	}
}

// sanitizeSubjectField prepares user-controlled text for the email Subject line.
// Subject is not HTML-rendered, so HTML-escaping would leak literal entities
// (e.g. &lt;script&gt;) into the recipient's inbox. Instead strip control
// characters (defense in depth against header-injection-adjacent abuse even
// though Resend also filters CR/LF) and cap length so attackers can't stuff
// a full phishing subject into a workspace name.
func sanitizeSubjectField(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	cleaned := b.String()
	if utf8.RuneCountInString(cleaned) <= maxSubjectFieldRunes {
		return cleaned
	}
	runes := []rune(cleaned)
	return string(runes[:maxSubjectFieldRunes-1]) + "…"
}
