package evals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// stubExecutor lets the wiring test flip the flag on without a live engine.
type stubExecutor struct{}

func (stubExecutor) ExecuteRun(context.Context, Eval) (EvalRunInput, error) {
	return EvalRunInput{}, nil
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
	h := (&Handler{}).WithRunExecutor(stubExecutor{})
	if h.executor == nil {
		t.Fatal("WithRunExecutor did not set the executor")
	}
}

// TestWithExecutorResolverEnablesFlag proves WithExecutorResolver arms the
// endpoint with per-workspace credentials and returns the handler for chaining.
func TestWithExecutorResolverEnablesFlag(t *testing.T) {
	h := (&Handler{}).WithExecutorResolver(func(context.Context, uuid.UUID) (RunExecutor, bool) {
		return stubExecutor{}, true
	})
	if h.resolver == nil {
		t.Fatal("WithExecutorResolver did not set the resolver")
	}
}

// TestRunNowResolverArmsEndpoint proves a wired resolver takes the endpoint past
// the default-OFF 404 guard: the request now reaches authentication (401 here)
// instead of the "run-now is not enabled" 404. So a workspace whose gateway
// resolves gets a real run, without any CEREBRO_EVALS_GATEWAY_* server env var.
func TestRunNowResolverArmsEndpoint(t *testing.T) {
	h := (&Handler{}).WithExecutorResolver(func(context.Context, uuid.UUID) (RunExecutor, bool) {
		return stubExecutor{}, true
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/eval-id/run", nil)

	h.RunNow(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("status = 404: resolver did not arm the endpoint")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (armed endpoint reaches auth)", rec.Code, http.StatusUnauthorized)
	}
}
