package service

import (
	"fmt"
	"os"
	"strings"

	"github.com/resend/resend-go/v2"
)

// ResendEmailService sends emails via the Resend API.
//
// Required environment variables:
//
//	RESEND_API_KEY    — Resend API key; when unset the service runs in dev mode
//	                    and prints codes/invites to stdout instead of sending.
//	RESEND_FROM_EMAIL — envelope / From header address (default: noreply@multica.ai)
type ResendEmailService struct {
	client    *resend.Client
	fromEmail string
}

// NewResendEmailService reads Resend configuration from environment variables
// and returns a ready-to-use ResendEmailService.
func NewResendEmailService() *ResendEmailService {
	apiKey := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("RESEND_FROM_EMAIL")
	if from == "" {
		from = "noreply@multica.ai"
	}

	var client *resend.Client
	if apiKey != "" {
		client = resend.NewClient(apiKey)
	}

	return &ResendEmailService{
		client:    client,
		fromEmail: from,
	}
}

// SendVerificationCode sends a one-time login code via the Resend API. The code
// is server-generated (6-digit numeric) so no user-controlled text reaches the
// email body here. If that ever changes, escape the user-controlled fields the
// same way SendInvitationEmail does.
func (s *ResendEmailService) SendVerificationCode(to, code string) error {
	if s.client == nil {
		fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
		return nil
	}

	params := &resend.SendEmailRequest{
		From:    s.fromEmail,
		To:      []string{to},
		Subject: "Your Multica verification code",
		Html: fmt.Sprintf(
			`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
				<h2>Your verification code</h2>
				<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
				<p>This code expires in 10 minutes.</p>
				<p style="color: #666; font-size: 14px;">If you didn't request this code, you can safely ignore this email.</p>
			</div>`, code),
	}

	return callWithTimeout(func() error {
		_, err := s.client.Emails.Send(params)
		return err
	})
}

// SendInvitationEmail notifies the invitee that they have been invited to a
// workspace. invitationID is included in the URL so the email deep-links to
// /invite/{id}.
func (s *ResendEmailService) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://app.multica.ai"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)

	if s.client == nil {
		fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s \u2014 %s\n", to, inviterName, workspaceName, inviteURL)
		return nil
	}

	subject, htmlBody := invitationBody(inviterName, workspaceName, inviteURL)
	params := &resend.SendEmailRequest{
		From:    s.fromEmail,
		To:      []string{to},
		Subject: subject,
		Html:    htmlBody,
	}
	return callWithTimeout(func() error {
		_, err := s.client.Emails.Send(params)
		return err
	})
}
