package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestGitHubAdmissionRequiresSecret(t *testing.T) {
	if got := verifyWebhookSignatureForProvider("github", "", http.Header{}, []byte(`{}`)); got != sigStatusInvalid {
		t.Fatalf("unconfigured GitHub trigger must fail closed, got %s", got)
	}
}

func githubPatch(t *testing.T, h *Handler, ap, trigger string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := testutil.WithURLParams(newRequest("PATCH", "/", body), "id", ap, "triggerId", trigger)
	w := httptest.NewRecorder()
	h.UpdateAutopilotTrigger(w, req)
	return w
}

func TestGitHubSecretLifecycle(t *testing.T) {
	fx := newActingFixture(t, "github lifecycle")
	trig := createWebhookTriggerViaHandler(t, fx.autopilotID)
	// Provider changes cannot silently downgrade a live endpoint.
	if w := githubPatch(t, testHandler, fx.autopilotID, trig.ID, map[string]any{"provider": "github", "signing_secret": testSigningSecret}); w.Code != 400 {
		t.Fatalf("live provider change: %d", w.Code)
	}
	testutil.Call(t, testHandler.UpdateAutopilotTrigger, testutil.WithURLParams(newRequest("PATCH", "/", map[string]any{"enabled": false}), "id", fx.autopilotID, "triggerId", trig.ID)).Want(200)
	if w := githubPatch(t, testHandler, fx.autopilotID, trig.ID, map[string]any{"provider": "github", "signing_secret": testSigningSecret}); w.Code != 200 {
		t.Fatalf("provider change: %d %s", w.Code, w.Body)
	}
	if w := githubPatch(t, testHandler, fx.autopilotID, trig.ID, map[string]any{"enabled": true}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := githubPatch(t, testHandler, fx.autopilotID, trig.ID, map[string]any{"clear_signing_secret": true}); w.Code != 400 {
		t.Fatal("enabled GitHub secret cleared")
	}
	rotated := " new-key-with-exact-whitespace "
	if w := githubPatch(t, testHandler, fx.autopilotID, trig.ID, map[string]any{"signing_secret": rotated}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	body := []byte(`{"a":1}`)
	if w := postWebhook(t, *trig.WebhookToken, body, map[string]string{"X-Hub-Signature-256": signBody(testSigningSecret, body)}); w.Code != 401 {
		t.Fatal("old secret accepted after rotation")
	}
	if w := postWebhook(t, *trig.WebhookToken, body, map[string]string{"X-Hub-Signature-256": signBody(rotated, body)}); w.Code != 200 {
		t.Fatal("new exact secret not accepted")
	}
	if w := githubPatch(t, testHandler, fx.autopilotID, trig.ID, map[string]any{"enabled": false, "clear_signing_secret": true}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := githubPatch(t, testHandler, fx.autopilotID, trig.ID, map[string]any{"enabled": true}); w.Code != 400 {
		t.Fatal("GitHub enabled without secret")
	}
	// Missing and null values on the old PUT must not silently erase a key.
	for _, body := range []map[string]any{{}, {"signing_secret": nil}} {
		req := testutil.WithURLParams(newRequest("PUT", "/", body), "id", fx.autopilotID, "triggerId", trig.ID)
		testutil.Call(t, testHandler.SetAutopilotTriggerSigningSecret, req).Want(400)
	}
}

type githubFailCommitStarter struct{ before func() }
type githubFailCommitTx struct {
	pgx.Tx
	before func()
}

func (s githubFailCommitStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := testPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return githubFailCommitTx{tx, s.before}, nil
}
func (tx githubFailCommitTx) Commit(ctx context.Context) error {
	if tx.before != nil {
		tx.before()
	}
	return errors.New("injected commit failure")
}

func TestGitHubConfigRollbackAndUncommittedCreateInvisible(t *testing.T) {
	fx := newActingFixture(t, "github rollback")
	h := *testHandler
	h.TxStarter = githubFailCommitStarter{before: func() {
		var n int
		if err := testPool.QueryRow(context.Background(), "SELECT count(*) FROM autopilot_trigger WHERE autopilot_id=$1 AND provider='github'", fx.autopilotID).Scan(&n); err != nil {
			t.Error(err)
		}
		if n != 0 {
			t.Error("uncommitted GitHub trigger became visible")
		}
	}}
	req := withURLParam(newRequest("POST", "/", map[string]any{"kind": "webhook", "provider": "github", "enabled": false, "signing_secret": testSigningSecret}), "id", fx.autopilotID)
	testutil.Call(t, h.CreateAutopilotTrigger, req).Want(500)
	var n int
	dbfx.QueryRow(t, "SELECT count(*) FROM autopilot_trigger WHERE autopilot_id=$1 AND provider='github'", fx.autopilotID).Scan(&n)
	if n != 0 {
		t.Fatal("failed create left a trigger")
	}
	trig := createWebhookTriggerViaHandler(t, fx.autopilotID)
	setSigningSecretViaHandler(t, fx.autopilotID, trig.ID, testSigningSecret)
	setTriggerProvider(t, trig.ID, "github")
	h.TxStarter = githubFailCommitStarter{}
	w := githubPatch(t, &h, fx.autopilotID, trig.ID, map[string]any{"enabled": false, "signing_secret": "replacement-signing-secret"})
	if w.Code != 500 {
		t.Fatalf("injected failure: %d", w.Code)
	}
	var secret string
	var enabled bool
	dbfx.QueryRow(t, "SELECT signing_secret,enabled FROM autopilot_trigger WHERE id=$1", trig.ID).Scan(&secret, &enabled)
	if secret != testSigningSecret || !enabled {
		t.Fatal("failed rotation changed prior config")
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("error leaked secret")
	}
}

func TestGitHubConcurrentEnableAndClear(t *testing.T) {
	fx := newActingFixture(t, "github concurrent")
	trig := createWebhookTriggerViaHandler(t, fx.autopilotID)
	setSigningSecretViaHandler(t, fx.autopilotID, trig.ID, testSigningSecret)
	setTriggerProvider(t, trig.ID, "github")
	if w := githubPatch(t, testHandler, fx.autopilotID, trig.ID, map[string]any{"enabled": false}); w.Code != 200 {
		t.Fatal(w.Body)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make(chan int, 2)
	for _, body := range []map[string]any{{"enabled": true}, {"clear_signing_secret": true}} {
		wg.Add(1)
		go func(body map[string]any) {
			defer wg.Done()
			<-start
			codes <- githubPatch(t, testHandler, fx.autopilotID, trig.ID, body).Code
		}(body)
	}
	close(start)
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[400] != 1 {
		t.Fatalf("concurrent results: %v", counts)
	}
	var unsafe bool
	dbfx.QueryRow(t, "SELECT enabled AND COALESCE(signing_secret,'')='' FROM autopilot_trigger WHERE id=$1", trig.ID).Scan(&unsafe)
	if unsafe {
		t.Fatal("enabled GitHub trigger lost its key")
	}
}

func TestGitHubReadbackAndNoPermission(t *testing.T) {
	fx := newActingFixture(t, "github readback")
	trig := createWebhookTriggerViaHandler(t, fx.autopilotID)
	setSigningSecretViaHandler(t, fx.autopilotID, trig.ID, testSigningSecret)
	setTriggerProvider(t, trig.ID, "github")
	req := withURLParam(newRequest("GET", "/", nil), "id", fx.autopilotID)
	w := httptest.NewRecorder()
	testHandler.GetAutopilot(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if strings.Contains(w.Body.String(), testSigningSecret) || strings.Contains(w.Body.String(), *trig.WebhookToken) {
		t.Fatal("ordinary readback leaked credential")
	}
	var response struct {
		Triggers []AutopilotTriggerResponse `json:"triggers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range response.Triggers {
		if r.ID == trig.ID {
			found = r.Provider != nil && *r.Provider == "github" && r.HasSigningSecret && r.SigningSecretHint == nil
		}
	}
	if !found {
		t.Fatal("missing admission readback")
	}
	outsider := plainMember(t, "github-outsider")
	req = testutil.WithURLParams(newRequestAs(outsider, "PATCH", "/", map[string]any{"signing_secret": "unauthorized-replacement"}), "id", fx.autopilotID, "triggerId", trig.ID)
	testutil.Call(t, testHandler.UpdateAutopilotTrigger, req).Want(403)
}

func TestGitHubAdmissionRejectsBeforePersistence(t *testing.T) {
	fx := newActingFixture(t, "github admission")
	trig := createWebhookTriggerViaHandler(t, fx.autopilotID)
	setSigningSecretViaHandler(t, fx.autopilotID, trig.ID, testSigningSecret)
	setTriggerProvider(t, trig.ID, "github")
	original := []byte(`{"a":1,"b":2}`)
	counts := func() [2]int64 {
		var result [2]int64
		dbfx.QueryRow(t, "SELECT count(*) FROM issue WHERE workspace_id=$1", testWorkspaceID).Scan(&result[0])
		dbfx.QueryRow(t, "SELECT count(*) FROM agent_task_queue WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id=$1)", testWorkspaceID).Scan(&result[1])
		return result
	}
	before := counts()
	for _, tc := range []struct{ name, body, sig string }{
		{"missing", string(original), ""},
		{"malformed", string(original), "sha256=nope"},
		{"wrong-key", string(original), signBody("different-secret", original)},
		{"wrong-algorithm", string(original), "sha1=abcd"},
		{"whitespace", `{ "a":1,"b":2}`, signBody(testSigningSecret, original)},
		{"order", `{"b":2,"a":1}`, signBody(testSigningSecret, original)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := postWebhook(t, *trig.WebhookToken, []byte(tc.body), map[string]string{"X-Hub-Signature-256": tc.sig, "X-GitHub-Delivery": "same-id"})
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d", w.Code)
			}
			for _, query := range []string{
				"SELECT count(*) FROM webhook_delivery WHERE autopilot_id=$1",
				"SELECT count(*) FROM autopilot_run WHERE autopilot_id=$1",
			} {
				var count int
				if err := testPool.QueryRow(context.Background(), query, fx.autopilotID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("unauthenticated request persisted %d rows", count)
				}
			}
			if counts() != before {
				t.Fatal("unauthenticated request created an issue or task")
			}
		})
	}
	// A rejected delivery ID must not poison a later authentic request.
	w := postWebhook(t, *trig.WebhookToken, original, map[string]string{"X-Hub-Signature-256": signBody(testSigningSecret, original), "X-GitHub-Delivery": "same-id"})
	if w.Code != http.StatusOK {
		t.Fatalf("valid request: %d %s", w.Code, w.Body.String())
	}
	var first, retry map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	w = postWebhook(t, *trig.WebhookToken, original, map[string]string{"X-Hub-Signature-256": signBody(testSigningSecret, original), "X-GitHub-Delivery": "same-id"})
	if err := json.Unmarshal(w.Body.Bytes(), &retry); err != nil {
		t.Fatal(err)
	}
	if first["run_id"] == nil || first["run_id"] != retry["run_id"] {
		t.Fatal("authentic retry did not reuse the run")
	}
	for _, table := range []string{"autopilot_run", "webhook_delivery"} {
		var n int
		dbfx.QueryRow(t, "SELECT count(*) FROM "+table+" WHERE autopilot_id=$1", fx.autopilotID).Scan(&n)
		if n != 1 {
			t.Fatalf("%s count=%d, want 1", table, n)
		}
	}
	w = postWebhook(t, *trig.WebhookToken, original, map[string]string{"X-GitHub-Delivery": "same-id"})
	if w.Code != 401 {
		t.Fatal("forged duplicate bypassed authentication")
	}
	var attempts int
	dbfx.QueryRow(t, "SELECT attempt_count FROM webhook_delivery WHERE autopilot_id=$1", fx.autopilotID).Scan(&attempts)
	if attempts != 2 {
		t.Fatal("forged duplicate mutated attempt count")
	}
}

func TestGitHubTriggerAtomicDisabledCreate(t *testing.T) {
	fx := newActingFixture(t, "github disabled")
	req := withURLParam(newRequest("POST", "/", map[string]any{"kind": "webhook", "provider": "github", "enabled": false, "signing_secret": testSigningSecret}), "id", fx.autopilotID)
	var out AutopilotTriggerResponse
	testutil.Call(t, testHandler.CreateAutopilotTrigger, req).Want(http.StatusCreated).JSON(&out)
	if out.Enabled || out.Provider == nil || *out.Provider != "github" || !out.HasSigningSecret {
		t.Fatal("atomic configuration was not preserved")
	}
	var enabled bool
	var secret string
	if err := testPool.QueryRow(context.Background(), "SELECT enabled, signing_secret FROM autopilot_trigger WHERE id=$1", out.ID).Scan(&enabled, &secret); err != nil {
		t.Fatal(err)
	}
	if enabled || secret != testSigningSecret {
		t.Fatal("persisted configuration differs")
	}
	var token string
	dbfx.QueryRow(t, "SELECT webhook_token FROM autopilot_trigger WHERE id=$1", out.ID).Scan(&token)
	body := []byte(`{}`)
	w := postWebhook(t, token, body, map[string]string{"X-Hub-Signature-256": signBody(testSigningSecret, body)})
	if w.Code != 200 {
		t.Fatalf("disabled response: %d", w.Code)
	}
	var n int
	dbfx.QueryRow(t, "SELECT count(*) FROM webhook_delivery WHERE trigger_id=$1", out.ID).Scan(&n)
	if n != 0 {
		t.Fatal("disabled GitHub endpoint accepted a delivery")
	}
}

func TestGitHubTriggerRejectsMissingSecret(t *testing.T) {
	fx := newActingFixture(t, "github no secret")
	req := withURLParam(newRequest("POST", "/", map[string]any{"kind": "webhook", "provider": "github"}), "id", fx.autopilotID)
	w := httptest.NewRecorder()
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}
