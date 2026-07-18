package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// CEREBRO-PATCH(daemon-model-usage-event-transport-test): FIR-3337 keeps the
// legacy aggregate and canonical event in one idempotent report request.
func TestClientReportTaskUsageSendsModelUsageEvents(t *testing.T) {
	var received struct {
		Usage  []TaskUsageEntry       `json:"usage"`
		Events []ModelUsageEventEntry `json:"events"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL)
	err := client.ReportTaskUsage(context.Background(), "task-1",
		[]TaskUsageEntry{{Provider: "openai", Model: "gpt-5.6-sol", InputTokens: 99_000}},
		[]ModelUsageEventEntry{{SchemaVersion: "1", EventID: "event-1"}},
	)
	if err != nil {
		t.Fatalf("ReportTaskUsage: %v", err)
	}
	if len(received.Usage) != 1 || len(received.Events) != 1 || received.Events[0].EventID != "event-1" {
		t.Fatalf("received = %+v", received)
	}
}
