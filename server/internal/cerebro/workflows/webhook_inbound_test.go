package workflows

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeWebhookInboundQueries supplies a single workflow row keyed by token.
// Returns pgx.ErrNoRows on misses so the production "isNoRows" sentinel
// matches.
type fakeWebhookInboundQueries struct {
	workflows map[string]cerebrodb.CerebroWorkflow
}

func (f *fakeWebhookInboundQueries) GetCerebroWorkflowByInboundToken(_ context.Context, token pgtype.Text) (cerebrodb.CerebroWorkflow, error) {
	if !token.Valid {
		return cerebrodb.CerebroWorkflow{}, pgx.ErrNoRows
	}
	if wf, ok := f.workflows[token.String]; ok {
		return wf, nil
	}
	return cerebrodb.CerebroWorkflow{}, pgx.ErrNoRows
}

// fakeExecutor captures the Execute call so tests can confirm the workflow
// was dispatched (or not).
type fakeExecutor struct {
	enabled    bool
	called     bool
	gotWF      workflow
	gotTE      TriggerEvent
	executeErr error
}

func (f *fakeExecutor) Enabled() bool { return f.enabled }
func (f *fakeExecutor) Execute(_ context.Context, wf workflow, te TriggerEvent) error {
	f.called = true
	f.gotWF = wf
	f.gotTE = te
	return f.executeErr
}
func (f *fakeExecutor) conditionsHold(_ context.Context, _ workflow, _ TriggerEvent) bool { return true }

// fakeIssueLookup gives the inbound handler an in-memory issue map so the
// issue_id-resolution path can be exercised.
type fakeIssueLookup struct {
	issues map[string]db.Issue
}

func (f *fakeIssueLookup) GetIssue(_ context.Context, id pgtype.UUID) (db.Issue, error) {
	if iss, ok := f.issues[uuidString(id)]; ok {
		return iss, nil
	}
	return db.Issue{}, pgx.ErrNoRows
}

// inboundTestSetup wires a chi router with the inbound handler so requests
// hit the same route shape they will in production.
func inboundTestSetup(t *testing.T, h *WebhookInboundHandler) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/cerebro/workflows/webhook/{token}", h.ServeHTTP)
	return r
}

// buildInboundRequest signs the body with the given secret when secret != "".
// Returns the *http.Request ready to drive through the test server.
func buildInboundRequest(token string, ts time.Time, body []byte, secret string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/cerebro/workflows/webhook/"+token, bytes.NewReader(body))
	r.Header.Set("X-Multica-Timestamp", fmt.Sprintf("%d", ts.Unix()))
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(fmt.Sprintf("%d", ts.Unix())))
		mac.Write([]byte("."))
		mac.Write(body)
		r.Header.Set("X-Multica-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	return r
}

func TestWebhookInbound_HappyPath_NoSecret(t *testing.T) {
	wfID := mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc")
	wsID := mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	token := "tok_happy_path_aaaaaaaaaaaaaaaaaaa"
	row := cerebrodb.CerebroWorkflow{
		ID:                  wfID,
		WorkspaceID:         wsID,
		Enabled:             true,
		TriggerType:         TriggerWebhookInbound,
		ActionType:          ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	exec := &fakeExecutor{enabled: true}
	h := &WebhookInboundHandler{Cerebro: queries, Service: exec}
	srv := inboundTestSetup(t, h)

	req := buildInboundRequest(token, time.Now(), []byte(`{"foo":"bar"}`), "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202\nbody: %s", w.Code, w.Body.String())
	}
	if !exec.called {
		t.Fatal("Execute must be called on accepted request")
	}
	if exec.gotTE.Type != TriggerWebhookInbound {
		t.Errorf("trigger type = %q", exec.gotTE.Type)
	}
	payload, _ := exec.gotTE.Raw["payload"].(map[string]any)
	if payload["foo"] != "bar" {
		t.Errorf("payload not decoded: %v", payload)
	}
}

func TestWebhookInbound_UnknownToken_404(t *testing.T) {
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: &fakeExecutor{enabled: true}}
	srv := inboundTestSetup(t, h)

	req := buildInboundRequest("unknown_token_xxxxxx", time.Now(), []byte(`{}`), "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestWebhookInbound_DisabledWorkflow_403(t *testing.T) {
	token := "tok_disabled_xxxxxxxxxxx"
	row := cerebrodb.CerebroWorkflow{
		ID:                  mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		WorkspaceID:         mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Enabled:             false,
		TriggerType:         TriggerWebhookInbound,
		ActionType:          ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	exec := &fakeExecutor{enabled: true}
	h := &WebhookInboundHandler{Cerebro: queries, Service: exec}
	srv := inboundTestSetup(t, h)

	req := buildInboundRequest(token, time.Now(), []byte(`{}`), "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if exec.called {
		t.Fatal("Execute must NOT be called when workflow is disabled")
	}
}

func TestWebhookInbound_MissingTimestamp_401(t *testing.T) {
	token := "tok_no_ts_xxxxxxxxxxx"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: &fakeExecutor{enabled: true}}
	srv := inboundTestSetup(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/cerebro/workflows/webhook/"+token, strings.NewReader(`{}`))
	// No X-Multica-Timestamp.
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401\nbody: %s", w.Code, w.Body.String())
	}
}

func TestWebhookInbound_StaleTimestamp_401(t *testing.T) {
	token := "tok_stale_xxxxxxxxxxx"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: &fakeExecutor{enabled: true}}
	srv := inboundTestSetup(t, h)

	// 10 minutes ago — outside the 5-minute window.
	req := buildInboundRequest(token, time.Now().Add(-10*time.Minute), []byte(`{}`), "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestWebhookInbound_MissingSignatureWhenRequired_401(t *testing.T) {
	token := "tok_signed_xxxxxxxxxx"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken:  pgtype.Text{String: token, Valid: true},
		InboundSigningSecret: pgtype.Text{String: "shh-secret", Valid: true},
	}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: &fakeExecutor{enabled: true}}
	srv := inboundTestSetup(t, h)

	// Secret set but request has no signature.
	req := buildInboundRequest(token, time.Now(), []byte(`{}`), "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestWebhookInbound_BadSignature_401(t *testing.T) {
	token := "tok_badsig_xxxxxxxxxx"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken:  pgtype.Text{String: token, Valid: true},
		InboundSigningSecret: pgtype.Text{String: "real-secret", Valid: true},
	}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: &fakeExecutor{enabled: true}}
	srv := inboundTestSetup(t, h)

	// Sign with the wrong secret.
	req := buildInboundRequest(token, time.Now(), []byte(`{}`), "wrong-secret")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestWebhookInbound_GoodSignature_202(t *testing.T) {
	token := "tok_goodsig_xxxxxxxxx"
	secret := "the-right-secret"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken:  pgtype.Text{String: token, Valid: true},
		InboundSigningSecret: pgtype.Text{String: secret, Valid: true},
	}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	exec := &fakeExecutor{enabled: true}
	h := &WebhookInboundHandler{Cerebro: queries, Service: exec}
	srv := inboundTestSetup(t, h)

	body := []byte(`{"hello":"world"}`)
	req := buildInboundRequest(token, time.Now(), body, secret)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202\nbody: %s", w.Code, w.Body.String())
	}
	if !exec.called {
		t.Fatal("Execute must be invoked on a valid signature")
	}
}

func TestWebhookInbound_OversizedBody_413(t *testing.T) {
	token := "tok_oversize_xxxxxxxx"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: &fakeExecutor{enabled: true}}
	srv := inboundTestSetup(t, h)

	big := bytes.Repeat([]byte("a"), maxInboundWebhookBody+10)
	req := buildInboundRequest(token, time.Now(), big, "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", w.Code)
	}
}

func TestWebhookInbound_BodyDecodeFailure_400(t *testing.T) {
	token := "tok_baddecode_xxxxxxx"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: &fakeExecutor{enabled: true}}
	srv := inboundTestSetup(t, h)

	req := buildInboundRequest(token, time.Now(), []byte(`not-json-{`), "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestWebhookInbound_IssueIDResolution_WorkspaceMatch(t *testing.T) {
	token := "tok_iss_match_xxxxxxx"
	wsID := mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	issueIDStr := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: wsID,
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	issues := &fakeIssueLookup{issues: map[string]db.Issue{
		issueIDStr: {ID: mustUUID(issueIDStr), WorkspaceID: wsID},
	}}
	exec := &fakeExecutor{enabled: true}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: exec, IssueQuery: issues}
	srv := inboundTestSetup(t, h)

	body, _ := json.Marshal(map[string]any{"issue_id": issueIDStr})
	req := buildInboundRequest(token, time.Now(), body, "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if exec.gotTE.IssueID != issueIDStr {
		t.Errorf("IssueID = %q, want %q", exec.gotTE.IssueID, issueIDStr)
	}
}

func TestWebhookInbound_IssueIDResolution_CrossWorkspaceDropped(t *testing.T) {
	token := "tok_iss_cross_xxxxxxx"
	wsID := mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	otherWS := mustUUID("a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1")
	issueIDStr := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: wsID,
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	issues := &fakeIssueLookup{issues: map[string]db.Issue{
		issueIDStr: {ID: mustUUID(issueIDStr), WorkspaceID: otherWS}, // wrong ws
	}}
	exec := &fakeExecutor{enabled: true}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: exec, IssueQuery: issues}
	srv := inboundTestSetup(t, h)

	body, _ := json.Marshal(map[string]any{"issue_id": issueIDStr})
	req := buildInboundRequest(token, time.Now(), body, "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if exec.gotTE.IssueID != "" {
		t.Errorf("cross-workspace IssueID must be dropped, got %q", exec.gotTE.IssueID)
	}
}

func TestWebhookInbound_IssueIDResolution_BadUUIDIgnored(t *testing.T) {
	token := "tok_iss_baduid_xxxxxx"
	wsID := mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: wsID,
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	exec := &fakeExecutor{enabled: true}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: exec, IssueQuery: &fakeIssueLookup{}}
	srv := inboundTestSetup(t, h)

	body, _ := json.Marshal(map[string]any{"issue_id": "not-a-uuid"})
	req := buildInboundRequest(token, time.Now(), body, "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if exec.gotTE.IssueID != "" {
		t.Errorf("bad issue_id must be silently dropped, got %q", exec.gotTE.IssueID)
	}
}

func TestComputeInboundSignature_Deterministic(t *testing.T) {
	a := computeInboundSignature("s", "1700000000", []byte(`{"x":1}`))
	b := computeInboundSignature("s", "1700000000", []byte(`{"x":1}`))
	if a != b {
		t.Fatal("signatures must be deterministic")
	}
	if !strings.HasPrefix(a, "sha256=") {
		t.Fatalf("signature missing prefix: %q", a)
	}
	if computeInboundSignature("s", "1700000000", []byte(`{"x":2}`)) == a {
		t.Fatal("different body must produce different signature")
	}
}

func TestBuildInboundEventID_StableAndUnique(t *testing.T) {
	a := buildInboundEventID("tok", "1700000000", []byte(`{"x":1}`))
	b := buildInboundEventID("tok", "1700000000", []byte(`{"x":1}`))
	if a != b {
		t.Fatal("event id must be deterministic for same inputs")
	}
	if buildInboundEventID("tok", "1700000001", []byte(`{"x":1}`)) == a {
		t.Fatal("different timestamp must change event id")
	}
}

func TestReadLimitedBody_OverLimit(t *testing.T) {
	body, tooBig, err := readLimitedBody(strings.NewReader(strings.Repeat("a", 100)), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tooBig {
		t.Fatal("expected tooBig=true for body over the cap")
	}
	if body != nil {
		t.Fatal("expected nil body when over the cap")
	}
}

func TestReadLimitedBody_ExactlyAtLimit(t *testing.T) {
	body, tooBig, err := readLimitedBody(strings.NewReader(strings.Repeat("a", 50)), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tooBig {
		t.Fatal("expected tooBig=false at exactly cap")
	}
	if len(body) != 50 {
		t.Fatalf("body len = %d, want 50", len(body))
	}
}

func TestWebhookInbound_ExecuteFailure_500(t *testing.T) {
	token := "tok_execfail_xxxxxxxx"
	row := cerebrodb.CerebroWorkflow{
		ID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"), WorkspaceID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Enabled: true, TriggerType: TriggerWebhookInbound, ActionType: ActionSetStatus,
		InboundWebhookToken: pgtype.Text{String: token, Valid: true},
	}
	exec := &fakeExecutor{enabled: true, executeErr: errors.New("db down")}
	queries := &fakeWebhookInboundQueries{workflows: map[string]cerebrodb.CerebroWorkflow{token: row}}
	h := &WebhookInboundHandler{Cerebro: queries, Service: exec}
	srv := inboundTestSetup(t, h)

	req := buildInboundRequest(token, time.Now(), []byte(`{}`), "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500\nbody: %s", w.Code, w.Body.String())
	}
}

// Make sure readLimitedBody handles nil body (e.g. GET without body) without
// panicking — defensive even though the route is POST-only.
func TestReadLimitedBody_NilReader(t *testing.T) {
	body, tooBig, err := readLimitedBody(nil, 100)
	if err != nil || tooBig || body != nil {
		t.Fatalf("nil reader must yield (nil,false,nil), got %v %v %v", body, tooBig, err)
	}
}

