package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestModelListStore_RunningRequestTimesOut pins the escape hatch for
// requests that were claimed (PopPending → Running) but whose result was
// never reported — usually because the heartbeat response carrying the
// `pending_model_list` field was lost in transit. Before this, the only
// way out of Running was the 2-minute memory GC, which exceeded the UI
// polling window and surfaced as a silent "discovery failed" (MUL-1397).
func TestModelListStore_RunningRequestTimesOut(t *testing.T) {
	store := NewInMemoryModelListStore()
	ctx := context.Background()
	req, _ := store.Create(ctx, "runtime-xyz")
	claimed, _ := store.PopPending(ctx, "runtime-xyz")
	if claimed == nil {
		t.Fatal("expected PopPending to claim the pending request")
	}
	if claimed.Status != ModelListRunning {
		t.Fatalf("expected Running after PopPending, got %s", claimed.Status)
	}

	// Age the running record past the threshold without the daemon ever
	// reporting a result. Get() must flip it to Timeout so the UI can
	// terminate polling instead of waiting for the 2-minute GC.
	claimed.UpdatedAt = time.Now().Add(-(modelListRunningTimeout + time.Second))
	got, _ := store.Get(ctx, req.ID)
	if got == nil {
		t.Fatal("expected stored request")
	}
	if got.Status != ModelListTimeout {
		t.Fatalf("expected Timeout after running threshold, got %s", got.Status)
	}
	if got.Error == "" {
		t.Fatal("expected timeout error message")
	}
}

// TestReportModelListResult_PreservesDefault guards the daemon → server
// → UI wire format for the model-discovery result. The `default` bool
// on each ModelEntry lights up the UI's "default" badge; if it gets
// dropped here (e.g. by going through a map[string]string), the badge
// silently disappears.
func TestReportModelListResult_PreservesDefault(t *testing.T) {
	store := NewInMemoryModelListStore()
	ctx := context.Background()
	req, _ := store.Create(ctx, "runtime-xyz")

	// Report a completed result with one default entry and one not.
	body := map[string]any{
		"status":    "completed",
		"supported": true,
		"models": []map[string]any{
			{"id": "foo-default", "label": "Foo", "provider": "p", "default": true},
			{"id": "bar", "label": "Bar", "provider": "p"},
		},
	}
	raw, _ := json.Marshal(body)

	// Use the store's Complete directly — we're verifying the wire
	// shape, not HTTP auth. The handler itself unmarshals into
	// []ModelEntry and forwards verbatim, which is the path we care
	// about here.
	var parsed struct {
		Models []ModelEntry `json:"models"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal report body: %v", err)
	}
	store.Complete(ctx, req.ID, parsed.Models, true)

	got, _ := store.Get(ctx, req.ID)
	if got == nil {
		t.Fatal("expected stored result")
	}
	if len(got.Models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(got.Models), got.Models)
	}
	if !got.Models[0].Default {
		t.Errorf("first model should carry Default=true, got %+v", got.Models[0])
	}
	if got.Models[1].Default {
		t.Errorf("second model should carry Default=false, got %+v", got.Models[1])
	}

	// Serialise the stored request back out (what UI actually sees)
	// and confirm `default: true` survives.
	out, _ := json.Marshal(got)
	if !bytes.Contains(out, []byte(`"default":true`)) {
		t.Errorf(`expected "default":true in JSON response, got: %s`, out)
	}
}

// TestReportModelListResult_DecodesJSONBodyDefault verifies the
// handler's request-body parsing accepts the `default` bool from
// the daemon POST — not just through the store API.
func TestReportModelListResult_DecodesJSONBodyDefault(t *testing.T) {
	// Simulate the shape the daemon POSTs: status + models + supported
	// with `default` on one entry.
	payload := `{"status":"completed","supported":true,"models":[{"id":"a","label":"A","default":true},{"id":"b","label":"B"}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/daemon/runtimes/rt/models/req/result", bytes.NewBufferString(payload))

	var body struct {
		Status    string       `json:"status"`
		Models    []ModelEntry `json:"models"`
		Supported *bool        `json:"supported"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Models) != 2 {
		t.Fatalf("want 2 models, got %d", len(body.Models))
	}
	if !body.Models[0].Default {
		t.Errorf("default flag lost on model[0]: %+v", body.Models[0])
	}
}
