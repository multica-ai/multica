package service

import (
	"fmt"
	"html"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// maxSubjectFieldRunes bounds how much user-controlled text (workspace name,
// inviter name) can land in an email Subject. Prevents attackers from stuffing
// a full phishing pitch into a workspace name that gets sent from our domain.
const maxSubjectFieldRunes = 60

// emailSendTimeout is the maximum time allowed for a single email send attempt.
var emailSendTimeout = 30 * time.Second

// EmailService is the interface that email provider implementations must satisfy.
type EmailService interface {
	SendVerificationCode(to, code string) error
	SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error
}

// NewEmailService returns an EmailService selected by the EMAIL_PROVIDER
// environment variable. Supported values: "resend" (default), "smtp".
func NewEmailService() EmailService {
	switch strings.ToLower(os.Getenv("EMAIL_PROVIDER")) {
	case "smtp":
		return NewSMTPEmailService()
	default: // resend or unset
		return NewResendEmailService()
	}
}

// callWithTimeout runs fn in a goroutine and returns its error. If fn does not
// finish within emailSendTimeout an error is returned; the goroutine continues
// in the background so the underlying connection is not left dangling.
func callWithTimeout(fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()

	select {
	case err := <-done:
		return err
	case <-time.After(emailSendTimeout):
		return fmt.Errorf("email send timed out after %s", emailSendTimeout)
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
	return string(runes[:maxSubjectFieldRunes-1]) + "\u2026"
}

// buildInvitationHTML renders the HTML body for a workspace invitation email.
// safeWorkspace and safeInviter must already be HTML-escaped by the caller.
func buildInvitationHTML(safeInviter, safeWorkspace, inviteURL string) string {
	return strings.Join([]string{
		`<div style="font-family: sans-serif; max-width: 480px; margin: 0 auto;">`,
		`<h2>You're invited to join `, safeWorkspace, `</h2>`,
		`<p><strong>`, safeInviter, `</strong> invited you to collaborate in the <strong>`, safeWorkspace, `</strong> workspace on Multica.</p>`,
		`<p style="margin: 24px 0;">`,
		`<a href="`, inviteURL, `" style="display: inline-block; padding: 12px 24px; background: #000; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500;">Accept invitation</a>`,
		`</p>`,
		`<p style="color: #666; font-size: 14px;">You'll need to log in to accept or decline the invitation.</p>`,
		`</div>`,
	}, "")
}

// invitationSubject returns the email subject for a workspace invitation.
// inviterName and workspaceName are the raw (unescaped) values.
func invitationSubject(inviterName, workspaceName string) string {
	return sanitizeSubjectField(inviterName) + " invited you to " + sanitizeSubjectField(workspaceName) + " on Multica"
}

// invitationBody returns subject and HTML body for a workspace invitation,
// applying all required escaping. Used by provider implementations.
func invitationBody(inviterName, workspaceName, inviteURL string) (subject, htmlBody string) {
	subject = invitationSubject(inviterName, workspaceName)
	htmlBody = buildInvitationHTML(
		html.EscapeString(inviterName),
		html.EscapeString(workspaceName),
		inviteURL,
	)
	return
}
