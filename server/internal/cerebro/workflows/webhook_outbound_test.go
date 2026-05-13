package workflows

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// outboundWorkflow is the common workflow projection used by the outbound
// tests. Fixed UUIDs keep error-message assertions deterministic.
func outboundWorkflow(actionConfig ActionConfigWebhookOutbound, secret string) workflow {
	cfg, _ := json.Marshal(actionConfig)
	return workflow{
		id:             mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		workspaceID:    mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		name:           "Test workflow",
		actionType:     ActionWebhookOutbound,
		actionConfig:   cfg,
		outboundSecret: secret,
	}
}

func outboundEvent() TriggerEvent {
	return TriggerEvent{
		EventID:     "evt-out-1",
		WorkspaceID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		IssueID:     "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		Type:        TriggerStatusChanged,
		FromStatus:  "todo",
		ToStatus:    "in_review",
		Raw: map[string]any{
			"issue": map[string]any{
				"title":  "Login bug",
				"status": "in_review",
			},
		},
	}
}

// withTestHTTPServer spins an httptest.Server, runs fn with its URL +
// observed-request channel, and tears down.
func withTestHTTPServer(t *testing.T, statusCode int, fn func(url string, gotReq chan *http.Request, gotBody chan []byte)) {
	t.Helper()
	gotReq := make(chan *http.Request, 1)
	gotBody := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotReq <- r
		gotBody <- body
		w.WriteHeader(statusCode)
	}))
	defer srv.Close()
	fn(srv.URL, gotReq, gotBody)
}

func TestOutboundSSRFGuard_RejectsLoopback(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "")
	cases := []string{
		"https://localhost/hook",
		"https://127.0.0.1/hook",
		"https://[::1]/hook",
		"https://example.internal/hook",
		"https://service.local/hook",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			err := validateOutboundURL(raw, false)
			if err == nil {
				t.Fatalf("expected SSRF rejection for %q", raw)
			}
			if !errors.Is(err, ErrOutboundBadURL) {
				t.Fatalf("expected ErrOutboundBadURL, got %v", err)
			}
		})
	}
}

func TestOutboundSSRFGuard_RejectsPrivateRanges(t *testing.T) {
	// We test the IP-classification step in isolation — the URL parse
	// step's hostname lookup would need real DNS to resolve to these
	// addresses.
	cases := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"169.254.1.1", true},
		{"100.64.0.1", true}, // CGNAT
		{"fc00::1", true},    // IPv6 ULA
		{"fe80::1", true},    // IPv6 link-local
		{"::1", true},        // loopback
		{"127.0.0.1", true},  // loopback
		{"8.8.8.8", false},   // public — fine
		{"1.1.1.1", false},   // public — fine
	}
	for _, c := range cases {
		t.Run(c.ip, func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("parse %q failed", c.ip)
			}
			got := ipForbidden(ip, false)
			if got != c.want {
				t.Errorf("ipForbidden(%q)=%v want %v", c.ip, got, c.want)
			}
		})
	}
}

func TestOutboundSSRFGuard_AllowLocalhostBypass(t *testing.T) {
	// Bypass flag turns off the loopback / private rejection so dev/test
	// can post against httptest servers on 127.0.0.1.
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "1")
	if err := validateOutboundURL("http://127.0.0.1:1234/hook", true); err != nil {
		t.Fatalf("expected bypass to allow loopback, got %v", err)
	}
}

func TestOutboundSSRFGuard_RejectsNonHTTPS(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "")
	err := validateOutboundURL("http://example.com/hook", false)
	if err == nil || !strings.Contains(err.Error(), "scheme must be https") {
		t.Fatalf("expected scheme rejection, got %v", err)
	}
}

func TestOutboundHeaderValidation(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		wantErr bool
	}{
		{"empty", nil, false},
		{"valid", map[string]string{"X-My-Trace": "abc"}, false},
		{"bad chars", map[string]string{"X Spaces": "abc"}, true},
		{"forbidden auth", map[string]string{"Authorization": "Bearer x"}, true},
		{"forbidden cookie", map[string]string{"Cookie": "x=1"}, true},
		{"forbidden multica prefix", map[string]string{"X-Multica-Trigger": "fake"}, true},
		{"forbidden multica prefix lower", map[string]string{"x-multica-trigger": "fake"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateOutboundHeaders(c.headers)
			if c.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}

func TestActionWebhookOutbound_Happy2xx(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "1")
	withTestHTTPServer(t, http.StatusOK, func(url string, _ chan *http.Request, gotBody chan []byte) {
		svc := &Service{enabled: true}
		wf := outboundWorkflow(ActionConfigWebhookOutbound{
			URL: url,
		}, "")
		if err := svc.actionWebhookOutbound(context.Background(), wf, outboundEvent()); err != nil {
			t.Fatalf("expected 200 to succeed, got %v", err)
		}
		// Confirm the body has the canonical shape.
		body := <-gotBody
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("body not JSON: %v\n%s", err, body)
		}
		trig, _ := payload["trigger"].(map[string]any)
		if trig["type"] != TriggerStatusChanged {
			t.Errorf("trigger.type = %v", trig["type"])
		}
		if _, ok := payload["fired_at"].(string); !ok {
			t.Error("fired_at missing or not a string")
		}
		issue, _ := payload["issue"].(map[string]any)
		if issue["id"] != "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" {
			t.Errorf("issue.id default snapshot = %v", issue["id"])
		}
	})
}

func TestActionWebhookOutbound_Includes_HMACAndCustomHeaders(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "1")
	secret := "out-secret"
	withTestHTTPServer(t, http.StatusOK, func(url string, gotReq chan *http.Request, gotBody chan []byte) {
		svc := &Service{enabled: true}
		wf := outboundWorkflow(ActionConfigWebhookOutbound{
			URL:     url,
			Headers: map[string]string{"X-My-Trace": "abc-123"},
		}, secret)
		if err := svc.actionWebhookOutbound(context.Background(), wf, outboundEvent()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		req := <-gotReq
		body := <-gotBody
		// Custom header should be passed through.
		if got := req.Header.Get("X-My-Trace"); got != "abc-123" {
			t.Errorf("custom header missing: %q", got)
		}
		// Framework headers must always be set.
		if req.Header.Get("Content-Type") != "application/json" {
			t.Error("missing/wrong Content-Type")
		}
		if req.Header.Get("X-Multica-Workflow-Id") == "" {
			t.Error("missing X-Multica-Workflow-Id")
		}
		// Signature must match HMAC of body.
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got := req.Header.Get("X-Multica-Signature"); got != want {
			t.Errorf("signature = %q, want %q", got, want)
		}
	})
}

func TestActionWebhookOutbound_RetryOn5xx(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "1")
	withTestHTTPServer(t, http.StatusInternalServerError, func(url string, _ chan *http.Request, _ chan []byte) {
		svc := &Service{enabled: true}
		wf := outboundWorkflow(ActionConfigWebhookOutbound{URL: url}, "")
		err := svc.actionWebhookOutbound(context.Background(), wf, outboundEvent())
		if err == nil {
			t.Fatal("expected non-2xx to surface as error")
		}
		if !strings.Contains(err.Error(), "non-2xx") {
			t.Errorf("expected non-2xx error, got %v", err)
		}
	})
}

func TestActionWebhookOutbound_IncludeIssueSnapshot(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "1")
	withTestHTTPServer(t, http.StatusAccepted, func(url string, _ chan *http.Request, gotBody chan []byte) {
		svc := &Service{enabled: true}
		wf := outboundWorkflow(ActionConfigWebhookOutbound{
			URL:                  url,
			IncludeIssueSnapshot: true,
		}, "")
		if err := svc.actionWebhookOutbound(context.Background(), wf, outboundEvent()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := <-gotBody
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		issue, _ := payload["issue"].(map[string]any)
		if issue["title"] != "Login bug" {
			t.Errorf("snapshot title missing: got %v", issue["title"])
		}
		if issue["status"] != "in_review" {
			t.Errorf("snapshot status missing: got %v", issue["status"])
		}
	})
}

func TestActionWebhookOutbound_SSRFBlocksBadURLAtRuntime(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "")
	svc := &Service{enabled: true}
	wf := outboundWorkflow(ActionConfigWebhookOutbound{
		URL: "http://localhost/hook", // http + localhost — should be blocked
	}, "")
	err := svc.actionWebhookOutbound(context.Background(), wf, outboundEvent())
	if err == nil {
		t.Fatal("expected SSRF rejection")
	}
	if !errors.Is(err, ErrOutboundBadURL) {
		t.Fatalf("expected ErrOutboundBadURL, got %v", err)
	}
}

func TestActionWebhookOutbound_BadConfigSurfaces(t *testing.T) {
	svc := &Service{enabled: true}
	wf := outboundWorkflow(ActionConfigWebhookOutbound{}, "")
	err := svc.actionWebhookOutbound(context.Background(), wf, outboundEvent())
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected url-required, got %v", err)
	}
}

func TestComputeOutboundSignature_FormatAndDeterminism(t *testing.T) {
	a := computeOutboundSignature("k", []byte("hello"))
	b := computeOutboundSignature("k", []byte("hello"))
	if a != b {
		t.Fatal("expected determinism")
	}
	if !strings.HasPrefix(a, "sha256=") {
		t.Fatalf("missing prefix: %q", a)
	}
}

// Workflow w/o secret must still post but with no X-Multica-Signature header.
func TestActionWebhookOutbound_NoSecret_NoSignatureHeader(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "1")
	withTestHTTPServer(t, http.StatusOK, func(url string, gotReq chan *http.Request, _ chan []byte) {
		svc := &Service{enabled: true}
		wf := outboundWorkflow(ActionConfigWebhookOutbound{URL: url}, "")
		if err := svc.actionWebhookOutbound(context.Background(), wf, outboundEvent()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		req := <-gotReq
		if got := req.Header.Get("X-Multica-Signature"); got != "" {
			t.Errorf("expected no signature header when secret unset, got %q", got)
		}
	})
}
