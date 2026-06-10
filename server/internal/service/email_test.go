package service

import (
	"os"
	"strings"
	"testing"
)

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
			_, body := buildInvitationEmail(
				tt.inviter,
				tt.workspace,
				"https://app.multica.ai/invite/abc-123",
			)
			for _, needle := range tt.wantInBody {
				if !strings.Contains(body, needle) {
					t.Errorf("body missing %q\nbody: %s", needle, body)
				}
			}
			for _, needle := range tt.wantNotInBody {
				if strings.Contains(body, needle) {
					t.Errorf("body should not contain raw %q\nbody: %s", needle, body)
				}
			}
		})
	}
}

func TestBuildInvitationEmail_SubjectStripsControls(t *testing.T) {
	subject, _ := buildInvitationEmail(
		"Alice\r\n",
		"Acme\t",
		"https://app.multica.ai/invite/abc",
	)
	if strings.ContainsAny(subject, "\r\n\t") {
		t.Errorf("subject still contains control characters: %q", subject)
	}
	if subject != "Alice invited you to Acme on Multica" {
		t.Errorf("unexpected subject: %q", subject)
	}
}

func TestBuildInvitationEmail_SubjectNotHTMLEscaped(t *testing.T) {
	// Subject is not HTML-rendered; entities would render literally in inboxes.
	subject, _ := buildInvitationEmail(
		"Alice",
		"Acme & Co.",
		"https://app.multica.ai/invite/abc",
	)
	if strings.Contains(subject, "&amp;") {
		t.Errorf("subject should not be HTML-escaped, got %q", subject)
	}
	if !strings.Contains(subject, "Acme & Co.") {
		t.Errorf("subject missing literal ampersand: %q", subject)
	}
}

func TestBuildInvitationEmail_SubjectTruncated(t *testing.T) {
	longWorkspace := strings.Repeat("A", 200)
	subject, _ := buildInvitationEmail(
		"Alice",
		longWorkspace,
		"https://app.multica.ai/invite/abc",
	)
	// Template: "Alice invited you to <ws> on Multica"
	// ws is capped at maxSubjectFieldRunes; overall subject should also be bounded.
	maxExpected := len("Alice invited you to  on Multica") + maxSubjectFieldRunes
	if runes := len([]rune(subject)); runes > maxExpected {
		t.Errorf("subject not bounded: %d runes, max %d: %q", runes, maxExpected, subject)
	}
	if !strings.Contains(subject, "…") {
		t.Errorf("truncated subject should contain ellipsis marker: %q", subject)
	}
}

func TestBuildInvitationEmail_BodyContainsInviteURL(t *testing.T) {
	_, body := buildInvitationEmail(
		"Alice",
		"Acme",
		"https://app.multica.ai/invite/abc",
	)
	if !strings.Contains(body, "https://app.multica.ai/invite/abc") {
		t.Errorf("body missing invite URL: %s", body)
	}
}

func TestSendNotificationEmail_DevMode(t *testing.T) {
	svc := &EmailService{sender: nil, fromEmail: "test@multica.ai"}
	err := svc.SendNotificationEmail("user@example.com", "Test Title", "Test Body", "https://app.multica.ai/issue/123", "Alice")
	if err != nil {
		t.Fatalf("expected nil error in dev mode, got %v", err)
	}
}

func TestSendNotificationEmail_EmptyTitle(t *testing.T) {
	svc := &EmailService{sender: nil, fromEmail: "test@multica.ai"}
	err := svc.SendNotificationEmail("user@example.com", "", "Body text", "", "")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestSendNotificationEmail_HTMLEscaping(t *testing.T) {
	var capturedSubject, capturedBody string
	mockSender := &mockEmailSender{
		sendFn: func(from string, to []string, subject, htmlBody string) error {
			capturedSubject = subject
			capturedBody = htmlBody
			return nil
		},
	}
	svc := &EmailService{sender: mockSender, fromEmail: "test@multica.ai"}

	err := svc.SendNotificationEmail("user@example.com", "<script>alert(1)</script>", "Hello & goodbye", "https://app.multica.ai", "Alice")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if strings.Contains(capturedBody, "<script>") {
		t.Errorf("body should not contain raw script tag: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "&lt;script&gt;") {
		t.Errorf("body should contain escaped script tag: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "Hello &amp; goodbye") {
		t.Errorf("body should contain escaped ampersand: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "View in Multica") {
		t.Errorf("body should contain link button: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, "<strong>From:</strong> Alice") {
		t.Errorf("body should contain sender name: %s", capturedBody)
	}
	_ = capturedSubject
}

func TestSendNotificationEmail_LinkEscaping(t *testing.T) {
	var capturedBody string
	mockSender := &mockEmailSender{
		sendFn: func(from string, to []string, subject, htmlBody string) error {
			capturedBody = htmlBody
			return nil
		},
	}
	svc := &EmailService{sender: mockSender, fromEmail: "test@multica.ai"}

	tests := []struct {
		name        string
		link        string
		wantNot     string
		wantContain string
	}{
		{
			name:        "double quote in link",
			link:        `https://evil.com/x" onclick="alert(1)`,
			wantNot:     `href="https://evil.com/x" onclick="alert(1)"`,
			wantContain: `href="https://evil.com/x&#34; onclick=&#34;alert(1)"`,
		},
		{
			name:        "angle bracket in link",
			link:        `https://evil.com/<script>`,
			wantNot:     `href="https://evil.com/<script>"`,
			wantContain: `&lt;script&gt;`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.SendNotificationEmail("user@example.com", "Test", "Body", tt.link, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(capturedBody, tt.wantNot) {
				t.Errorf("body should not contain unescaped link: %s", capturedBody)
			}
			if !strings.Contains(capturedBody, tt.wantContain) {
				t.Errorf("body should contain escaped link segment %q:\n%s", tt.wantContain, capturedBody)
			}
		})
	}
}

func TestSendNotificationEmail_SenderInSubjectAndBody(t *testing.T) {
	var capturedSubject, capturedBody string
	mockSender := &mockEmailSender{
		sendFn: func(from string, to []string, subject, htmlBody string) error {
			capturedSubject = subject
			capturedBody = htmlBody
			return nil
		},
	}
	svc := &EmailService{sender: mockSender, fromEmail: "test@multica.ai"}

	err := svc.SendNotificationEmail("user@example.com", "OPE-20 · Review", "Please check", "https://app.multica.ai", "Alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedSubject != "Alice mentioned you in OPE-20 · Review" {
		t.Fatalf("unexpected subject: %q", capturedSubject)
	}
	if !strings.Contains(capturedBody, "<strong>From:</strong> Alice") {
		t.Fatalf("body missing sender: %s", capturedBody)
	}
}

func TestBuildNotificationEmailSubject_StripsControls(t *testing.T) {
	subject := buildNotificationEmailSubject("Issue\r\nBcc: evil@example.com", "Alice\t")
	if strings.ContainsAny(subject, "\r\n\t") {
		t.Fatalf("subject still contains control characters: %q", subject)
	}
	if subject != "Alice mentioned you in IssueBcc: evil@example.com" {
		t.Fatalf("unexpected subject: %q", subject)
	}
}

type mockEmailSender struct {
	sendFn func(from string, to []string, subject, htmlBody string) error
}

func (m *mockEmailSender) Send(from string, to []string, subject, htmlBody string) error {
	return m.sendFn(from, to, subject, htmlBody)
}
