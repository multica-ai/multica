package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestCompleteTaskWireShapeDualWriteAgrees pins the producer-side guarantee the
// server's conflict check backstops: result.summary and the legacy output field
// are built from one string and can never disagree.
func TestCompleteTaskWireShapeDualWriteAgrees(t *testing.T) {
	for _, answer := range []string{
		"the agent's answer",
		"", // a tool-only turn is a legal completion with no prose
		"multi\nline\nanswer",
		"unicode ✓ and emoji 🎉",
	} {
		t.Run(strings.ReplaceAll(answer, "\n", "_"), func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Errorf("request body is not JSON: %v", err)
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			c := NewClient(srv.URL)
			if err := c.CompleteTask(t.Context(), "task-1", answer, "", "", "", false, "", ""); err != nil {
				t.Fatalf("CompleteTask: %v", err)
			}

			resultObj, ok := got["result"].(map[string]any)
			if !ok {
				t.Fatalf("body has no nested result object: %v", got)
			}
			if v := resultObj["version"]; v != float64(protocol.CompletionResultVersion1) {
				t.Errorf("result.version = %v, want %d", v, protocol.CompletionResultVersion1)
			}
			// The load-bearing assertion: one answer, two fields, always equal.
			if resultObj["summary"] != got["output"] {
				t.Errorf("dual write diverged: result.summary = %v, output = %v", resultObj["summary"], got["output"])
			}
			if resultObj["summary"] != answer {
				t.Errorf("result.summary = %v, want %q", resultObj["summary"], answer)
			}
			// summary must be PRESENT even when empty — the server rejects a v1
			// payload that omits it, so omitempty here would break every
			// tool-only turn.
			if _, present := resultObj["summary"]; !present {
				t.Error("result.summary key is absent; the server requires it even when empty")
			}
			if _, present := resultObj["artifact_ids"]; !present {
				t.Error("result.artifact_ids key is absent; want an empty array")
			}
		})
	}
}

// TestCompleteTaskWireShapeKeepsTransportOutsideResult guards the other half of
// the contract: transport fields stay top-level and must not leak into the
// envelope, which is what previously made result a second source of truth.
func TestCompleteTaskWireShapeKeepsTransportOutsideResult(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL)
	err := c.CompleteTask(t.Context(), "task-1", "answer", "feat/x", "ses_1", "/Users/a/p", true, "ses_old", "/Users/a/durable")
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	resultObj := got["result"].(map[string]any)
	for _, leaked := range []string{"session_id", "work_dir", "durable_work_dir", "branch_name", "retired_session_id"} {
		if _, present := resultObj[leaked]; present {
			t.Errorf("transport field %q leaked into the v1 envelope", leaked)
		}
	}
	for _, field := range []string{"session_id", "work_dir", "durable_work_dir", "branch_name", "retired_session_id"} {
		if _, present := got[field]; !present {
			t.Errorf("transport field %q missing from the top level", field)
		}
	}
	if got["session_rollout_missing"] != true {
		t.Errorf("session_rollout_missing = %v, want true", got["session_rollout_missing"])
	}
	// pr_url is gone from the active protocol entirely.
	if _, present := got["pr_url"]; present {
		t.Error("pr_url is still on the wire")
	}
}
