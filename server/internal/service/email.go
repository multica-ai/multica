package service

import (
	"crypto/tls"
	"fmt"
	"html"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/resend/resend-go/v2"
)

// maxSubjectFieldRunes bounds how much user-controlled text (workspace name,
// inviter name) can land in an email Subject. Prevents attackers from stuffing
// a full phishing pitch into a workspace name that gets sent from our domain.
const maxSubjectFieldRunes = 60

// emailSender abstracts the actual send mechanism (Resend API or SMTP).
type emailSender interface {
	Send(from string, to []string, subject, htmlBody string) error
}

type EmailService struct {
	sender    emailSender
	fromEmail string
	fromName  string
}

// resendSender wraps the Resend SDK client.
type resendSender struct {
	client *resend.Client
}

func (s *resendSender) Send(from string, to []string, subject, htmlBody string) error {
	_, err := s.client.Emails.Send(&resend.SendEmailRequest{
		From:    from,
		To:      to,
		Subject: subject,
		Html:    htmlBody,
	})
	return err
}

// smtpSender sends mail via SMTP (supports SSL/TLS on port 465 and STARTTLS).
type smtpSender struct {
	host     string
	port     int
	username string
	password string
	useSSL   bool
}

func (s *smtpSender) Send(from string, to []string, subject, htmlBody string) error {
	// Build RFC 2822 message
	var msg strings.Builder
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	// Extract the bare email from a "Name <email>" formatted from address.
	senderAddr := from
	if idx := strings.Index(from, "<"); idx >= 0 {
		senderAddr = strings.Trim(from[idx:], "<> ")
	}

	if s.useSSL {
		return s.sendSSL(addr, senderAddr, to, msg.String())
	}
	return s.sendStartTLS(addr, senderAddr, to, msg.String())
}

// sendSSL connects via TLS first (implicit TLS, typically port 465).
func (s *smtpSender) sendSSL(addr, from string, to []string, msg string) error {
	tlsCfg := &tls.Config{ServerName: s.host}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("smtp ssl dial: %w", err)
	}

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp ssl new client: %w", err)
	}
	defer client.Close()

	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp ssl auth: %w", err)
		}
	}

	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp ssl MAIL: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp ssl RCPT %s: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp ssl DATA: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp ssl write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp ssl close data: %w", err)
	}
	return client.Quit()
}

// sendStartTLS connects in plain text and upgrades via STARTTLS (typically port 587).
func (s *smtpSender) sendStartTLS(addr, from string, to []string, msg string) error {
	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	return smtp.SendMail(addr, auth, from, to, []byte(msg))
}

func NewEmailService() *EmailService {
	fromName := os.Getenv("SMTP_FROM_NAME")

	var sender emailSender
	var from string

	// Priority: SMTP > Resend API > dev console (nil sender)
	smtpHost := os.Getenv("SMTP_HOST")
	smtpUsername := os.Getenv("SMTP_USERNAME")
	if smtpHost != "" {
		port := 465
		if p := os.Getenv("SMTP_PORT"); p != "" {
			if parsed, err := strconv.Atoi(p); err == nil {
				port = parsed
			}
		}
		useSSL := true
		if v := os.Getenv("SMTP_SSL"); v == "false" {
			useSSL = false
		}
		sender = &smtpSender{
			host:     smtpHost,
			port:     port,
			username: smtpUsername,
			password: os.Getenv("SMTP_PASSWORD"),
			useSSL:   useSSL,
		}
		// When SMTP is used, the envelope sender MUST match the auth user
		// for most providers (QQ Exmail, Office365, etc.).
		from = smtpUsername
	} else if apiKey := os.Getenv("RESEND_API_KEY"); apiKey != "" {
		sender = &resendSender{client: resend.NewClient(apiKey)}
	}

	// Allow explicit override via RESEND_FROM_EMAIL (works for both backends).
	if override := os.Getenv("RESEND_FROM_EMAIL"); override != "" {
		from = override
	}
	if from == "" {
		from = "noreply@multica.ai"
	}

	return &EmailService{
		sender:    sender,
		fromEmail: from,
		fromName:  fromName,
	}
}

// formatFrom returns "Name <email>" if fromName is set, otherwise just the email.
func (s *EmailService) formatFrom() string {
	if s.fromName != "" {
		return fmt.Sprintf("%s <%s>", s.fromName, s.fromEmail)
	}
	return s.fromEmail
}

// SendVerificationCode sends a one-time login code. The code is server-generated
// (6-digit numeric) so no user-controlled text reaches the email body here.
// If that ever changes, escape the user-controlled fields the same way
// SendInvitationEmail does.
func (s *EmailService) SendVerificationCode(to, code string) error {
	subject := "Your Multica verification code"
	htmlBody := fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
				<h2>Your verification code</h2>
				<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
				<p>This code expires in 10 minutes.</p>
				<p style="color: #666; font-size: 14px;">If you didn't request this code, you can safely ignore this email.</p>
			</div>`, code)

	if s.sender == nil {
		fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
		return nil
	}

	return s.sender.Send(s.formatFrom(), []string{to}, subject, htmlBody)
}

// SendInvitationEmail notifies the invitee that they have been invited to a workspace.
// invitationID is included in the URL so the email deep-links to /invite/{id}.
func (s *EmailService) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://app.multica.ai"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)

	if s.sender == nil {
		fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, inviteURL)
		return nil
	}

	subject, htmlBody := buildInvitationEmail(inviterName, workspaceName, inviteURL)
	return s.sender.Send(s.formatFrom(), []string{to}, subject, htmlBody)
}

// SendNotificationEmail sends a notification email (e.g. when a user is @mentioned).
func (s *EmailService) SendNotificationEmail(to, title, body, link, senderName string) error {
	safeTitle := html.EscapeString(title)
	safeBody := html.EscapeString(body)
	safeSenderName := html.EscapeString(strings.TrimSpace(senderName))

	subject := buildNotificationEmailSubject(title, senderName)

	linkHTML := ""
	if link != "" {
		linkHTML = fmt.Sprintf(
			`<p style="margin: 24px 0;">
				<a href="%s" style="display: inline-block; padding: 12px 24px; background: #000; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500;">View in Multica</a>
			</p>`, html.EscapeString(link))
	}

	senderHTML := ""
	if safeSenderName != "" {
		senderHTML = fmt.Sprintf(
			`<p style="margin: 0 0 16px 0; color: #333;"><strong>From:</strong> %s</p>`,
			safeSenderName,
		)
	}

	htmlBody := fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 480px; margin: 0 auto;">
			<h2>%s</h2>
			%s
			<p style="white-space: pre-wrap;">%s</p>
			%s
			<p style="color: #666; font-size: 14px;">You received this email because notifications are enabled in your Multica settings.</p>
		</div>`, safeTitle, senderHTML, safeBody, linkHTML)

	if s.sender == nil {
		if strings.TrimSpace(senderName) != "" {
			fmt.Printf("[DEV] Notification email to %s from %s: %s — %s\n", to, strings.TrimSpace(senderName), title, link)
		} else {
			fmt.Printf("[DEV] Notification email to %s: %s — %s\n", to, title, link)
		}
		return nil
	}

	return s.sender.Send(s.formatFrom(), []string{to}, subject, htmlBody)
}

func buildNotificationEmailSubject(title, senderName string) string {
	subjectTitle := sanitizeSubjectField(title)
	subjectSender := sanitizeSubjectField(senderName)

	if subjectSender != "" && subjectTitle != "" {
		return fmt.Sprintf("%s mentioned you in %s", subjectSender, subjectTitle)
	}
	if subjectSender != "" {
		return fmt.Sprintf("%s mentioned you on Multica", subjectSender)
	}
	if subjectTitle != "" {
		return subjectTitle
	}
	return "Multica Notification"
}

// buildInvitationEmail assembles subject and HTML body for an invitation email.
// Separated so the sanitisation behavior is unit-testable without needing to
// mock any sending backend.
func buildInvitationEmail(inviterName, workspaceName, inviteURL string) (subject, htmlBody string) {
	safeWorkspace := html.EscapeString(workspaceName)
	safeInviter := html.EscapeString(inviterName)
	subjectInviter := sanitizeSubjectField(inviterName)
	subjectWorkspace := sanitizeSubjectField(workspaceName)

	subject = fmt.Sprintf("%s invited you to %s on Multica", subjectInviter, subjectWorkspace)
	htmlBody = fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 480px; margin: 0 auto;">
				<h2>You're invited to join %s</h2>
				<p><strong>%s</strong> invited you to collaborate in the <strong>%s</strong> workspace on Multica.</p>
				<p style="margin: 24px 0;">
					<a href="%s" style="display: inline-block; padding: 12px 24px; background: #000; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500;">Accept invitation</a>
				</p>
				<p style="color: #666; font-size: 14px;">You'll need to log in to accept or decline the invitation.</p>
			</div>`, safeWorkspace, safeInviter, safeWorkspace, inviteURL)
	return
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
