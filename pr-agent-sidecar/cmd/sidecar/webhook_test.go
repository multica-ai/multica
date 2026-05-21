package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// --- helpers ---------------------------------------------------------------

func signPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newSignedRequest(t *testing.T, secret, event, delivery string, payload []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-Hub-Signature-256", signPayload(secret, payload))
	return req
}

func prEventPayload(action, ownerRepo string, prNumber int, installationID int64) []byte {
	parts := strings.SplitN(ownerRepo, "/", 2)
	owner, name := parts[0], parts[1]
	return []byte(`{
		"action": "` + action + `",
		"number": ` + intStr(prNumber) + `,
		"pull_request": {
			"number": ` + intStr(prNumber) + `,
			"title": "Add feature X",
			"html_url": "https://github.com/` + ownerRepo + `/pull/` + intStr(prNumber) + `",
			"head": {"sha": "abcdef1234567890"},
			"user": {"login": "pranit"}
		},
		"repository": {
			"name": "` + name + `",
			"full_name": "` + ownerRepo + `",
			"owner": {"login": "` + owner + `"}
		},
		"installation": {"id": ` + int64Str(installationID) + `}
	}`)
}

func intStr(n int) string {
	return int64Str(int64(n))
}

func int64Str(n int64) string {
	// avoid strconv import to keep this file lean
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := make([]byte, 0, 20)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

func newTestServer(t *testing.T, multicaURL string, allowlist map[string]struct{}) *Server {
	t.Helper()
	cfg := &Config{
		GitHubAppID:         12345,
		GitHubAppPrivateKey: []byte("not-used-in-webhook-tests"),
		GitHubWebhookSecret: "topsecret",
		MulticaPAT:          "mul_test",
		MulticaBaseURL:      multicaURL,
		MulticaWorkspaceID:  "ws-uuid",
		PRReviewerAgentID:   "agent-uuid",
		RepoAllowlist:       allowlist,
		SidecarPublicURL:    "https://pr-agent.zoop.tools",
		Port:                "9000",
	}
	return NewServer(cfg)
}

// --- tests -----------------------------------------------------------------

func TestWebhook_HappyPath_CreatesMulticaIssue(t *testing.T) {
	var calls int32
	var captured CreateIssueRequest

	multica := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"u-1","identifier":"INV-512"}`)
	}))
	defer multica.Close()

	srv := newTestServer(t, multica.URL, map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()

	payload := prEventPayload("opened", "zoopone/multica", 7, 999)
	req := newSignedRequest(t, "topsecret", "pull_request", "delivery-1", payload)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("multica called %d times, want 1", calls)
	}
	if captured.AssigneeType != "agent" || captured.AssigneeID != "agent-uuid" {
		t.Errorf("captured request = %+v", captured)
	}
	if !strings.Contains(captured.Title, "Review PR #7") {
		t.Errorf("title = %q", captured.Title)
	}
	if !strings.Contains(captured.Description, "Token callback:") {
		t.Errorf("description missing callback line: %q", captured.Description)
	}
	if !strings.Contains(captured.Description, "nonce=") {
		t.Errorf("description missing nonce: %q", captured.Description)
	}
}

func TestWebhook_BadSignature_Returns401(t *testing.T) {
	srv := newTestServer(t, "http://unused", map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()

	payload := prEventPayload("opened", "zoopone/multica", 1, 1)
	req := newSignedRequest(t, "WRONG-secret", "pull_request", "delivery-bad", payload)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rr.Code, rr.Body.String())
	}
}

func TestWebhook_DedupHit_DoesNotCallMultica(t *testing.T) {
	var calls int32
	multica := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"u-1","identifier":"INV-512"}`)
	}))
	defer multica.Close()

	srv := newTestServer(t, multica.URL, map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()

	payload := prEventPayload("opened", "zoopone/multica", 1, 1)

	// First call: should hit multica
	rr1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr1, newSignedRequest(t, "topsecret", "pull_request", "delivery-dup", payload))
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("first call status = %d", rr1.Code)
	}

	// Second call (same delivery ID): should dedup
	rr2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr2, newSignedRequest(t, "topsecret", "pull_request", "delivery-dup", payload))
	if rr2.Code != http.StatusOK {
		t.Fatalf("second call status = %d, want 200 (dedup)", rr2.Code)
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("multica called %d times across two deliveries, want 1", got)
	}

	var body map[string]any
	_ = json.Unmarshal(rr2.Body.Bytes(), &body)
	if body["deduplicated"] != true {
		t.Errorf("expected deduplicated=true in response, got %v", body)
	}
}

func TestWebhook_IgnoredAction_Returns200WithoutMulticaCall(t *testing.T) {
	var calls int32
	multica := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer multica.Close()

	srv := newTestServer(t, multica.URL, map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()

	payload := prEventPayload("closed", "zoopone/multica", 1, 1)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, newSignedRequest(t, "topsecret", "pull_request", "delivery-ignored", payload))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("multica was called for ignored action")
	}
}

func TestWebhook_RepoNotInAllowlist_Returns200WithoutMulticaCall(t *testing.T) {
	var calls int32
	multica := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer multica.Close()

	srv := newTestServer(t, multica.URL, map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()

	payload := prEventPayload("opened", "someone/else", 1, 1)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, newSignedRequest(t, "topsecret", "pull_request", "delivery-notallowed", payload))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("multica was called for non-allowlisted repo")
	}
}

func TestWebhook_NonPREvent_Returns200WithoutMulticaCall(t *testing.T) {
	var calls int32
	multica := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer multica.Close()

	srv := newTestServer(t, multica.URL, map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()

	// Issues event payload — valid JSON, parses to a non-PR event type.
	payload := []byte(`{"action":"opened","issue":{"number":1},"repository":{"full_name":"zoopone/multica"}}`)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, newSignedRequest(t, "topsecret", "issues", "delivery-issues", payload))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("multica was called for non-PR event")
	}
}

func TestWebhook_MulticaError_Returns500(t *testing.T) {
	multica := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))
	defer multica.Close()

	srv := newTestServer(t, multica.URL, map[string]struct{}{"zoopone/multica": {}})
	defer srv.Close()

	payload := prEventPayload("opened", "zoopone/multica", 1, 1)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, newSignedRequest(t, "topsecret", "pull_request", "delivery-err", payload))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}

func TestBuildIssueBody_IncludesAllFields(t *testing.T) {
	body := buildIssueBody(issueBodyData{
		Repo:        "zoopone/multica",
		PRNumber:    42,
		PRTitle:     "Add label filter",
		PRURL:       "https://github.com/zoopone/multica/pull/42",
		HeadSHA:     "abc123",
		Author:      "pranit",
		CallbackURL: "https://pr-agent.zoop.tools/installation-token?nonce=xxx",
	})

	mustContain := []string{
		"zoopone/multica",
		"#42",
		"Add label filter",
		"https://github.com/zoopone/multica/pull/42",
		"abc123",
		"@pranit",
		"nonce=xxx",
		"pr-reviewer",
	}
	for _, s := range mustContain {
		if !strings.Contains(body, s) {
			t.Errorf("body missing %q\nbody:\n%s", s, body)
		}
	}
}
