package claudemanagedruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUnresolvedSurvivesRejectedLookup(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			h := setup(t)
			h.lostCreates, h.lostCreatePersists = 1, true
			if err := h.b.step(context.Background()); err == nil {
				t.Fatal("expected ambiguous create")
			}
			h.status = "cancelled"
			h.listStatus = status
			if err := h.b.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(h.b.state.Unresolved) != 1 {
				t.Fatal("expected cleanup to queue unresolved creation")
			}
			err := h.b.cleanup(context.Background())
			if err == nil {
				t.Fatal("lookup error was not surfaced")
			}
			unauthorized := status == http.StatusUnauthorized || status == http.StatusForbidden
			if errors.Is(err, ErrCleanupUnauthorized) != unauthorized {
				t.Fatalf("unexpected cleanup error: %v", err)
			}
			h.restart(t)
			if len(h.b.state.Unresolved) != 1 {
				t.Fatal("rejected lookup erased the persisted cleanup entry")
			}
			h.listStatus = 0
			if err := h.b.cleanup(context.Background()); err != nil {
				t.Fatal(err)
			}
			if h.remoteStatus != "idle" || h.cancels != 1 || h.creates != 1 || len(h.b.state.Unresolved) != 0 || len(h.b.state.Cleanup) != 0 {
				t.Fatalf("cleanup did not recover: status=%s creates=%d cancels=%d unresolved=%d cleanup=%d", h.remoteStatus, h.creates, h.cancels, len(h.b.state.Unresolved), len(h.b.state.Cleanup))
			}
		})
	}
}

func TestRunReportsCleanupAuthorizationFailure(t *testing.T) {
	for _, queue := range []string{"unresolved", "cleanup"} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
			t.Run(fmt.Sprintf("%s/%d", queue, status), func(t *testing.T) {
				multica := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/api/daemon/register" {
						t.Errorf("work accepted while remote cleanup is unauthorized: %s", r.URL.Path)
						http.NotFound(w, r)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"runtimes": []map[string]string{{"id": "runtime-1", "provider": Provider}}})
				}))
				t.Cleanup(multica.Close)
				anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(status)
				}))
				t.Cleanup(anthropic.Close)
				state := journal{Binding: Binding("", workspaceID, anthropic.URL, "env_1")}
				if queue == "unresolved" {
					state.Unresolved = []unresolvedCreate{{TaskID: "task-1", AgentID: "agent_1", CreateAttemptAt: time.Now()}}
				} else {
					state.Cleanup = []string{"sess_1"}
				}
				raw, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				bridge, err := New(Config{
					MulticaURL: multica.URL, MulticaToken: "multica-secret", WorkspaceID: workspaceID,
					AnthropicURL: anthropic.URL, AnthropicToken: "revoked-key", EnvironmentID: "env_1",
					LoadState: func(context.Context) ([]byte, error) { return raw, nil },
					SaveState: func(_ context.Context, data []byte) error { raw = data; return nil },
				}, nil)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := bridge.Run(ctx); !errors.Is(err, ErrCleanupUnauthorized) {
					t.Fatalf("credential error was not reported to the supervisor: %v", err)
				}
				restored, err := decodeJournal(raw, state.Binding)
				if err != nil || len(restored.Unresolved)+len(restored.Cleanup) != 1 {
					t.Fatalf("cleanup journal was not retained: err=%v", err)
				}
			})
		}
	}
}
