package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mailgun/mailgun-go/v5"
)

// maxSubjectFieldRunes bounds how much user-controlled text (workspace name,
// inviter name) can land in an email Subject. Prevents attackers from stuffing
// a full phishing pitch into a workspace name that gets sent from our domain.
const maxSubjectFieldRunes = 60

// sendTimeout caps how long a single Mailgun send may take. Without this, the
// default HTTP client has no timeout — upstream slowness would pile up
// goroutines (invitation emails are fire-and-forget) and block the
// verification-code path.
const sendTimeout = 15 * time.Second

// EmailPayload is the SDK-agnostic shape the builder produces. Keeping this
// separate from *mailgun.Message means unit tests can assert on body/subject/
// recipients without depending on the SDK's internal builder state, and the
// email provider can be swapped without rewriting tests.
type EmailPayload struct {
	From    string
	To      []string
	Subject string
	Html    string
	Text    string
}

type EmailService struct {
	mg        *mailgun.Client
	domain    string
	fromEmail string
}

// NewEmailService wires up the Mailgun client from environment config. When
// MAILGUN_API_KEY or MAILGUN_DOMAIN are unset, the service runs in a
// stdout-print dev mode so local development works without a Mailgun account
// (matches the 888888 master-code flow gated by APP_ENV != "production").
func NewEmailService() *EmailService {
	apiKey := strings.TrimSpace(os.Getenv("MAILGUN_API_KEY"))
	domain := strings.TrimSpace(os.Getenv("MAILGUN_DOMAIN"))
	from := strings.TrimSpace(os.Getenv("MAILGUN_FROM_EMAIL"))
	if from == "" {
		from = "noreply@agentfarm.g2.com"
	}

	// Both key AND domain are required for live sending. Missing either →
	// dev mode. This is deliberately stricter than "api key only" because
	// Mailgun's send API needs the domain per call; a half-configured setup
	// would fail at send time with a confusing 404.
	var mg *mailgun.Client
	if apiKey != "" && domain != "" {
		mg = mailgun.NewMailgun(apiKey)
		mg.SetHTTPClient(&http.Client{Timeout: sendTimeout})
	}

	return &EmailService{
		mg:        mg,
		domain:    domain,
		fromEmail: from,
	}
}

// SendVerificationCode sends a one-time login code. The code is server-generated
// (6-digit numeric) so no user-controlled text reaches the email body here.
// If that ever changes, escape the user-controlled fields the same way
// SendInvitationEmail does.
func (s *EmailService) SendVerificationCode(to, code string) error {
	payload := buildVerificationParams(s.fromEmail, to, code)

	if s.mg == nil {
		fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
		return nil
	}

	return s.send(payload)
}

// SendInvitationEmail notifies the invitee that they have been invited to a workspace.
// invitationID is included in the URL so the email deep-links to /invite/{id}.
func (s *EmailService) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://app.multica.ai"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)

	payload := buildInvitationParams(s.fromEmail, to, inviterName, workspaceName, inviteURL)

	if s.mg == nil {
		fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, inviteURL)
		return nil
	}

	return s.send(payload)
}

// send converts an EmailPayload into a Mailgun message and dispatches it.
// Mailgun's IsValid() check requires a non-empty text body even when HTML is
// set, so every EmailPayload must carry both parts. Message IDs are logged
// for correlation with Mailgun's dashboard / webhook events.
func (s *EmailService) send(p EmailPayload) error {
	if len(p.To) == 0 {
		return fmt.Errorf("email: no recipients")
	}
	msg := mailgun.NewMessage(s.domain, p.From, p.Subject, p.Text, p.To...)
	if p.Html != "" {
		msg.SetHTML(p.Html)
	}

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	resp, err := s.mg.Send(ctx, msg)
	if err != nil {
		return fmt.Errorf("mailgun send: %w", err)
	}
	slog.Info("email sent", "to", p.To[0], "subject", p.Subject, "mailgun_id", resp.ID)
	return nil
}

// buildVerificationParams assembles the payload for a one-time login code.
// The code is server-generated numeric, so no escaping is needed in the body.
// Separated from SendVerificationCode so the template is unit-testable without
// a live Mailgun account.
func buildVerificationParams(from, to, code string) EmailPayload {
	return EmailPayload{
		From:    from,
		To:      []string{to},
		Subject: "Your Multica verification code",
		Html: fmt.Sprintf(
			`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
				<h2>Your verification code</h2>
				<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
				<p>This code expires in 10 minutes.</p>
				<p style="color: #666; font-size: 14px;">If you didn't request this code, you can safely ignore this email.</p>
			</div>`, code),
		Text: fmt.Sprintf(
			"Your Multica verification code: %s\n\nThis code expires in 10 minutes.\n\nIf you didn't request this code, you can safely ignore this email.\n",
			code),
	}
}

// buildInvitationParams assembles the payload for an invitation email.
// Separated from SendInvitationEmail so the sanitization behavior is
// unit-testable without needing to mock the Mailgun SDK.
func buildInvitationParams(from, to, inviterName, workspaceName, inviteURL string) EmailPayload {
	safeWorkspace := html.EscapeString(workspaceName)
	safeInviter := html.EscapeString(inviterName)
	subjectInviter := sanitizeSubjectField(inviterName)
	subjectWorkspace := sanitizeSubjectField(workspaceName)

	return EmailPayload{
		From:    from,
		To:      []string{to},
		Subject: fmt.Sprintf("%s invited you to %s on Multica", subjectInviter, subjectWorkspace),
		Html: fmt.Sprintf(
			`<div style="font-family: sans-serif; max-width: 480px; margin: 0 auto;">
				<h2>You're invited to join %s</h2>
				<p><strong>%s</strong> invited you to collaborate in the <strong>%s</strong> workspace on Multica.</p>
				<p style="margin: 24px 0;">
					<a href="%s" style="display: inline-block; padding: 12px 24px; background: #000; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500;">Accept invitation</a>
				</p>
				<p style="color: #666; font-size: 14px;">You'll need to log in to accept or decline the invitation.</p>
			</div>`, safeWorkspace, safeInviter, safeWorkspace, inviteURL),
		// Plain-text alternative uses the unsanitized names — subject-line
		// sanitization is specifically for header-injection defense, and the
		// text body (rendered in plain-text mail clients) is safe to show the
		// raw characters the user chose for their name / workspace.
		Text: fmt.Sprintf(
			"%s invited you to collaborate in the %s workspace on Multica.\n\nAccept the invitation: %s\n\nYou'll need to log in to accept or decline the invitation.\n",
			inviterName, workspaceName, inviteURL),
	}
}

// sanitizeSubjectField prepares user-controlled text for the email Subject line.
// Subject is not HTML-rendered, so HTML-escaping would leak literal entities
// (e.g. &lt;script&gt;) into the recipient's inbox. Instead strip control
// characters (defense in depth against header-injection-adjacent abuse even
// though Mailgun also filters CR/LF) and cap length so attackers can't stuff
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
