//go:build verificationdev

package localhost

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func verificationEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MULTICA_VERIFICATION_MODE", "1")
	t.Setenv("MULTICA_VERIFICATION_TOKEN", strings.Repeat("a", 64))
}

func TestVerificationImportFreezesHistoryRemapsAttachmentsAndReplays(t *testing.T) {
	verificationEnv(t)
	server, svc := newV2Server(t)
	headers := v2Headers("user-1")
	status, _ := v2Do(t, server, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "original", "provider": "claude", "title": "Real content"}, headers)
	if status != http.StatusCreated {
		t.Fatal(status)
	}
	scope, err := svc.Store().EnsureScope(context.Background(), store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := svc.Upload(context.Background(), scope, "", "guide.pdf", "application/pdf", strings.NewReader("%PDF-verification"))
	if err != nil {
		t.Fatal(err)
	}
	status, _ = v2Do(t, server, http.MethodPost, "/local/v2/sessions/original/commands", map[string]any{"type": "send", "request_id": "send-one", "text": "Original message", "attachment_ids": []string{file.ID}}, headers)
	if status != http.StatusOK && status != http.StatusAccepted {
		t.Fatal(status)
	}
	_, snapshot := v2Do(t, server, http.MethodGet, "/local/v2/sessions/original/snapshot", nil, headers)
	sum := sha256.Sum256([]byte("%PDF-verification"))
	body := map[string]any{"schemaVersion": 1, "restoreId": "restore-test-01", "conversations": []any{map[string]any{"originalId": "original", "snapshot": snapshot, "attachments": []any{map[string]any{"originalId": file.ID, "name": "guide.pdf", "mediaType": "application/pdf", "data": base64.StdEncoding.EncodeToString([]byte("%PDF-verification")), "sha256": hex.EncodeToString(sum[:])}}}}}
	status, result := v2Do(t, server, http.MethodPost, "/local/v2/verification/import", body, headers)
	if status != http.StatusOK {
		t.Fatalf("import: %d %v", status, result)
	}
	mapping := result["mapping"].(map[string]any)
	copied := mapping["original"].(string)
	if copied == "original" || mapping[file.ID] == file.ID {
		t.Fatalf("ids not remapped: %v", mapping)
	}
	_, copy := v2Do(t, server, http.MethodGet, "/local/v2/sessions/"+copied+"/snapshot", nil, headers)
	if copy["queue"].(map[string]any)["paused"] != true {
		t.Fatalf("copy queue: %v", copy)
	}
	turns := copy["turns"].([]any)
	if len(turns) != 1 || turns[0].(map[string]any)["status"] != "cancelled" {
		t.Fatalf("copy turns: %v", turns)
	}
	message := turns[0].(map[string]any)["messages"].([]any)[0].(map[string]any)
	if message["content"] != "Original message" || message["attachment_ids"].([]any)[0] != mapping[file.ID] {
		t.Fatalf("copy message: %v", message)
	}
	status, again := v2Do(t, server, http.MethodPost, "/local/v2/verification/import", body, headers)
	if status != http.StatusOK || again["mapping"].(map[string]any)["original"] != copied {
		t.Fatalf("retry: %d %v", status, again)
	}
	_, original := v2Do(t, server, http.MethodGet, "/local/v2/sessions/original/snapshot", nil, headers)
	if original["turns"].([]any)[0].(map[string]any)["status"] != "running" {
		t.Fatal("import changed original execution")
	}
	other := headers.Clone()
	other.Set(headerUserID, "other-user")
	status, _ = v2Do(t, server, http.MethodPost, "/local/v2/verification/import", body, other)
	if status != http.StatusForbidden {
		t.Fatal("foreign account admitted", status)
	}
}

func TestVerificationFakeProviderDrivesTheNormalQueue(t *testing.T) {
	verificationEnv(t)
	server, _ := newV2Server(t)
	headers := v2Headers("user-1")
	v2Do(t, server, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "queue-test", "provider": "claude"}, headers)
	v2Do(t, server, http.MethodPost, "/local/v2/sessions/queue-test/commands", map[string]any{"type": "send", "request_id": "request-a", "text": "A"}, headers)
	status, result := v2Do(t, server, http.MethodPost, "/local/v2/sessions/queue-test/commands", map[string]any{"type": "queue_enqueue", "item": map[string]any{"requestId": "request-b", "content": "B", "display": map[string]any{"id": "request-b"}}}, headers)
	if status != http.StatusOK {
		t.Fatalf("enqueue: %d %v", status, result)
	}
	_, stats := v2Do(t, server, http.MethodGet, "/local/v2/verification/fixture?conversationId=queue-test", nil, headers)
	if len(stats["received"].([]any)) != 1 {
		t.Fatal(stats)
	}
	status, result = v2Do(t, server, http.MethodPost, "/local/v2/verification/fixture", map[string]any{"conversationId": "queue-test", "action": "release", "text": "Answer A", "expectedRequestId": "request-a"}, headers)
	if status != http.StatusOK {
		t.Fatalf("release: %d %v", status, result)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, stats = v2Do(t, server, http.MethodGet, "/local/v2/verification/fixture?conversationId=queue-test", nil, headers)
		if len(stats["received"].([]any)) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := stats["received"].([]any); len(got) != 2 || got[1] != "request-b" {
		t.Fatalf("queue dispatch: %v", stats)
	}
	v2Do(t, server, http.MethodPost, "/local/v2/verification/fixture", map[string]any{"conversationId": "queue-test", "action": "release", "text": "Answer B", "expectedRequestId": "request-b"}, headers)
	_, snapshot := v2Do(t, server, http.MethodGet, "/local/v2/sessions/queue-test/snapshot", nil, headers)
	status, events := v2Do(t, server, http.MethodGet, "/local/v2/verification/events?conversationId=queue-test&after=0", nil, headers)
	if status != http.StatusOK || len(events["events"].([]any)) == 0 || events["generation"] != snapshot["store_generation"] {
		t.Fatalf("events: %d %v", status, events)
	}
	status, beyond := v2Do(t, server, http.MethodGet, "/local/v2/verification/events?conversationId=queue-test&after=1000000", nil, headers)
	if status != http.StatusOK || len(beyond["events"].([]any)) != 0 || beyond["highWater"] != snapshot["snapshot_seq"] {
		t.Fatalf("events beyond high water: %d %v", status, beyond)
	}
	renderer := headers.Clone()
	renderer.Set("Authorization", "Bearer "+RendererToken(v2Token))
	renderer.Set("Origin", "http://localhost:5173")
	status, _ = v2Do(t, server, http.MethodGet, "/local/v2/verification/capabilities", nil, renderer)
	if status != http.StatusNotFound {
		t.Fatal("renderer reached observer route", status)
	}
}

func TestVerificationStopThenSubmitResumesQueue(t *testing.T) {
	verificationEnv(t)
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			server, _ := newV2Server(t)
			headers := v2Headers("user-1")
			base := "/local/v2/sessions/stop-resume"
			status, body := v2Do(t, server, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "stop-resume", "provider": "claude"}, headers)
			if status != http.StatusCreated {
				t.Fatalf("create: %d %v", status, body)
			}
			item := func(id string) map[string]any {
				return map[string]any{"requestId": id, "content": id, "display": map[string]any{"id": id}}
			}
			command := func(body map[string]any) map[string]any {
				t.Helper()
				status, result := v2Do(t, server, http.MethodPost, base+"/commands", body, headers)
				if status != http.StatusOK && status != http.StatusAccepted {
					t.Fatalf("command: %d %v", status, result)
				}
				return result
			}
			waitReceived := func(want []any) {
				t.Helper()
				eventuallyV2(t, func() bool {
					_, stats := v2Do(t, server, http.MethodGet, "/local/v2/verification/fixture?conversationId=stop-resume", nil, headers)
					return reflect.DeepEqual(stats["received"], want) && stats["pending"] == true
				})
			}
			release := func(id string) {
				t.Helper()
				status, result := v2Do(t, server, http.MethodPost, "/local/v2/verification/fixture", map[string]any{"conversationId": "stop-resume", "action": "release", "text": "done " + id, "expectedRequestId": id}, headers)
				if status != http.StatusOK {
					t.Fatalf("release %s: %d %v", id, status, result)
				}
			}
			command(map[string]any{"type": "submit", "item": item("first")})
			waitReceived([]any{"first"})
			if pending {
				command(map[string]any{"type": "queue_enqueue", "item": item("old"), "paused": true})
			}
			command(map[string]any{"type": "queue_update", "action": "stop", "operation_id": "stop-first"})
			result := command(map[string]any{"type": "submit", "item": item("new")})
			if result["queue"].(map[string]any)["paused"] != false {
				t.Fatalf("submit did not resume: %v", result)
			}
			want := []any{"first", "new"}
			if pending {
				want = []any{"first", "old"}
			}
			waitReceived(want)
			_, snapshot := v2Do(t, server, http.MethodGet, base+"/snapshot", nil, headers)
			turns := snapshot["turns"].([]any)
			if len(turns) != 2 || turns[0].(map[string]any)["status"] != "cancelled" || turns[1].(map[string]any)["status"] != "running" {
				t.Fatalf("stopped turn did not hand off execution: %v", snapshot)
			}
			if pending {
				release("old")
				want = []any{"first", "old", "new"}
				waitReceived(want)
			}
			release("new")
			eventuallyV2(t, func() bool {
				_, snapshot = v2Do(t, server, http.MethodGet, base+"/snapshot", nil, headers)
				turns := snapshot["turns"].([]any)
				return len(turns) == len(want) && turns[len(turns)-1].(map[string]any)["status"] == "completed" && len(snapshot["submissions"].([]any)) == 0
			})
		})
	}
}

func TestVerificationDevRoutesRequireExplicitLaunch(t *testing.T) {
	t.Setenv("MULTICA_VERIFICATION_MODE", "")
	server, _ := newV2Server(t)
	status, _ := v2Do(t, server, http.MethodGet, "/local/v2/verification/capabilities", nil, v2Headers("user-1"))
	if status != http.StatusNotFound {
		t.Fatal(status)
	}
}

func TestVerificationImportKeepsSubmissionsThatNeedReview(t *testing.T) {
	verificationEnv(t)
	server, _ := newV2Server(t)
	headers := v2Headers("user-1")
	item := func(id, text string) map[string]any {
		return map[string]any{"requestId": id, "content": text, "display": map[string]any{"id": id}}
	}
	body := map[string]any{"schemaVersion": 1, "restoreId": "restore-submissions-01", "conversations": []any{map[string]any{
		"originalId": "source",
		"snapshot": map[string]any{
			"conversation": map[string]any{"conversation_id": "source", "provider": "claude", "title": "Submissions"},
			"turns":        []any{},
			// The Local API queue view also lists items that need review.
			"queue": map[string]any{"items": []any{item("queued-a", "A"), item("review-b", "B")}},
			"submissions": []any{
				map[string]any{"request_id": "queued-a", "state": "queued", "item": item("queued-a", "A"), "blocked_reason": "Paused by restart"},
				map[string]any{"request_id": "review-b", "state": "recovery_required", "item": item("review-b", "B"), "blocked_reason": "Delivery unknown"},
				map[string]any{"request_id": "sending-c", "state": "starting", "item": item("sending-c", "C")},
			},
		},
		"attachments": []any{},
	}}}
	status, result := v2Do(t, server, http.MethodPost, "/local/v2/verification/import", body, headers)
	if status != http.StatusOK {
		t.Fatalf("import: %d %v", status, result)
	}
	mapping := result["mapping"].(map[string]any)
	_, copy := v2Do(t, server, http.MethodGet, "/local/v2/sessions/"+mapping["source"].(string)+"/snapshot", nil, headers)
	states := map[string]string{}
	reasons := map[string]string{}
	for _, raw := range copy["submissions"].([]any) {
		submission := raw.(map[string]any)
		id := submission["request_id"].(string)
		states[id], _ = submission["state"].(string)
		reasons[id], _ = submission["blocked_reason"].(string)
	}
	queued, review, sending := mapping["queued-a"].(string), mapping["review-b"].(string), mapping["sending-c"].(string)
	if states[queued] != "queued" || reasons[queued] != "Paused by restart" {
		t.Fatalf("queued submission: %v %v", states, reasons)
	}
	if states[review] != "recovery_required" || reasons[review] != "Delivery unknown" {
		t.Fatalf("review submission: %v %v", states, reasons)
	}
	if states[sending] != "recovery_required" || reasons[sending] == "" {
		t.Fatalf("in-flight submission: %v %v", states, reasons)
	}
	ids := []string{}
	for _, raw := range copy["queue"].(map[string]any)["items"].([]any) {
		ids = append(ids, raw.(map[string]any)["requestId"].(string))
	}
	if len(ids) != 3 || ids[0] != queued {
		t.Fatalf("queue view: %v", ids)
	}
}
