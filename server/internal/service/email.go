package service

import (
	"crypto/tls"
	"fmt"
	"html"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/resend/resend-go/v2"
)

// maxSubjectFieldRunes bounds how much user-controlled text (workspace name,
// inviter name) can land in an email Subject. Prevents attackers from stuffing
// a full phishing pitch into a workspace name that gets sent from our domain.
const maxSubjectFieldRunes = 60

// emailSender abstracts the actual send mechanism. Tests inject a mock sender
// through this interface so the HTML/subject generation remains unit-testable.
type emailSender interface {
	Send(from string, to []string, subject, htmlBody string) error
}

type EmailService struct {
	sender          emailSender
	client          *resend.Client
	fromEmail       string
	fromName        string
	smtpHost        string
	smtpPort        string
	smtpUsername    string
	smtpPassword    string
	smtpTLSInsecure bool
	smtpTLSImplicit bool
}

func NewEmailService() *EmailService {
	apiKey := strings.TrimSpace(os.Getenv("RESEND_API_KEY"))
	fromName := strings.TrimSpace(os.Getenv("SMTP_FROM_NAME"))
	from := strings.TrimSpace(os.Getenv("RESEND_FROM_EMAIL"))
	if from == "" {
		from = "noreply@multica.ai"
	}

	smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	smtpPort := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	smtpImplicitTLS := os.Getenv("SMTP_SSL") == "true"
	if smtpPort == "" {
		if smtpImplicitTLS {
			smtpPort = "465"
		} else {
			smtpPort = "25"
		}
	}
	smtpUsername := strings.TrimSpace(os.Getenv("SMTP_USERNAME"))
	smtpPassword := os.Getenv("SMTP_PASSWORD")
	smtpTLSInsecure := os.Getenv("SMTP_TLS_INSECURE") == "true"

	// SMTP_TLS=implicit forces an immediate TLS handshake on connect (SMTPS).
	// Required by providers like Aliyun enterprise mail that only offer port 465
	// SSL and do not advertise STARTTLS. Default (empty / "starttls") preserves
	// the prior STARTTLS-upgrade behavior.
	smtpTLSMode := strings.ToLower(strings.TrimSpace(os.Getenv("SMTP_TLS")))
	smtpTLSImplicit := smtpTLSMode == "implicit" || smtpTLSMode == "smtps" || smtpTLSMode == "ssl"
	if smtpImplicitTLS {
		smtpTLSImplicit = true
	}
	if smtpTLSMode == "" && smtpPort == "465" {
		smtpTLSImplicit = true
	}
	if smtpTLSMode != "" && !smtpTLSImplicit && smtpTLSMode != "starttls" {
		fmt.Printf("EmailService: SMTP_TLS=%q not recognized, falling back to starttls\n", smtpTLSMode)
	}

	var client *resend.Client
	if apiKey != "" {
		client = resend.NewClient(apiKey)
	}

	switch {
	case smtpHost != "":
		tlsLabel := "starttls"
		if smtpTLSImplicit {
			tlsLabel = "implicit-tls"
		}
		fmt.Printf("EmailService: SMTP relay %s:%s (%s) from=%s\n", smtpHost, smtpPort, tlsLabel, from)
	case client != nil:
		fmt.Printf("EmailService: Resend API from=%s\n", from)
	default:
		fmt.Println("EmailService: DEV mode — codes printed to stdout (set MULTICA_DEV_VERIFICATION_CODE in .env for a fixed local code)")
	}

	return &EmailService{
		client:          client,
		fromEmail:       from,
		fromName:        fromName,
		smtpHost:        smtpHost,
		smtpPort:        smtpPort,
		smtpUsername:    smtpUsername,
		smtpPassword:    smtpPassword,
		smtpTLSInsecure: smtpTLSInsecure,
		smtpTLSImplicit: smtpTLSImplicit,
	}
}

// sendSMTP delivers an HTML email via an SMTP server.
// Supports unauthenticated relay (SMTP_USERNAME empty) and authenticated SMTP.
// Upgrades to STARTTLS when advertised by the server and optionally supports
// implicit TLS when SMTP_SSL=true. Set SMTP_TLS_INSECURE=true for self-signed or
// private CA certificates.
func (s *EmailService) sendSMTP(to []string, subject, htmlBody string) error {
	addr := net.JoinHostPort(s.smtpHost, s.smtpPort)

	tlsCfg := &tls.Config{
		ServerName:         s.smtpHost,
		InsecureSkipVerify: s.smtpTLSInsecure, //nolint:gosec // opt-in via SMTP_TLS_INSECURE=true
	}

	// Bounded dial + whole-session deadline: prevents a blackholed SMTP server
	// from hanging the auth handler (or a background goroutine) indefinitely.
	var conn net.Conn
	var err error
	if s.smtpTLSImplicit {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	if err = conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		conn.Close()
		return fmt.Errorf("smtp set deadline: %w", err)
	}

	c, err := smtp.NewClient(conn, s.smtpHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	// STARTTLS upgrade only makes sense when the underlying connection is still
	// plaintext. Skip when we already dialed with implicit TLS.
	if !s.smtpTLSImplicit {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err = c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}

	if s.smtpUsername != "" {
		auth := smtp.PlainAuth("", s.smtpUsername, s.smtpPassword, s.smtpHost)
		if err = c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	// Probe 8BITMIME after (possible) STARTTLS so the extension list is current.
	// Use quoted-printable for relays that don't advertise 8BITMIME — safer for
	// non-ASCII workspace/inviter names crossing strict or older SMTP hops.
	has8Bit, _ := c.Extension("8BITMIME")
	encodedSubject := mime.QEncoding.Encode("utf-8", subject)
	msgID := fmt.Sprintf("<%d@%s>", time.Now().UnixNano(), s.smtpHost)

	var bodyBytes []byte
	var cte string
	if has8Bit {
		bodyBytes = []byte(htmlBody)
		cte = "8bit"
	} else {
		var buf strings.Builder
		qpw := quotedprintable.NewWriter(&buf)
		_, _ = qpw.Write([]byte(htmlBody))
		_ = qpw.Close()
		bodyBytes = []byte(buf.String())
		cte = "quoted-printable"
	}

	envelopeFrom := s.fromEmail
	if s.smtpUsername != "" {
		envelopeFrom = s.smtpUsername
	}
	if err = c.Mail(envelopeFrom); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	for _, recipient := range to {
		if err = c.Rcpt(recipient); err != nil {
			return fmt.Errorf("smtp RCPT TO <%s>: %w", recipient, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	headers := "From: " + s.formatFrom() + "\r\n" +
		"To: " + strings.Join(to, ", ") + "\r\n" +
		"Subject: " + encodedSubject + "\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: " + msgID + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: " + cte + "\r\n" +
		"\r\n"
	if _, err = fmt.Fprintf(w, "%s%s", headers, bodyBytes); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("smtp end data: %w", err)
	}
	return c.Quit()
}

func (s *EmailService) deliver(to []string, subject, htmlBody string) (bool, error) {
	switch {
	case s.sender != nil:
		return true, s.sender.Send(s.formatFrom(), to, subject, htmlBody)
	case s.smtpHost != "":
		return true, s.sendSMTP(to, subject, htmlBody)
	case s.client != nil:
		_, err := s.client.Emails.Send(&resend.SendEmailRequest{
			From:    s.formatFrom(),
			To:      to,
			Subject: subject,
			Html:    htmlBody,
		})
		return true, err
	default:
		return false, nil
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
// Delivery priority: injected sender → SMTP relay → Resend API → DEV stdout.
func (s *EmailService) SendVerificationCode(to, code string) error {
	subject := "Your Multica verification code"
	htmlBody := fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
			<h2>Your verification code</h2>
			<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
			<p>This code expires in 10 minutes.</p>
			<p style="color: #666; font-size: 14px;">If you didn't request this code, you can safely ignore this email.</p>
		</div>`, code)

	if sent, err := s.deliver([]string{to}, subject, htmlBody); sent {
		return err
	}

	fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
	return nil
}

// SendInvitationEmail notifies the invitee that they have been invited to a workspace.
// invitationID is included in the URL so the email deep-links to /invite/{id}.
func (s *EmailService) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://app.multica.ai"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)
	subject, htmlBody := buildInvitationEmail(inviterName, workspaceName, inviteURL)

	if sent, err := s.deliver([]string{to}, subject, htmlBody); sent {
		return err
	}

	fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, inviteURL)
	return nil
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

	if sent, err := s.deliver([]string{to}, subject, htmlBody); sent {
		return err
	}

	if strings.TrimSpace(senderName) != "" {
		fmt.Printf("[DEV] Notification email to %s from %s: %s — %s\n", to, strings.TrimSpace(senderName), title, link)
	} else {
		fmt.Printf("[DEV] Notification email to %s: %s — %s\n", to, title, link)
	}
	return nil
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

// buildInvitationParams assembles the Resend payload for an invitation.
func buildInvitationParams(from, to, inviterName, workspaceName, inviteURL string) *resend.SendEmailRequest {
	subject, htmlBody := buildInvitationEmail(inviterName, workspaceName, inviteURL)
	return &resend.SendEmailRequest{
		From:    from,
		To:      []string{to},
		Subject: subject,
		Html:    htmlBody,
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
