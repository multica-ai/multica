package service

import (
	"fmt"
	"html"
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

type EmailService struct {
	client    *resend.Client
	fromEmail string
}

func NewEmailService() *EmailService {
	apiKey := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("RESEND_FROM_EMAIL")
	if from == "" {
		from = "forge@asymbl.app"
	}

	var client *resend.Client
	if apiKey != "" {
		client = resend.NewClient(apiKey)
	}

	return &EmailService{
		client:    client,
		fromEmail: from,
	}
}

// SendVerificationCode sends a one-time login code. The code is server-generated
// (6-digit numeric) so no user-controlled text reaches the email body here.
// If that ever changes, escape the user-controlled fields the same way
// SendInvitationEmail does.
func (s *EmailService) SendVerificationCode(to, code string) error {
	if s.client == nil {
		fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
		return nil
	}

	params := &resend.SendEmailRequest{
		From:    s.fromEmail,
		To:      []string{to},
		Subject: "Your Forge verification code",
		Html: fmt.Sprintf(
			`<div style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Arial, sans-serif; max-width: 480px; margin: 0 auto; background: #FFFFFF; padding: 32px 24px;">
				<div style="text-align: center; margin-bottom: 24px;">
					<img src="https://forge.asymbl.app/brand/asymbl-logo-color.png" alt="Asymbl" height="36" style="height: 36px; width: auto;" />
				</div>
				<h2 style="color: #032D60; font-size: 22px; margin: 0 0 8px;">Your verification code</h2>
				<p style="color: #595959; font-size: 14px; margin: 0 0 24px;">Enter this code to sign in to Forge.</p>
				<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0; color: #032D60;">%s</p>
				<p style="color: #595959; font-size: 14px;">This code expires in 10 minutes.</p>
				<p style="color: #888; font-size: 12px; margin-top: 32px; padding-top: 16px; border-top: 1px solid #E8F4FC;">If you didn't request this code, you can safely ignore this email.</p>
			</div>`, code),
	}

	_, err := s.client.Emails.Send(params)
	return err
}

// SendInvitationEmail notifies the invitee that they have been invited to a workspace.
// invitationID is included in the URL so the email deep-links to /invite/{id}.
func (s *EmailService) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://forge.asymbl.app"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)

	if s.client == nil {
		fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, inviteURL)
		return nil
	}

	params := buildInvitationParams(s.fromEmail, to, inviterName, workspaceName, inviteURL)
	_, err := s.client.Emails.Send(params)
	return err
}

// SendInvitationEmail body uses Forge branding (Asymbl light-mode palette).

// buildInvitationParams assembles the Resend request for an invitation email.
// Separated from SendInvitationEmail so the sanitization behavior is unit-testable
// without needing to mock the Resend SDK.
func buildInvitationParams(from, to, inviterName, workspaceName, inviteURL string) *resend.SendEmailRequest {
	safeWorkspace := html.EscapeString(workspaceName)
	safeInviter := html.EscapeString(inviterName)
	subjectInviter := sanitizeSubjectField(inviterName)
	subjectWorkspace := sanitizeSubjectField(workspaceName)

	return &resend.SendEmailRequest{
		From:    from,
		To:      []string{to},
		Subject: fmt.Sprintf("%s invited you to %s on Forge", subjectInviter, subjectWorkspace),
		Html: fmt.Sprintf(
			`<div style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Arial, sans-serif; max-width: 520px; margin: 0 auto; background: #FFFFFF; padding: 32px 24px;">
				<div style="text-align: center; margin-bottom: 24px;">
					<img src="https://forge.asymbl.app/brand/asymbl-logo-color.png" alt="Asymbl" height="36" style="height: 36px; width: auto;" />
				</div>
				<h2 style="color: #032D60; font-size: 22px; margin: 0 0 12px;">You're invited to join %s</h2>
				<p style="color: #595959; font-size: 15px; line-height: 1.5;"><strong style="color: #032D60;">%s</strong> invited you to collaborate in the <strong style="color: #032D60;">%s</strong> workspace on Forge.</p>
				<p style="margin: 32px 0;">
					<a href="%s" style="display: inline-block; padding: 12px 28px; background: #DD7001; color: #FFFFFF; text-decoration: none; border-radius: 8px; font-weight: 600; font-size: 15px;">Accept invitation</a>
				</p>
				<p style="color: #595959; font-size: 14px;">You'll need to sign in to accept or decline the invitation.</p>
				<p style="color: #888; font-size: 12px; margin-top: 32px; padding-top: 16px; border-top: 1px solid #E8F4FC;">Forge is Asymbl's internal workspace for AI-augmented work. <a href="https://forge.asymbl.app" style="color: #385CAE;">forge.asymbl.app</a></p>
			</div>`, safeWorkspace, safeInviter, safeWorkspace, inviteURL),
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
