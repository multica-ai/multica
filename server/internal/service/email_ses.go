package service

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// sesSender delivers via AWS SES using ambient IAM credentials (instance/task
// role or the default credential chain) — no static access key is configured
// or stored by this code.
type sesSender struct {
	client    *sesv2.Client
	fromEmail string
}

func newSESSender() *sesSender {
	from := strings.TrimSpace(os.Getenv("SES_FROM_EMAIL"))
	if from == "" {
		from = strings.TrimSpace(os.Getenv("RESEND_FROM_EMAIL"))
	}
	if from == "" {
		from = "noreply@multica.ai"
	}

	region := strings.TrimSpace(os.Getenv("SES_REGION"))
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		fmt.Printf("EmailService: SES config error (%v) — falling back to DEV mode\n", err)
		return &sesSender{fromEmail: from}
	}

	fmt.Printf("EmailService: SES region=%s from=%s\n", cfg.Region, from)
	return &sesSender{
		client:    sesv2.NewFromConfig(cfg),
		fromEmail: from,
	}
}

func (s *sesSender) send(to, subject, htmlBody string) error {
	if s.client == nil {
		fmt.Printf("[DEV] Email to %s: %s\n", to, subject)
		return nil
	}
	_, err := s.client.SendEmail(context.Background(), &sesv2.SendEmailInput{
		FromEmailAddress: &s.fromEmail,
		Destination:      &types.Destination{ToAddresses: []string{to}},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{Data: &subject},
				Body:    &types.Body{Html: &types.Content{Data: &htmlBody}},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("ses send to %s: %w", to, err)
	}
	return nil
}

func (s *sesSender) SendVerificationCode(to, code string) error {
	return s.send(to, "Your Multica verification code", verificationHTML(code))
}

func (s *sesSender) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://multica.ai"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)
	return s.send(to, invitationSubject(inviterName, workspaceName), invitationHTML(inviterName, workspaceName, inviteURL))
}
