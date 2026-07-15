package service

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"testing"
)

type fakeSMTPAuthClient struct {
	authErrs   []error
	authCalls  []smtp.Auth
	authLine   string
	textClient *textproto.Conn
}

func (f *fakeSMTPAuthClient) Auth(auth smtp.Auth) error {
	f.authCalls = append(f.authCalls, auth)
	if len(f.authErrs) == 0 {
		return nil
	}
	err := f.authErrs[0]
	f.authErrs = f.authErrs[1:]
	return err
}

func (f *fakeSMTPAuthClient) Text() *textproto.Conn {
	return f.textClient
}

func (f *fakeSMTPAuthClient) Extension(name string) (bool, string) {
	if strings.EqualFold(name, "AUTH") && f.authLine != "" {
		return true, f.authLine
	}
	return false, ""
}

func TestSMTPAuthWithFallback_UsesPlainWhenAccepted(t *testing.T) {
	client := &fakeSMTPAuthClient{}
	fallback, err := smtpAuthWithFallback(client, "smtp.office365.com", "user", "pass")
	if err != nil {
		t.Fatalf("smtpAuthWithFallback returned error: %v", err)
	}
	if fallback {
		t.Fatalf("expected no fallback when PLAIN auth succeeds")
	}
	if len(client.authCalls) != 1 {
		t.Fatalf("expected 1 auth call, got %d", len(client.authCalls))
	}
	if _, ok := client.authCalls[0].(*loginAuth); ok {
		t.Fatalf("expected first auth to be PLAIN, got LOGIN")
	}
}

func TestSMTPAuthWithFallback_FallsBackToLoginOnOffice365Style504(t *testing.T) {
	client := &fakeSMTPAuthClient{
		authErrs: []error{
			errors.New("504 5.7.4 Unrecognized authentication type"),
			nil,
		},
		authLine: "XOAUTH2 LOGIN",
	}
	fallback, err := smtpAuthWithFallback(client, "smtp.office365.com", "user", "pass")
	if !fallback {
		t.Fatalf("expected fallback signal when Office 365 rejects PLAIN auth")
	}
	if err == nil {
		t.Fatalf("expected original PLAIN auth error to be returned for reconnect path")
	}
	if len(client.authCalls) != 1 {
		t.Fatalf("expected 1 auth call before reconnect, got %d", len(client.authCalls))
	}
	if _, ok := client.authCalls[0].(*loginAuth); ok {
		t.Fatalf("expected first auth attempt to remain PLAIN")
	}
}

func TestSMTPAuthWithFallback_DoesNotFallbackWithoutLoginSupport(t *testing.T) {
	wantErr := errors.New("504 5.7.4 Unrecognized authentication type")
	client := &fakeSMTPAuthClient{
		authErrs: []error{wantErr},
		authLine: "XOAUTH2",
	}
	fallback, err := smtpAuthWithFallback(client, "smtp.office365.com", "user", "pass")
	if fallback {
		t.Fatalf("did not expect fallback when server does not advertise LOGIN")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected original error, got %v", err)
	}
	if len(client.authCalls) != 1 {
		t.Fatalf("expected 1 auth call, got %d", len(client.authCalls))
	}
}

func TestSanitizeSubjectField(t *testing.T) {
	long := strings.Repeat("a", 100)
	longRunes := strings.Repeat("深", 100)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain ascii", "Acme", "Acme"},
		{"strips newline", "Acme\nEvil", "AcmeEvil"},
		{"strips crlf header-style", "Acme\r\nBcc: evil@example.com", "AcmeBcc: evil@example.com"},
		{"strips tab", "Acme\tTeam", "AcmeTeam"},
		{"strips unicode control", "Acme\x07Beep", "AcmeBeep"},
		{"preserves non-ascii", "深度学习工作区", "深度学习工作区"},
		{"preserves emoji", "Team 🚀", "Team 🚀"},
		{"truncates long ascii", long, strings.Repeat("a", maxSubjectFieldRunes-1) + "…"},
		{"truncates rune-aware", longRunes, strings.Repeat("深", maxSubjectFieldRunes-1) + "…"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeSubjectField(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeSubjectField(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewEmailService_TLSMode(t *testing.T) {
	tests := []struct {
		name         string
		smtpTLS      string
		smtpPort     string
		wantImplicit bool
	}{
		{"unset on 465 auto-enables implicit", "", "465", true},
		{"unset on 587 stays starttls", "", "587", false},
		{"unset default port stays starttls", "", "", false},
		{"explicit implicit on 587 forces SMTPS", "implicit", "587", true},
		{"smtps alias", "smtps", "587", true},
		{"ssl alias", "ssl", "587", true},
		{"explicit starttls on 465 overrides auto-detect", "starttls", "465", false},
		{"case-insensitive", "IMPLICIT", "587", true},
		{"trims whitespace", "  implicit  ", "587", true},
		{"unknown value falls back to starttls", "tls", "465", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Isolate from any ambient mail config so only SMTP_TLS/SMTP_PORT drive the result.
			t.Setenv("RESEND_API_KEY", "")
			t.Setenv("SMTP_HOST", "smtp.example.com")
			t.Setenv("SMTP_PORT", tt.smtpPort)
			t.Setenv("SMTP_TLS", tt.smtpTLS)

			s := NewEmailService()
			if s.smtpTLSImplicit != tt.wantImplicit {
				t.Errorf("SMTP_TLS=%q SMTP_PORT=%q: smtpTLSImplicit = %v, want %v",
					tt.smtpTLS, tt.smtpPort, s.smtpTLSImplicit, tt.wantImplicit)
			}
		})
	}
}

func TestNewEmailService_EHLOName(t *testing.T) {
	tests := []struct {
		name    string
		ehloEnv string
		want    string // when fromEnv is false, the os.Hostname() fallback is expected instead
		fromEnv bool
	}{
		{"explicit name used verbatim", "mail.example.com", "mail.example.com", true},
		{"explicit name is trimmed", "  mail.example.com  ", "mail.example.com", true},
		{"unset falls back to hostname", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Isolate from ambient mail config so only SMTP_EHLO_NAME drives the result.
			t.Setenv("RESEND_API_KEY", "")
			t.Setenv("SMTP_HOST", "smtp.example.com")
			t.Setenv("SMTP_EHLO_NAME", tt.ehloEnv)

			s := NewEmailService()
			if tt.fromEnv {
				if s.smtpEHLOName != tt.want {
					t.Errorf("SMTP_EHLO_NAME=%q: smtpEHLOName = %q, want %q", tt.ehloEnv, s.smtpEHLOName, tt.want)
				}
				return
			}
			// Unset: must mirror os.Hostname() exactly — including the empty result if
			// Hostname() errors, which makes sendSMTP skip the EHLO override.
			want, _ := os.Hostname()
			if s.smtpEHLOName != want {
				t.Errorf("SMTP_EHLO_NAME unset: smtpEHLOName = %q, want os.Hostname() %q", s.smtpEHLOName, want)
			}
		})
	}
}

func TestNewEmailService_FromEmailResolution(t *testing.T) {
	tests := []struct {
		name          string
		smtpHost      string
		smtpUsername  string
		smtpFromEmail string
		resendFrom    string
		want          string
	}{
		{
			name:       "resend mode uses resend from",
			resendFrom: "resend@example.com",
			want:       "resend@example.com",
		},
		{
			name:          "smtp mode prefers smtp from",
			smtpHost:      "smtp.example.com",
			smtpUsername:  "auth@example.com",
			smtpFromEmail: "sender@example.com",
			resendFrom:    "resend@example.com",
			want:          "sender@example.com",
		},
		{
			name:         "smtp mode falls back to resend from",
			smtpHost:     "smtp.example.com",
			smtpUsername: "auth@example.com",
			resendFrom:   "resend@example.com",
			want:         "resend@example.com",
		},
		{
			name: "default",
			want: "noreply@multica.ai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("RESEND_API_KEY", "")
			t.Setenv("SMTP_HOST", tt.smtpHost)
			t.Setenv("SMTP_USERNAME", tt.smtpUsername)
			t.Setenv("SMTP_FROM_EMAIL", tt.smtpFromEmail)
			t.Setenv("RESEND_FROM_EMAIL", tt.resendFrom)

			s := NewEmailService()
			if s.fromEmail != tt.want {
				t.Fatalf("fromEmail = %q, want %q", s.fromEmail, tt.want)
			}
		})
	}
}

func TestSendSMTPRequiresConfiguredFromEmail(t *testing.T) {
	s := &EmailService{
		smtpHost:     "127.0.0.1",
		smtpPort:     "1",
		smtpUsername: "auth@example.com",
		smtpPassword: "testpass",
	}

	err := s.sendSMTP("to@example.com", "Test Subject", "<p>Hello</p>")
	if err == nil {
		t.Fatal("expected missing from email error")
	}
	if got := err.Error(); got != "SMTP_FROM_EMAIL or RESEND_FROM_EMAIL is required when SMTP_HOST is set" {
		t.Fatalf("error = %q, want missing from email error", got)
	}
}

func TestBuildInvitationParams_EscapesHTMLInBody(t *testing.T) {
	tests := []struct {
		name          string
		inviter       string
		workspace     string
		wantInBody    []string
		wantNotInBody []string
	}{
		{
			name:      "escapes script tag in inviter",
			inviter:   "<script>alert(1)</script>",
			workspace: "Acme",
			wantInBody: []string{
				"&lt;script&gt;alert(1)&lt;/script&gt;",
			},
			wantNotInBody: []string{
				"<script>alert(1)</script>",
			},
		},
		{
			name:      "escapes attribute-break payload in inviter",
			inviter:   `Alice" onclick="evil()`,
			workspace: "Acme",
			wantNotInBody: []string{
				`Alice" onclick="evil()`,
			},
		},
		{
			name:      "escapes anchor tag in workspace",
			inviter:   "Alice",
			workspace: `<a href="https://evil.example">Click</a>`,
			wantInBody: []string{
				"&lt;a href=",
				"&gt;Click&lt;/a&gt;",
			},
			wantNotInBody: []string{
				`<a href="https://evil.example">Click</a>`,
			},
		},
		{
			name:      "benign text unchanged",
			inviter:   "Alice",
			workspace: "Acme",
			wantInBody: []string{
				"Alice",
				"Acme",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := buildInvitationParams(
				"noreply@multica.ai",
				"invitee@example.com",
				tt.inviter,
				tt.workspace,
				"https://multica.ai/invite/abc-123",
			)
			for _, needle := range tt.wantInBody {
				if !strings.Contains(p.Html, needle) {
					t.Errorf("body missing %q\nbody: %s", needle, p.Html)
				}
			}
			for _, needle := range tt.wantNotInBody {
				if strings.Contains(p.Html, needle) {
					t.Errorf("body should not contain raw %q\nbody: %s", needle, p.Html)
				}
			}
		})
	}
}

func TestBuildInvitationParams_SubjectStripsControls(t *testing.T) {
	p := buildInvitationParams(
		"noreply@multica.ai",
		"invitee@example.com",
		"Alice\r\n",
		"Acme\t",
		"https://multica.ai/invite/abc",
	)
	if strings.ContainsAny(p.Subject, "\r\n\t") {
		t.Errorf("subject still contains control characters: %q", p.Subject)
	}
	if p.Subject != "Alice invited you to Acme on Multica" {
		t.Errorf("unexpected subject: %q", p.Subject)
	}
}

func TestBuildInvitationParams_SubjectNotHTMLEscaped(t *testing.T) {
	// Subject is not HTML-rendered; entities would render literally in inboxes.
	p := buildInvitationParams(
		"noreply@multica.ai",
		"invitee@example.com",
		"Alice",
		"Acme & Co.",
		"https://multica.ai/invite/abc",
	)
	if strings.Contains(p.Subject, "&amp;") {
		t.Errorf("subject should not be HTML-escaped, got %q", p.Subject)
	}
	if !strings.Contains(p.Subject, "Acme & Co.") {
		t.Errorf("subject missing literal ampersand: %q", p.Subject)
	}
}

func TestBuildInvitationParams_SubjectTruncated(t *testing.T) {
	longWorkspace := strings.Repeat("A", 200)
	p := buildInvitationParams(
		"noreply@multica.ai",
		"invitee@example.com",
		"Alice",
		longWorkspace,
		"https://multica.ai/invite/abc",
	)
	// Template: "Alice invited you to <ws> on Multica"
	// ws is capped at maxSubjectFieldRunes; overall subject should also be bounded.
	maxExpected := len("Alice invited you to  on Multica") + maxSubjectFieldRunes
	if runes := len([]rune(p.Subject)); runes > maxExpected {
		t.Errorf("subject not bounded: %d runes, max %d: %q", runes, maxExpected, p.Subject)
	}
	if !strings.Contains(p.Subject, "…") {
		t.Errorf("truncated subject should contain ellipsis marker: %q", p.Subject)
	}
}

func TestBuildInvitationParams_ToAndFromPassedThrough(t *testing.T) {
	p := buildInvitationParams(
		"noreply@multica.ai",
		"invitee@example.com",
		"Alice",
		"Acme",
		"https://multica.ai/invite/abc",
	)
	if p.From != "noreply@multica.ai" {
		t.Errorf("From = %q", p.From)
	}
	if len(p.To) != 1 || p.To[0] != "invitee@example.com" {
		t.Errorf("To = %v", p.To)
	}
	if !strings.Contains(p.Html, "https://multica.ai/invite/abc") {
		t.Errorf("body missing invite URL: %s", p.Html)
	}
}

// --- loginAuth.Start security tests ---

func TestLoginAuth_Start_RefusesUnencryptedRemote(t *testing.T) {
	auth := &loginAuth{username: "user", password: "pass", host: "smtp.office365.com"}
	_, _, err := auth.Start(&smtp.ServerInfo{
		Name: "smtp.office365.com",
		TLS:  false,
	})
	if err == nil {
		t.Fatal("expected error for unencrypted remote connection")
	}
	if !strings.Contains(err.Error(), "unencrypted connection") {
		t.Errorf("expected 'unencrypted connection' error, got: %v", err)
	}
}

func TestLoginAuth_Start_AllowsTLS(t *testing.T) {
	auth := &loginAuth{username: "user", password: "pass", host: "smtp.office365.com"}
	_, _, err := auth.Start(&smtp.ServerInfo{
		Name: "smtp.office365.com",
		TLS:  true,
	})
	if err != nil {
		t.Fatalf("expected no error for TLS connection, got: %v", err)
	}
}

func TestLoginAuth_Start_AllowsLocalhost(t *testing.T) {
	auth := &loginAuth{username: "user", password: "pass", host: "localhost"}
	_, _, err := auth.Start(&smtp.ServerInfo{
		Name: "localhost",
		TLS:  false,
	})
	if err != nil {
		t.Fatalf("expected no error for localhost connection, got: %v", err)
	}
}

func TestLoginAuth_Start_RejectsWrongHost(t *testing.T) {
	auth := &loginAuth{username: "user", password: "pass", host: "smtp.office365.com"}
	_, _, err := auth.Start(&smtp.ServerInfo{
		Name: "evil-relay.example.com",
		TLS:  true,
	})
	if err == nil {
		t.Fatal("expected error for host mismatch")
	}
	if !strings.Contains(err.Error(), "wrong host name") {
		t.Errorf("expected 'wrong host name' error, got: %v", err)
	}
}

func TestLoginAuth_Start_AllowsLoopbackIPs(t *testing.T) {
	for _, name := range []string{"127.0.0.1", "::1"} {
		auth := &loginAuth{username: "user", password: "pass", host: name}
		_, _, err := auth.Start(&smtp.ServerInfo{
			Name: name,
			TLS:  false,
		})
		if err != nil {
			t.Errorf("expected no error for %s, got: %v", name, err)
		}
	}
}

// --- sendSMTP no panic on openSMTPClient failure ---

func TestSendSMTP_OpenClientFailureNoPanic(t *testing.T) {
	s := &EmailService{
		fromEmail:    "from@example.com",
		smtpHost:     "255.255.255.255", // unroutable, will time out or fail
		smtpPort:     "25",
		smtpUsername: "user",
		smtpPassword: "pass",
	}
	err := s.sendSMTP("to@example.com", "Subject", "<p>body</p>")
	if err == nil {
		t.Fatal("expected error from unreachable SMTP server")
	}
	// The important assertion: we reached here without panicking.
	t.Logf("sendSMTP correctly returned error: %v", err)
}

// --- Full sendSMTP flow tests with a mock SMTP server ---

// testSMTPServer is a minimal SMTP server that can simulate Office 365-style
// PLAIN auth rejection followed by LOGIN auth acceptance.
type testSMTPServer struct {
	Listener net.Listener
	Addr     string

	// Auth mechs advertised in EHLO response (e.g. "LOGIN" or "PLAIN LOGIN")
	AuthMechs string
	// If true, AUTH PLAIN returns 504; otherwise it succeeds
	RejectPlain  bool
	ExpectedUser string
	ExpectedPass string
	// If true, advertise STARTTLS in EHLO
	AdvertiseSTARTTLS bool
}

func startTestSMTPServer(t *testing.T, cfg testSMTPServer) (*testSMTPServer, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	cfg.Listener = l
	cfg.Addr = l.Addr().String()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go cfg.handleConn(conn)
		}
	}()

	cleanup := func() {
		l.Close()
		<-done
	}
	return &cfg, cleanup
}

func (s *testSMTPServer) handleConn(conn net.Conn) {
	defer conn.Close()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	writeLine := func(format string, args ...interface{}) {
		fmt.Fprintf(rw, format+"\r\n", args...)
		rw.Flush()
	}
	readLine := func() string {
		line, err := rw.ReadString('\n')
		if err != nil {
			return ""
		}
		return strings.TrimRight(line, "\r\n")
	}

	writeLine("220 test-smtp ESMTP")

	// Wait for EHLO
	ehloLine := readLine()
	if !strings.HasPrefix(strings.ToUpper(ehloLine), "EHLO") {
		writeLine("500 unrecognized command")
		return
	}

	// Build EHLO response
	writeLine("250-test-smtp Hello")
	if s.AdvertiseSTARTTLS {
		writeLine("250-STARTTLS")
	}
	if s.AuthMechs != "" {
		writeLine("250-AUTH " + s.AuthMechs)
	}
	writeLine("250 OK")

	// Read commands until QUIT
	for {
		line := readLine()
		if line == "" {
			return
		}

		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "AUTH PLAIN") || strings.HasPrefix(upper, "AUTH PLAIN "):
			if s.RejectPlain {
				writeLine("504 5.7.4 Unrecognized authentication type")
				continue
			}
			writeLine("235 2.7.0 Auth succeeded")

		case strings.HasPrefix(upper, "AUTH LOGIN"):
			writeLine("334 VXNlcm5hbWU6") // base64("Username:")
			userLine := readLine()
			userBytes, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(userLine))
			writeLine("334 UGFzc3dvcmQ6") // base64("Password:")
			passLine := readLine()
			passBytes, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(passLine))

			if string(userBytes) == s.ExpectedUser && string(passBytes) == s.ExpectedPass {
				writeLine("235 2.7.0 Auth succeeded")
			} else {
				writeLine("535 5.7.8 Auth failed")
			}

		case strings.HasPrefix(upper, "MAIL FROM:"):
			writeLine("250 OK")

		case strings.HasPrefix(upper, "RCPT TO:"):
			writeLine("250 OK")

		case upper == "DATA":
			writeLine("354 Start mail input; end with <CRLF>.<CRLF>")
			// Read until line containing only "."
			for {
				dataLine := readLine()
				if dataLine == "." {
					break
				}
			}
			writeLine("250 OK")

		case strings.HasPrefix(upper, "STARTTLS"):
			writeLine("220 Ready to start TLS")

		case strings.HasPrefix(upper, "QUIT"):
			writeLine("221 bye")
			return

		default:
			writeLine("500 unrecognized command")
		}
	}
}

func TestSendSMTP_FallbackReconnectsAndAuthsWithLOGIN(t *testing.T) {
	srv, cleanup := startTestSMTPServer(t, testSMTPServer{
		AuthMechs:    "PLAIN LOGIN",
		RejectPlain:  true,
		ExpectedUser: "testuser",
		ExpectedPass: "testpass",
	})
	defer cleanup()
	host, port, _ := net.SplitHostPort(srv.Addr)

	s := &EmailService{
		fromEmail:    "from@example.com",
		smtpHost:     host,
		smtpPort:     port,
		smtpUsername: "testuser",
		smtpPassword: "testpass",
	}
	// smtpEHLOName is empty so net/smtp defaults to "localhost", which the
	// test server accepts. No STARTTLS advertised → plain connection to
	// localhost, which loginAuth.Start allows.

	err := s.sendSMTP("to@example.com", "Test Subject", "<p>Hello</p>")
	if err != nil {
		t.Fatalf("sendSMTP failed: %v", err)
	}
}

func TestSendSMTP_PlainAuthSucceedsWithoutFallback(t *testing.T) {
	srv, cleanup := startTestSMTPServer(t, testSMTPServer{
		AuthMechs:    "PLAIN LOGIN",
		RejectPlain:  false, // PLAIN succeeds
		ExpectedUser: "testuser",
		ExpectedPass: "testpass",
	})
	defer cleanup()
	host, port, _ := net.SplitHostPort(srv.Addr)

	s := &EmailService{
		fromEmail:    "from@example.com",
		smtpHost:     host,
		smtpPort:     port,
		smtpUsername: "testuser",
		smtpPassword: "testpass",
	}

	err := s.sendSMTP("to@example.com", "Test Subject", "<p>Hello</p>")
	if err != nil {
		t.Fatalf("sendSMTP failed: %v", err)
	}
}

func TestSendSMTP_NoAuthWhenUsernameEmpty(t *testing.T) {
	srv, cleanup := startTestSMTPServer(t, testSMTPServer{
		AuthMechs: "PLAIN LOGIN",
	})
	defer cleanup()
	host, port, _ := net.SplitHostPort(srv.Addr)

	s := &EmailService{
		fromEmail: "from@example.com",
		smtpHost:  host,
		smtpPort:  port,
		// smtpUsername is empty → unauthenticated relay
	}

	err := s.sendSMTP("to@example.com", "Test Subject", "<p>Hello</p>")
	if err != nil {
		t.Fatalf("sendSMTP failed for unauthenticated relay: %v", err)
	}
}

func TestSendSMTP_LoginAuthRejectsUnencryptedRemote(t *testing.T) {
	// Simulate a remote server that advertises LOGIN but not STARTTLS.
	// Since the connection is not TLS and not localhost, loginAuth.Start
	// must refuse to send credentials.
	auth := &loginAuth{
		username: "user",
		password: "pass",
		host:     "smtp.remote.example.com",
	}
	_, _, err := auth.Start(&smtp.ServerInfo{
		Name: "smtp.remote.example.com",
		TLS:  false,
	})
	if err == nil {
		t.Fatal("expected error: LOGIN auth on unencrypted remote connection")
	}
	if !strings.Contains(err.Error(), "unencrypted connection") {
		t.Errorf("expected 'unencrypted connection' error, got: %v", err)
	}
}
