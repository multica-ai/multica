package evals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubExecutor lets the wiring test flip the flag on without a live engine.
type stubExecutor struct{}

func (stubExecutor) Execute(context.Context, Eval) (RunExecution, error) {
	return RunExecution{}, nil
}

// TestRunNowDisabledByDefault proves the flag is OFF by default: with no
// executor wired, POST /{id}/run reports 404 and never touches the store, so the
// existing run path is unchanged until the gateway is deliberately configured.
func TestRunNowDisabledByDefault(t *testing.T) {
	h := &Handler{} // no executor: default-OFF
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/eval-id/run", nil)

	h.RunNow(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d when run-now is disabled", rec.Code, http.StatusNotFound)
	}
}

// TestWithRunExecutorEnablesFlag proves WithRunExecutor arms the endpoint and
// returns the handler for chained wiring.
func TestWithRunExecutorEnablesFlag(t *testing.T) {
	h := (&Handler{store: NewStore(nil)}).WithRunExecutor(stubExecutor{})
	if !h.runNowEnabled {
		t.Fatal("WithRunExecutor did not set the executor")
	}
}
