package composio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/multica-ai/multica/server/pkg/composio"
)

func TestExecuteTool_ConnectedAccountIdentityOnWire(t *testing.T) {
	for _, userID := range []string{"personal_user", "ws_team"} {
		for _, version := range []string{"", "20261027_00"} {
			t.Run(userID+"/"+version, func(t *testing.T) {
				args := map[string]any{"user_id": "me", "max_results": float64(5)}
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Method != http.MethodPost || r.URL.Path != "/tools/execute/GMAIL_FETCH_EMAILS" {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					}
					var wire struct {
						UserID             string         `json:"user_id"`
						ConnectedAccountID string         `json:"connected_account_id"`
						Version            string         `json:"version"`
						Arguments          map[string]any `json:"arguments"`
					}
					if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
						t.Error(err)
					}
					if wire.UserID != userID || wire.ConnectedAccountID != "ca_selected" {
						t.Errorf("missing or mismatched identity: %+v", wire)
						http.Error(w, "User ID is required with connected account", http.StatusBadRequest)
						return
					}
					wantVersion := version
					if wantVersion == "" {
						wantVersion = "latest"
					}
					if wire.Version != wantVersion || !reflect.DeepEqual(wire.Arguments, args) {
						t.Errorf("tool inputs changed: %+v", wire)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"successful":true,"data":{"messages":[]}}`))
				}))
				defer srv.Close()
				client, err := sdk.NewClient(sdk.Options{APIKey: "test-key", BaseURL: srv.URL, Timeout: time.Second})
				if err != nil {
					t.Fatal(err)
				}
				svc := newTestService(t, client, newFakeStore())
				resp, err := svc.ExecuteTool(context.Background(), "ca_selected", userID, sdk.Tool{Slug: "GMAIL_FETCH_EMAILS", Version: version}, args)
				if err != nil || !resp.Successful || calls.Load() != 1 {
					t.Fatalf("execution = %+v, %v; upstream calls = %d", resp, err, calls.Load())
				}
			})
		}
	}
}

func TestExecuteTool_RequiresConnectionIdentity(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer srv.Close()
	client, err := sdk.NewClient(sdk.Options{APIKey: "test-key", BaseURL: srv.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, client, newFakeStore())
	for _, pair := range [][2]string{{"ca_selected", ""}, {"ca_selected", " \t"}, {"", "stored_user"}, {" \t", "stored_user"}} {
		resp, err := svc.ExecuteTool(context.Background(), pair[0], pair[1], sdk.Tool{Slug: "GMAIL_FETCH_EMAILS"}, map[string]any{"user_id": "me"})
		if err == nil || resp != nil {
			t.Fatalf("incomplete identity %q accepted: %+v, %v", pair, resp, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("incomplete identity reached upstream %d times", calls.Load())
	}
}
