package apps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSubmitWorkflowRunUsesThePrivateWorkerContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/run-id/execute" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer worker-secret" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"succeeded"}`))
	}))
	defer server.Close()

	status, err := SubmitWorkflowRun(context.Background(), server.Client(), server.URL, "worker-secret", "run-id")
	if err != nil || status != "succeeded" {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func TestHatchetWorkerRegistersMiniAppWorkflowTask(t *testing.T) {
	raw, err := os.ReadFile("../../../cmd/cerebro_hatchet_worker/main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{"cerebro-mini-app-workflow", "cerebro-mini-app-schedule-sweep", "SubmitWorkflowRun", "SubmitWorkflowTrigger", "CEREBRO_APP_WORKFLOW_EXECUTE_URL", "CEREBRO_APP_WORKFLOW_TRIGGER_URL", "CEREBRO_APP_WORKFLOW_INGEST_KEY"} {
		if !strings.Contains(source, required) {
			t.Errorf("Hatchet worker is missing %q", required)
		}
	}
}
