package handler

// wecom_install_web_test.go — what the scan dialog can read off the two
// scan-install endpoints.
//
// The dialog is read by a workspace admin in their own language, so everything
// it shows has to arrive as a code it can translate. The begin refusals carry a
// `code`; the status poll carries `error_reason` and nothing else, because the
// detail stored beside the reason is an English, operator-facing string.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// TestWecomScanBeginErrorMatrix pins status + code for every refusal the scan
// dialog explains to the admin, and pins that the rest carry no code — the
// dialog shows its generic line for those, and a code there would be one it
// has no translation for.
func TestWecomScanBeginErrorMatrix(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"agent already has a bot", wecom.ErrActiveInstallationExists, http.StatusConflict, "wecom_agent_already_connected"},
		{"rate limited", wecom.ErrRateLimited, http.StatusTooManyRequests, "wecom_scan_install_rate_limited"},
		{"scan install off", wecom.ErrInstallNotConfigured, http.StatusServiceUnavailable, "wecom_scan_install_unavailable"},
		{"wrapped sentinel", fmt.Errorf("begin: %w", wecom.ErrRateLimited), http.StatusTooManyRequests, "wecom_scan_install_rate_limited"},
		{"key reused for another agent", wecom.ErrAgentMismatch, http.StatusConflict, ""},
		{"missing key", wecom.ErrIdempotencyKeyRequired, http.StatusBadRequest, ""},
		{"key too long", wecom.ErrIdempotencyKeyTooLong, http.StatusBadRequest, ""},
		{"database down", errors.New("dial tcp: connection refused"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeWecomScanBeginError(w, tc.err)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			var body struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v (body %q)", err, w.Body.String())
			}
			if body.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", body.Code, tc.wantCode)
			}
			if body.Error == "" {
				t.Error("no fallback sentence for a client that does not know the code")
			}
		})
	}
}

// The status poll returns the failure as a reason code and never as the stored
// detail, which is English and can hold an internal error string.
func TestWecomInstallStatusOmitsTheOperatorDetail(t *testing.T) {
	ctx := context.Background()
	var sessionID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO wecom_install_session (request_key_hash, workspace_id, agent_id, initiator_user_id, status, error_reason, error_message)
VALUES ('status-detail', $1, gen_random_uuid(), $2, 'error', 'bind_failed', 'Could not finalize the WeCom install: wecom: lock bot routing slot: dial tcp 10.0.0.5:5432')
RETURNING id::text`, testWorkspaceID, testUserID).Scan(&sessionID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM wecom_install_session WHERE id = $1`, sessionID)
	})

	h := *testHandler
	h.WecomInstall = wecom.NewInstallService(testHandler.Queries, testPool, nil, wecom.InstallServiceConfig{})

	req := withURLParams(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/wecom/install/"+sessionID, nil),
		"id", testWorkspaceID, "sessionId", sessionID)
	w := httptest.NewRecorder()
	h.GetWecomInstallStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error_reason"] != "bind_failed" {
		t.Errorf("error_reason = %v, want bind_failed", body["error_reason"])
	}
	if _, ok := body["error_message"]; ok {
		t.Errorf("the poll returned the operator detail to the browser: %s", w.Body.String())
	}
}

// The settings tab offers "create a new bot" only when the listing says scan
// install works. BYO needs just the secret key; scanning also needs the install
// service fully configured, so the two flags can differ.
func TestListWecomInstallationsReportsScanSupportSeparately(t *testing.T) {
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	creds, err := wecom.NewSecretboxCredentialsResolver(box)
	if err != nil {
		t.Fatalf("credentials resolver: %v", err)
	}
	h := *testHandler
	h.WecomStore = wecom.NewStore(testHandler.Queries)
	h.WecomCredentials = creds
	h.ChannelRouter = &engine.Router{}

	list := func() map[string]any {
		t.Helper()
		req := withURLParams(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/wecom/installations", nil),
			"id", testWorkspaceID)
		w := httptest.NewRecorder()
		h.ListWecomInstallations(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body
	}

	if body := list(); body["install_supported"] != true || body["scan_install_supported"] != false {
		t.Fatalf("no scan service: install_supported=%v scan_install_supported=%v, want true/false",
			body["install_supported"], body["scan_install_supported"])
	}

	binder, err := wecom.NewInstallationService(testHandler.Queries, testPool, box)
	if err != nil {
		t.Fatalf("installation service: %v", err)
	}

	// A service that cannot reach WeCom's QR endpoints is no scan support: the
	// option would only lead to a 503.
	h.WecomInstall = wecom.NewInstallService(testHandler.Queries, testPool, binder, wecom.InstallServiceConfig{Box: box})
	if body := list(); body["scan_install_supported"] != false {
		t.Fatalf("scan service without a provider: scan_install_supported=%v, want false", body["scan_install_supported"])
	}

	provider, err := wecom.NewHTTPProvider(wecom.HTTPProviderConfig{SourceID: wecom.DefaultSourceID})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	h.WecomInstall = wecom.NewInstallService(testHandler.Queries, testPool, binder, wecom.InstallServiceConfig{Provider: provider, Box: box})
	if body := list(); body["scan_install_supported"] != true {
		t.Fatalf("configured scan service: scan_install_supported=%v, want true", body["scan_install_supported"])
	}
}
