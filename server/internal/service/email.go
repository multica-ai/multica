package service

import (
	"crypto/tls"
	"encoding/base64"
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

func currentYear() int {
	return time.Now().Year()
}

// maxSubjectFieldRunes bounds how much user-controlled text (workspace name,
// inviter name) can land in an email Subject. Prevents attackers from stuffing
// a full phishing pitch into a workspace name that gets sent from our domain.
const maxSubjectFieldRunes = 60

type EmailService struct {
	client          *resend.Client
	fromEmail       string
	smtpHost        string
	smtpPort        string
	smtpUsername    string
	smtpPassword    string
	smtpTLSInsecure bool
	smtpTLSImplicit bool
	smtpEHLOName    string
}

type smtpAuthClient interface {
	Auth(smtp.Auth) error
	Extension(string) (bool, string)
}

type smtpClientAdapter struct {
	client *smtp.Client
}

func (a smtpClientAdapter) Auth(auth smtp.Auth) error {
	return a.client.Auth(auth)
}

func (a smtpClientAdapter) Extension(name string) (bool, string) {
	return a.client.Extension(name)
}

func isLocalhost(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}

type loginAuth struct {
	username string
	password string
	host     string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !isLocalhost(server.Name) {
		return "", nil, fmt.Errorf("unencrypted connection")
	}
	if server.Name != a.host {
		return "", nil, fmt.Errorf("wrong host name: %q does not match expected %q", server.Name, a.host)
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}

	raw := strings.TrimSpace(string(fromServer))
	challenge := strings.ToLower(raw)
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
		challenge = strings.ToLower(strings.TrimSpace(string(decoded)))
	}

	switch {
	case strings.Contains(challenge, "username") || strings.Contains(challenge, "user name"):
		return []byte(a.username), nil
	case strings.Contains(challenge, "password"):
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("unexpected LOGIN challenge %q", raw)
	}
}

func smtpAuthWithFallback(c smtpAuthClient, host, username, password string) (bool, error) {
	plainErr := c.Auth(smtp.PlainAuth("", username, password, host))
	if plainErr == nil {
		return false, nil
	}

	msg := strings.ToLower(plainErr.Error())
	if !strings.Contains(msg, "unrecognized authentication type") && !strings.Contains(msg, "504 5.7.4") {
		return false, plainErr
	}

	ok, authLine := c.Extension("AUTH")
	if !ok || !strings.Contains(strings.ToUpper(authLine), "LOGIN") {
		return false, plainErr
	}
	return true, plainErr
}

func (s *EmailService) openSMTPClient() (*smtp.Client, error) {
	addr := net.JoinHostPort(s.smtpHost, s.smtpPort)

	tlsCfg := &tls.Config{
		ServerName:         s.smtpHost,
		InsecureSkipVerify: s.smtpTLSInsecure, //nolint:gosec // opt-in via SMTP_TLS_INSECURE=true
	}

	var conn net.Conn
	var err error
	if s.smtpTLSImplicit {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	if err = conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp set deadline: %w", err)
	}

	c, err := smtp.NewClient(conn, s.smtpHost)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp client: %w", err)
	}

	if s.smtpEHLOName != "" {
		if err = c.Hello(s.smtpEHLOName); err != nil {
			c.Close()
			return nil, fmt.Errorf("smtp EHLO %s: %w", s.smtpEHLOName, err)
		}
	}

	if !s.smtpTLSImplicit {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err = c.StartTLS(tlsCfg); err != nil {
				c.Close()
				return nil, fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}

	return c, nil
}

func NewEmailService() *EmailService {
	apiKey := os.Getenv("RESEND_API_KEY")
	from := strings.TrimSpace(os.Getenv("RESEND_FROM_EMAIL"))
	if from == "" {
		from = "noreply@xct.dev"
	}

	smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	smtpPort := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if smtpPort == "" {
		smtpPort = "25"
	}
	smtpUsername := os.Getenv("SMTP_USERNAME")
	smtpPassword := os.Getenv("SMTP_PASSWORD")
	smtpTLSInsecure := os.Getenv("SMTP_TLS_INSECURE") == "true"

	// EHLO/HELO name, only relevant on the SMTP relay send path. net/smtp defaults
	// to "localhost", which strict relays (e.g. smtp-relay.gmail.com) reject from a
	// public source. Fall back to the machine hostname when SMTP_EHLO_NAME is unset.
	// Resolved only in SMTP mode so the Resend/DEV paths never touch os.Hostname()
	// or emit its failure log.
	var smtpEHLOName string
	if smtpHost != "" {
		smtpEHLOName = strings.TrimSpace(os.Getenv("SMTP_EHLO_NAME"))
		if smtpEHLOName == "" {
			hostname, hostErr := os.Hostname()
			if hostErr != nil {
				// Empty name makes sendSMTP skip Hello() and fall back to net/smtp's
				// lazy "localhost" — which strict relays reject. Surface it so operators
				// know to set SMTP_EHLO_NAME explicitly.
				fmt.Printf("EmailService: os.Hostname() failed (%v); SMTP EHLO falls back to \"localhost\" — set SMTP_EHLO_NAME for strict relays\n", hostErr)
			}
			smtpEHLOName = hostname
		}
	}

	// SMTP_TLS=implicit forces an immediate TLS handshake on connect (SMTPS).
	// Required by providers like Aliyun enterprise mail that only offer port 465
	// SSL and do not advertise STARTTLS. Default (empty / "starttls") preserves
	// the prior STARTTLS-upgrade behavior.
	smtpTLSMode := strings.ToLower(strings.TrimSpace(os.Getenv("SMTP_TLS")))
	smtpTLSImplicit := smtpTLSMode == "implicit" || smtpTLSMode == "smtps" || smtpTLSMode == "ssl"
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
		smtpHost:        smtpHost,
		smtpPort:        smtpPort,
		smtpUsername:    smtpUsername,
		smtpPassword:    smtpPassword,
		smtpTLSInsecure: smtpTLSInsecure,
		smtpTLSImplicit: smtpTLSImplicit,
		smtpEHLOName:    smtpEHLOName,
	}
}

// sendSMTP delivers an HTML email via an SMTP server.
// Supports unauthenticated relay (SMTP_USERNAME empty) and authenticated SMTP.
// Upgrades to STARTTLS when advertised by the server.
// Set SMTP_TLS_INSECURE=true for self-signed or private CA certificates.
func (s *EmailService) sendSMTP(to, subject, htmlBody string) error {
	c, err := s.openSMTPClient()
	if err != nil {
		return err
	}
	defer c.Close()

	if s.smtpUsername != "" {
		fallbackToLogin, authErr := smtpAuthWithFallback(smtpClientAdapter{client: c}, s.smtpHost, s.smtpUsername, s.smtpPassword)
		if authErr != nil {
			if !fallbackToLogin {
				return fmt.Errorf("smtp auth: %w", authErr)
			}

			c.Close()
			c, err = s.openSMTPClient()
			if err != nil {
				return fmt.Errorf("smtp auth: plain auth failed (%v); login reconnect failed: %w", authErr, err)
			}
			defer c.Close()

			if err = c.Auth(&loginAuth{username: s.smtpUsername, password: s.smtpPassword, host: s.smtpHost}); err != nil {
				return fmt.Errorf("smtp auth: plain auth failed (%v); login auth fallback failed: %w", authErr, err)
			}
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

	if err = c.Mail(s.fromEmail); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err = c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp RCPT TO <%s>: %w", to, err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	headers := "From: " + s.fromEmail + "\r\n" +
		"To: " + to + "\r\n" +
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

// SendVerificationCode sends a one-time login code. The code is server-generated
// (6-digit numeric) so no user-controlled text reaches the email body here.
// Delivery priority: SMTP relay → Resend API → DEV stdout.
func (s *EmailService) SendVerificationCode(to, code string) error {
	body := fmt.Sprintf(
		`<div style="font-family: sans-serif; max-width: 400px; margin: 0 auto;">
			<h2>Your verification code</h2>
			<p style="font-size: 32px; font-weight: bold; letter-spacing: 8px; margin: 24px 0;">%s</p>
			<p>This code expires in 10 minutes.</p>
			<p style="color: #666; font-size: 14px;">If you didn't request this code, you can safely ignore this email.</p>
		</div>`, code)

	if s.smtpHost != "" {
		return s.sendSMTP(to, "Your XCT Workspace verification code", body)
	}
	if s.client == nil {
		fmt.Printf("[DEV] Verification code for %s: %s\n", to, code)
		return nil
	}
	params := &resend.SendEmailRequest{
		From:    s.fromEmail,
		To:      []string{to},
		Subject: "Your XCT Workspace verification code",
		Html: fmt.Sprintf(
			`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="margin:0;padding:0;background-color:#f4f4f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background-color:#f4f4f5;padding:40px 20px;">
<tr><td align="center">
<table role="presentation" width="480" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 1px 3px rgba(0,0,0,0.08);">
  <tr><td style="background-color:#18181b;padding:24px 32px;text-align:center;">
    <span style="color:#ffffff;font-size:18px;font-weight:600;letter-spacing:0.5px;">XCT Workspace</span>
  </td></tr>
  <tr><td style="padding:40px 32px;">
    <h2 style="margin:0 0 8px;font-size:20px;font-weight:600;color:#18181b;">Verification Code</h2>
    <p style="margin:0 0 24px;font-size:14px;color:#71717a;line-height:1.5;">Enter the following code to verify your identity. This code is valid for 10 minutes.</p>
    <table role="presentation" width="100%%" cellpadding="0" cellspacing="0">
      <tr><td style="background-color:#f4f4f5;border-radius:8px;padding:20px;text-align:center;">
        <span style="font-size:36px;font-weight:700;letter-spacing:10px;color:#18181b;font-family:'Courier New',monospace;">%s</span>
      </td></tr>
    </table>
    <p style="margin:24px 0 0;font-size:13px;color:#a1a1aa;line-height:1.5;">If you didn't request this code, you can safely ignore this email. Someone may have entered your email address by mistake.</p>
  </td></tr>
  <tr><td style="padding:20px 32px;border-top:1px solid #e4e4e7;text-align:center;">
    <p style="margin:0;font-size:12px;color:#a1a1aa;">© %d XCT Workspace. All rights reserved.</p>
  </td></tr>
</table>
</td></tr>
</table>
</body>
</html>`, code, currentYear()),

	}
	_, err := s.client.Emails.Send(params)
	return err
}

// SendInvitationEmail notifies the invitee that they have been invited to a workspace.
// invitationID is included in the URL so the email deep-links to /invite/{id}.
func (s *EmailService) SendInvitationEmail(to, inviterName, workspaceName, invitationID string) error {
	appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if appURL == "" {
		appURL = "https://xct.dev"
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", appURL, invitationID)

	if s.smtpHost != "" {
		params := buildInvitationParams(s.fromEmail, to, inviterName, workspaceName, inviteURL)
		return s.sendSMTP(to, params.Subject, params.Html)
	}
	if s.client == nil {
		fmt.Printf("[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, inviteURL)
		return nil
	}
	params := buildInvitationParams(s.fromEmail, to, inviterName, workspaceName, inviteURL)
	_, err := s.client.Emails.Send(params)
	return err
}

// buildInvitationParams assembles the email request for an invitation.
// Separated from SendInvitationEmail so the sanitization behavior is unit-testable
// without needing to mock the Resend SDK or an SMTP server.
func buildInvitationParams(from, to, inviterName, workspaceName, inviteURL string) *resend.SendEmailRequest {
	safeWorkspace := html.EscapeString(workspaceName)
	safeInviter := html.EscapeString(inviterName)
	subjectInviter := sanitizeSubjectField(inviterName)
	subjectWorkspace := sanitizeSubjectField(workspaceName)

	return &resend.SendEmailRequest{
		From:    from,
		To:      []string{to},
		Subject: fmt.Sprintf("%s invited you to %s on XCT Workspace", subjectInviter, subjectWorkspace),
		Html: fmt.Sprintf(
			`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="margin:0;padding:0;background-color:#f4f4f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background-color:#f4f4f5;padding:40px 20px;">
<tr><td align="center">
<table role="presentation" width="520" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 1px 3px rgba(0,0,0,0.08);">
  <tr><td style="background-color:#18181b;padding:24px 32px;text-align:center;">
    <span style="color:#ffffff;font-size:18px;font-weight:600;letter-spacing:0.5px;">XCT Workspace</span>
  </td></tr>
  <tr><td style="padding:40px 32px;">
    <h2 style="margin:0 0 8px;font-size:20px;font-weight:600;color:#18181b;">You're invited to join %s</h2>
    <p style="margin:0 0 24px;font-size:15px;color:#3f3f46;line-height:1.6;"><strong style="color:#18181b;">%s</strong> invited you to collaborate in the <strong style="color:#18181b;">%s</strong> workspace.</p>
    <table role="presentation" width="100%%" cellpadding="0" cellspacing="0">
      <tr><td align="center" style="padding:8px 0 24px;">
        <a href="%s" style="display:inline-block;padding:14px 32px;background-color:#18181b;color:#ffffff;text-decoration:none;border-radius:8px;font-size:15px;font-weight:600;">Accept Invitation</a>
      </td></tr>
    </table>
    <p style="margin:0;font-size:13px;color:#a1a1aa;line-height:1.5;">You'll need to sign in or create an account to accept this invitation. If you weren't expecting this email, you can safely ignore it.</p>
  </td></tr>
  <tr><td style="padding:20px 32px;border-top:1px solid #e4e4e7;text-align:center;">
    <p style="margin:0;font-size:12px;color:#a1a1aa;">© %d XCT Workspace. All rights reserved.</p>
  </td></tr>
</table>
</td></tr>
</table>
</body>
</html>`, safeWorkspace, safeInviter, safeWorkspace, inviteURL, currentYear()),
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
