package daemon

import (
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const providerEmptyContentFixture = `{"message":"Provider returned empty content","type":"server_error","code":"server_error"}`

func TestShouldRetryProviderEmptyContent(t *testing.T) {
	failed := agent.Result{Status: "failed", Error: providerEmptyContentFixture}
	if !shouldRetryProviderEmptyContent(failed, 0) {
		t.Fatal("empty content before a tool must get one in-run retry")
	}
	if shouldRetryProviderEmptyContent(failed, 1) {
		t.Fatal("empty content after a tool must not be retried")
	}
	if !shouldRetryProviderEmptyContent(agent.Result{Status: "completed", Output: providerEmptyContentFixture}, 0) {
		t.Fatal("provider error returned as completed output must get one in-run retry")
	}
}

func TestReconcileProviderRetryResult(t *testing.T) {
	first := agent.Result{Status: "failed", Error: providerEmptyContentFixture}
	retry := agent.Result{Status: "completed", Output: "done"}
	got, tools := reconcileProviderRetryResult(first, nil, 0, retry, 0, nil)
	if got.Status != "completed" || tools != 0 {
		t.Fatalf("retry result = %+v, tools=%d", got, tools)
	}

	got, _ = reconcileProviderRetryResult(first, nil, 0, agent.Result{}, 0, errors.New("start failed"))
	if got.Error != providerEmptyContentFixture {
		t.Fatalf("failed retry replaced original diagnostic: %+v", got)
	}
}
