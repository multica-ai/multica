package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

type sesSender struct {
	client    *sesv2.Client
	fromEmail string
}

func newSESSender() *sesSender {
	from := os.Getenv("SES_FROM_EMAIL")
	if from == "" {
		from = os.Getenv("RESEND_FROM_EMAIL")
	}
	if from == "" {
		from = "noreply@multica.ai"
	}

	region := os.Getenv("SES_REGION")
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		slog.Warn("ses: failed to load AWS config, falling back to dev mode", "error", err)
		return &sesSender{fromEmail: from}
	}

	return &sesSender{
		client:    sesv2.NewFromConfig(cfg),
		fromEmail: from,
	}
}

func (s *sesSender) SendVerificationCode(to, code string) error {
	if s.client == nil {
		fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
		return nil
	}

	_, err := s.client.SendEmail(context.Background(), &sesv2.SendEmailInput{
		FromEmailAddress: &s.fromEmail,
		Destination:      &types.Destination{ToAddresses: []string{to}},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{Data: strPtr("Your Multica verification code")},
				Body: &types.Body{
					Html: &types.Content{Data: strPtr(verificationHTML(code))},
					Text: &types.Content{Data: strPtr(fmt.Sprintf("Your Multica verification code is: %s\n\nThis code expires in 10 minutes.\nIf you didn't request this code, you can safely ignore this email.", code))},
				},
				Headers: transactionalHeaders(),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("ses send verification code: %w", err)
	}
	return nil
}

func (s *sesSender) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	link := inviteURL(invitationID)

	if s.client == nil {
		fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, link)
		return nil
	}

	subject := invitationSubject(inviterName, workspaceName)
	_, err := s.client.SendEmail(context.Background(), &sesv2.SendEmailInput{
		FromEmailAddress: &s.fromEmail,
		Destination:      &types.Destination{ToAddresses: []string{to}},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{Data: &subject},
				Body: &types.Body{
					Html: &types.Content{Data: strPtr(invitationHTML(inviterName, workspaceName, link))},
					Text: &types.Content{Data: strPtr(fmt.Sprintf("%s invited you to collaborate in the %s workspace on Multica.\n\nAccept the invitation: %s", inviterName, workspaceName, link))},
				},
				Headers: transactionalHeaders(),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("ses send invitation email: %w", err)
	}
	return nil
}

// transactionalHeaders marks emails as high-priority transactional messages.
// This helps email clients surface them prominently and reduces false-positive
// spam classification (especially Microsoft 365 Defender).
func transactionalHeaders() []types.MessageHeader {
	return []types.MessageHeader{
		{Name: strPtr("X-Priority"), Value: strPtr("1")},
		{Name: strPtr("X-Mailer"), Value: strPtr("Multica")},
		{Name: strPtr("Importance"), Value: strPtr("high")},
	}
}

func strPtr(s string) *string { return &s }
