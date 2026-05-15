package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
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

// emailSender is the internal interface for email delivery.
// resendSender, postmarkSender, and logSender each implement it.
type emailSender interface {
	sendVerificationCode(to, code string) error
	sendInvitationEmail(to, inviterName, workspaceName, invitationID string) error
}

// EmailService is the public-facing email service used by handlers.
// Provider selection is encapsulated here; no handler references a concrete provider.
type EmailService struct {
	sender emailSender
}

func NewEmailService() *EmailService {
	postmarkToken := os.Getenv("POSTMARK_SERVER_TOKEN")
	resendKey := os.Getenv("RESEND_API_KEY")

	// POSTMARK_FROM_EMAIL takes precedence; RESEND_FROM_EMAIL is the fallback.
	fromEmail := os.Getenv("POSTMARK_FROM_EMAIL")
	if fromEmail == "" {
		fromEmail = os.Getenv("RESEND_FROM_EMAIL")
	}
	if fromEmail == "" {
		fromEmail = "noreply@multica.ai"
	}

	var sender emailSender
	switch {
	case postmarkToken != "":
		if resendKey != "" {
			log.Println("[email] both POSTMARK_SERVER_TOKEN and RESEND_API_KEY are set; using Postmark")
		}
		sender = &postmarkSender{token: postmarkToken, from: fromEmail}
	case resendKey != "":
		sender = &resendSender{client: resend.NewClient(resendKey), from: fromEmail}
	default:
		sender = &logSender{}
	}

	return &EmailService{sender: sender}
}

// SendVerificationCode sends a one-time login code. The code is server-generated
// (6-digit numeric) so no user-controlled text reaches the email body here.
func (s *EmailService) SendVerificationCode(to, code string) error {
	return s.sender.sendVerificationCode(to, code)
}

// SendInvitationEmail notifies the invitee that they have been invited to a workspace.
func (s *EmailService) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	return s.sender.sendInvitationEmail(to, inviterName, workspaceName, invitationID)
}

// --- logSender (fallback when no provider is configured) ---

type logSender struct{}

func (l *logSender) sendVerificationCode(to, code string) error {
	fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
	return nil
}

func (l *logSender) sendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://app.multica.ai"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)
	fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, inviteURL)
	return nil
}

// --- resendSender ---

type resendSender struct {
	client *resend.Client
	from   string
}

func (r *resendSender) sendVerificationCode(to, code string) error {
	params := &resend.SendEmailRequest{
		From:    r.from,
		To:      []string{to},
		Subject: "Your Multica verification code",
		Html:    verificationCodeHTML(code),
	}
	_, err := r.client.Emails.Send(params)
	return err
}

func (r *resendSender) sendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://app.multica.ai"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)
	params := buildInvitationParams(r.from, to, inviterName, workspaceName, inviteURL)
	_, err := r.client.Emails.Send(params)
	return err
}

// --- postmarkSender (plain HTTP — no extra dep) ---

type postmarkSender struct {
	token string
	from  string
}

type postmarkEmailRequest struct {
	From     string `json:"From"`
	To       string `json:"To"`
	Subject  string `json:"Subject"`
	HtmlBody string `json:"HtmlBody"`
}

func (p *postmarkSender) post(email postmarkEmailRequest) error {
	body, err := json.Marshal(email)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.postmarkapp.com/email", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Postmark-Server-Token", p.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("postmark: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (p *postmarkSender) sendVerificationCode(to, code string) error {
	return p.post(postmarkEmailRequest{
		From:     p.from,
		To:       to,
		Subject:  "Your Multica verification code",
		HtmlBody: verificationCodeHTML(code),
	})
}

func (p *postmarkSender) sendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://app.multica.ai"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)
	safeWorkspace := html.EscapeString(workspaceName)
	safeInviter := html.EscapeString(inviterName)
	subjectInviter := sanitizeSubjectField(inviterName)
	subjectWorkspace := sanitizeSubjectField(workspaceName)
	return p.post(postmarkEmailRequest{
		From:     p.from,
		To:       to,
		Subject:  fmt.Sprintf("%s invited you to %s on Multica", subjectInviter, subjectWorkspace),
		HtmlBody: invitationHTML(safeInviter, safeWorkspace, inviteURL),
	})
}

// --- shared HTML builders ---

func verificationCodeHTML(code string) string {
	return fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
			<h2>Your verification code</h2>
			<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
			<p>This code expires in 10 minutes.</p>
			<p style="color: #666; font-size: 14px;">If you didn't request this code, you can safely ignore this email.</p>
		</div>`, code)
}

func invitationHTML(safeInviter, safeWorkspace, inviteURL string) string {
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

// buildInvitationParams assembles the Resend request for an invitation email.
// Separated from resendSender so the sanitization behaviour is unit-testable
// without needing to mock the Resend SDK.
func buildInvitationParams(from, to, inviterName, workspaceName, inviteURL string) *resend.SendEmailRequest {
	safeWorkspace := html.EscapeString(workspaceName)
	safeInviter := html.EscapeString(inviterName)
	subjectInviter := sanitizeSubjectField(inviterName)
	subjectWorkspace := sanitizeSubjectField(workspaceName)

	return &resend.SendEmailRequest{
		From:    from,
		To:      []string{to},
		Subject: fmt.Sprintf("%s invited you to %s on Multica", subjectInviter, subjectWorkspace),
		Html:    invitationHTML(safeInviter, safeWorkspace, inviteURL),
	}
}

// sanitizeSubjectField prepares user-controlled text for the email Subject line.
// Subject is not HTML-rendered, so HTML-escaping would leak literal entities
// (e.g. &lt;script&gt;) into the recipient's inbox. Instead strip control
// characters (defense in depth against header-injection-adjacent abuse even
// though Resend/Postmark also filter CR/LF) and cap length so attackers can't
// stuff a full phishing subject into a workspace name.
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
