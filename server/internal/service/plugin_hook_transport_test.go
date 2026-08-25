package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// These exercise the real outbound path — a live HTTPS server, a real request,
// a real signature — rather than the pieces in isolation. The endpoint runs on
// loopback, which the SSRF guard refuses by design, so the test opts that exact
// origin in through DevOrigins: the same switch a plugin author uses to develop
// a hook against localhost. The consent check is NOT relaxed by it, which is
// what TestHookRefusesEndpointOutsideNetScope pins down.

type hookTestServer struct {
	server   *httptest.Server
	received chan hookReceivedRequest
	respond  func(w http.ResponseWriter, body []byte)
}

type hookReceivedRequest struct {
	Body      []byte
	Signature string
	Timestamp string
	Header    http.Header
}

func newHookTestServer(t *testing.T) *hookTestServer {
	t.Helper()
	harness := &hookTestServer{received: make(chan hookReceivedRequest, 8)}
	harness.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		harness.received <- hookReceivedRequest{
			Body:      body,
			Signature: r.Header.Get("X-Multica-Signature"),
			Timestamp: r.Header.Get("X-Multica-Timestamp"),
			Header:    r.Header.Clone(),
		}
		if harness.respond != nil {
			harness.respond(w, body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(harness.server.Close)
	return harness
}

// hookTestService builds a service whose only outbound destination is the test
// server, with signing enabled.
func hookTestService(t *testing.T, harness *hookTestServer) *PluginService {
	t.Helper()
	service := testSigningService(t)
	service.DevOrigins = []string{harness.server.URL}
	service.HookClient = harness.server.Client()
	service.Callbacks = NewCallbackTokens()
	service.CallbackBaseURL = "https://plugin-api.multica.test/v1"
	return service
}

// hookTestHost is the bare hostname of the test server, which is what a net:
// scope names — the scope is an exact host, never a URL.
func hookTestHost(harness *hookTestServer) string {
	return strings.Split(strings.TrimPrefix(harness.server.URL, "https://"), ":")[0]
}

func hookTestInstallation(t *testing.T, endpoint, netScope string, triggers []string) db.PluginInstallation {
	t.Helper()
	triggerJSON, err := json.Marshal(triggers)
	if err != nil {
		t.Fatalf("marshal triggers: %v", err)
	}
	manifest := fmt.Sprintf(`{
		"manifest_version": 1,
		"key": "com.example.hooktest",
		"name": "Hook Test",
		"description": "d",
		"version": "1.0.0",
		"author": {"name": "example"},
		"scopes": ["issues:read", "net:%s"],
		"contributes": {"hooks": [{
			"key": "summarize",
			"name": "Summarize",
			"description": "Summarize the thread.",
			"triggers": %s,
			"events": ["issue.created"],
			"transport": {"type": "http", "url": "%s"}
		}]}
	}`, netScope, string(triggerJSON), endpoint)

	scopes, err := json.Marshal([]string{"issues:read", "net:" + netScope})
	if err != nil {
		t.Fatalf("marshal scopes: %v", err)
	}
	return db.PluginInstallation{
		ID:            testInstallationID(t),
		WorkspaceID:   testInstallationID(t),
		Enabled:       true,
		Manifest:      []byte(manifest),
		GrantedScopes: scopes,
	}
}

// The receiving end must be able to prove the call came from us, using only the
// signing secret it was configured with.
func TestHookRequestIsSignedAndVerifiableByTheReceiver(t *testing.T) {
	harness := newHookTestServer(t)
	service := hookTestService(t, harness)
	host := hookTestHost(harness)
	installation := hookTestInstallation(t, harness.server.URL+"/hooks/summarize", host, []string{plugincontract.TriggerManual})

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	output, err := service.callHookEndpoint(context.Background(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerManual,
		Actor:        HookActor{Type: "member", ID: testInstallationID(t)},
		Input:        map[string]string{"issue_id": "abc"},
	})
	if err != nil {
		t.Fatalf("hook call: %v", err)
	}
	if string(output) != `{"ok":true}` {
		t.Fatalf("unexpected output %q", string(output))
	}

	select {
	case received := <-harness.received:
		secret, err := service.HookSigningSecret(installation.ID)
		if err != nil {
			t.Fatalf("derive secret: %v", err)
		}
		if err := VerifyHookSignature(secret, received.Timestamp, received.Body, received.Signature, time.Now()); err != nil {
			t.Fatalf("the receiver must be able to verify the signature: %v", err)
		}
		// One byte changed anywhere in the body must break it.
		tampered := append([]byte{}, received.Body...)
		tampered[len(tampered)-2] ^= 0xff
		if err := VerifyHookSignature(secret, received.Timestamp, tampered, received.Signature, time.Now()); err == nil {
			t.Fatal("a tampered body must not verify against the delivered signature")
		}
	default:
		t.Fatal("the endpoint received no request")
	}
}

// The `net:` scope is the whole promise made on the consent screen: this plugin
// sends data to these hosts and no others. DevOrigins must not widen it.
func TestHookRefusesEndpointOutsideNetScope(t *testing.T) {
	harness := newHookTestServer(t)
	service := hookTestService(t, harness)
	// Granted somewhere else entirely, while the transport points at the test
	// server the operator did opt in to.
	installation := hookTestInstallation(t, harness.server.URL+"/hooks/summarize", "elsewhere.example.com", []string{plugincontract.TriggerManual})

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	_, err = service.callHookEndpoint(context.Background(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerManual,
	})
	if err == nil {
		t.Fatal("a destination outside the granted net: scope must be refused")
	}
	if !strings.Contains(err.Error(), "net:") {
		t.Fatalf("the refusal should name the scope, got %v", err)
	}
	select {
	case <-harness.received:
		t.Fatal("no request may leave for an unapproved host")
	default:
	}
}

// An installation with no net: scope cannot call out at all.
func TestHookRefusesWhenNoNetScopeGranted(t *testing.T) {
	harness := newHookTestServer(t)
	service := hookTestService(t, harness)
	installation := hookTestInstallation(t, harness.server.URL+"/hooks/summarize", "example.com", []string{plugincontract.TriggerManual})
	installation.GrantedScopes = []byte(`["issues:read"]`)

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	if _, err := service.callHookEndpoint(context.Background(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerManual,
	}); err == nil {
		t.Fatal("an installation with no net: scope must not reach the network")
	}
}

// The callback token travels in the body, so a handler can answer without ever
// being given standing access.
//
// Deliberately NOT single-use. A real handler reads the issue, decides, then
// writes — two calls minimum. Making the token single-use looked stricter and
// only pushed authors toward the installation's standing token, which never
// expires; this test pins the looser bound as the intended one.
func TestHookCarriesAnInvocationScopedCallbackToken(t *testing.T) {
	harness := newHookTestServer(t)
	service := hookTestService(t, harness)
	host := hookTestHost(harness)
	installation := hookTestInstallation(t, harness.server.URL+"/hooks/summarize", host, []string{plugincontract.TriggerManual})

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	actorID := testInstallationID(t)
	if _, err := service.callHookEndpoint(context.Background(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerManual,
		Actor:        HookActor{Type: "member", ID: actorID},
	}); err != nil {
		t.Fatalf("hook call: %v", err)
	}

	received := <-harness.received
	var body hookRequestBody
	if err := json.Unmarshal(received.Body, &body); err != nil {
		t.Fatalf("decode delivered body: %v", err)
	}
	if body.CallbackToken == "" {
		t.Fatal("the handler needs a callback token to answer with")
	}
	if body.CallbackURL != "https://plugin-api.multica.test/v1" {
		t.Fatalf("unexpected callback url %q", body.CallbackURL)
	}
	if body.Actor.Type != "member" {
		t.Fatalf("a manual trigger must carry the member actor, got %q", body.Actor.Type)
	}

	// Re-issued here because callHookEndpoint revokes the grant once the call it
	// was issued for has returned.
	token, err := service.Callbacks.Issue(context.Background(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerManual,
		Actor:        HookActor{Type: "member", ID: actorID},
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	grant, err := service.Callbacks.Resolve(token)
	if err != nil {
		t.Fatalf("first resolve must succeed: %v", err)
	}
	if grant.Actor.Type != "member" || uuidString(grant.Actor.ID) != uuidString(actorID) {
		t.Fatalf("the grant must carry the actor decided at dispatch, got %+v", grant.Actor)
	}
	// The second call is the one that used to fail. A handler that read the
	// issue and then wanted to comment on it got a 403 here.
	if _, err := service.Callbacks.Resolve(token); err != nil {
		t.Fatalf("a handler must be able to make more than one call with one grant: %v", err)
	}

	// Revoked when the invocation is over, so it does not linger for its TTL.
	service.Callbacks.Revoke(token)
	if _, err := service.Callbacks.Resolve(token); err == nil {
		t.Fatal("a revoked grant must stop resolving")
	}
}

// The host resolved and permission-checked the issue, so it says which one this
// call is about. Without it a handler has to read a client-supplied field —
// unvalidated for ui/manual, and absent entirely for event, where no client was
// involved.
func TestHookBodyCarriesTheIssueTheHostResolved(t *testing.T) {
	harness := newHookTestServer(t)
	service := hookTestService(t, harness)
	host := hookTestHost(harness)
	installation := hookTestInstallation(t, harness.server.URL+"/hooks/summarize", host, []string{plugincontract.TriggerEvent})

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	issueID, err := parseUUIDValue("3fa85f64-5717-4562-b3fc-2c963f66afa6")
	if err != nil {
		t.Fatalf("parse issue id: %v", err)
	}
	if _, err := service.callHookEndpoint(context.Background(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerEvent,
		EventType:    plugincontract.EventIssueCreated,
		Actor:        HookActor{Type: "plugin", ID: installation.ID},
		IssueID:      issueID,
	}); err != nil {
		t.Fatalf("hook call: %v", err)
	}

	received := <-harness.received
	var body hookRequestBody
	if err := json.Unmarshal(received.Body, &body); err != nil {
		t.Fatalf("decode delivered body: %v", err)
	}
	if body.IssueID != uuidString(issueID) {
		t.Fatalf("issue_id = %q, want %q — the handler has no other trustworthy way to know", body.IssueID, uuidString(issueID))
	}
}

// A hook may not be reached through a trigger it never declared, even if the
// caller asks for one the host supports.
func TestInvokeHookRefusesUndeclaredTrigger(t *testing.T) {
	harness := newHookTestServer(t)
	service := hookTestService(t, harness)
	host := hookTestHost(harness)
	installation := hookTestInstallation(t, harness.server.URL+"/hooks/summarize", host, []string{plugincontract.TriggerManual})

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	if _, err := service.InvokeHook(context.Background(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerEvent,
	}, 1); err == nil {
		t.Fatal("the event trigger was not declared and must be refused")
	}
	select {
	case <-harness.received:
		t.Fatal("no request may leave for an undeclared trigger")
	default:
	}
}

// A disabled installation is off, not merely hidden.
func TestInvokeHookRefusesDisabledInstallation(t *testing.T) {
	harness := newHookTestServer(t)
	service := hookTestService(t, harness)
	host := hookTestHost(harness)
	installation := hookTestInstallation(t, harness.server.URL+"/hooks/summarize", host, []string{plugincontract.TriggerManual})
	installation.Enabled = false

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	if _, err := service.InvokeHook(context.Background(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerManual,
	}, 1); err == nil {
		t.Fatal("a disabled installation must not call out")
	}
}

// A failing endpoint must produce an error the caller can act on, and must not
// leak whatever the remote end said into our records.
func TestHookFailureIsRedactedAndClassified(t *testing.T) {
	harness := newHookTestServer(t)
	harness.respond = func(w http.ResponseWriter, _ []byte) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal detail: secret-token-abc123 and an issue title"))
	}
	service := hookTestService(t, harness)
	host := hookTestHost(harness)
	installation := hookTestInstallation(t, harness.server.URL+"/hooks/summarize", host, []string{plugincontract.TriggerManual})

	hook, err := FindHook(installation, "summarize")
	if err != nil {
		t.Fatalf("find hook: %v", err)
	}
	_, err = service.callHookEndpoint(context.Background(), HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerManual,
	})
	if err == nil {
		t.Fatal("a 500 from the endpoint must be an error")
	}
	message := redactHookError(err)
	if strings.Contains(message, "secret-token-abc123") || strings.Contains(message, "issue title") {
		t.Fatalf("the recorded error must not echo the remote body, got %q", message)
	}
	if hookFailureStatus(err) != "failed" {
		t.Fatalf("an unreachable-or-erroring endpoint is 'failed', got %q", hookFailureStatus(err))
	}
}

// Refusals are decisions, not outages. The dispatcher relies on this to avoid
// retrying a call that will be refused identically three times.
func TestHookFailureStatusSeparatesRefusalsFromOutages(t *testing.T) {
	refused := pluginErrf(PluginErrorForbidden, "not granted")
	if hookFailureStatus(refused) != "refused" {
		t.Fatalf("a forbidden error is a refusal, got %q", hookFailureStatus(refused))
	}
	quota := pluginErrf(PluginErrorQuota, "too many")
	if hookFailureStatus(quota) != "refused" {
		t.Fatalf("a quota error is a refusal, got %q", hookFailureStatus(quota))
	}
	outage := &PluginError{Kind: PluginErrorUnavailable, Message: "endpoint did not answer"}
	if hookFailureStatus(outage) != "failed" {
		t.Fatalf("an unavailable endpoint is a failure, got %q", hookFailureStatus(outage))
	}
}
