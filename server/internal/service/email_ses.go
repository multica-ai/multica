package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

type sesSender struct {
	client    *sesv2.Client
	fromEmail string
}

func newSESSender() *sesSender {
	from := fromEmailWithFallback("SES_FROM_EMAIL")

	region := strings.TrimSpace(os.Getenv("SES_REGION"))
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

	slog.Info("EmailService: SES", "region", region, "from", from)
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

	subject := "Your Multica verification code"
	_, err := s.client.SendEmail(context.Background(), &sesv2.SendEmailInput{
		FromEmailAddress: &s.fromEmail,
		Destination:      &types.Destination{ToAddresses: []string{to}},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{Data: &subject},
				Body: &types.Body{
					Html: &types.Content{Data: ptr(verificationHTML(code))},
					Text: &types.Content{Data: ptr(verificationText(code))},
				},
				Headers: sesTransactionalHeaders(),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("ses send verification code to %s: %w", to, err)
	}
	return nil
}

func (s *sesSender) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	link := buildInviteURL(invitationID)

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
					Html: &types.Content{Data: ptr(invitationHTML(inviterName, workspaceName, link))},
					Text: &types.Content{Data: ptr(invitationText(inviterName, workspaceName, link))},
				},
				Headers: sesTransactionalHeaders(),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("ses send invitation to %s: %w", to, err)
	}
	return nil
}

func sesTransactionalHeaders() []types.MessageHeader {
	return []types.MessageHeader{
		{Name: ptr("X-Priority"), Value: ptr("1")},
		{Name: ptr("Importance"), Value: ptr("high")},
		{Name: ptr("X-Mailer"), Value: ptr("Multica")},
	}
}

func ptr(s string) *string { return &s }
