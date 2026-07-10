package runtime

// CEREBRO-PATCH(memory-autorecall): FIR-1794 layer 3 — unit tests for the
// automatic run-start recall: gate subsets decide which stores are searched,
// identity is stamped server-side, and every failure path degrades to "no
// injection" instead of failing the run.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestMemoryAutoRecallQueryJoinsAndBounds(t *testing.T) {
	if got := CerebroMemoryAutoRecallQuery("  ", "", "\n"); got != "" {
		t.Fatalf("all-empty parts = %q, want empty", got)
	}
	if got := CerebroMemoryAutoRecallQuery("Fix login", "", "trigger text"); got != "Fix login\ntrigger text" {
		t.Fatalf("joined = %q", got)
	}
	long := strings.Repeat("x", 2*memoryAutoRecallMaxQueryChars)
	if got := CerebroMemoryAutoRecallQuery(long); len(got) != memoryAutoRecallMaxQueryChars {
		t.Fatalf("len = %d, want %d", len(got), memoryAutoRecallMaxQueryChars)
	}
}

func TestMemoryAutoRecallFlagOffOrEmptyQueryInjectsNothing(t *testing.T) {
	srv, calls := memoryServiceCapture(t)

	base := memToolBase(t, srv.URL, &fakeMemoryGateQuerier{}) // flag off
	if got := cerebroMemoryAutoRecallBlock(context.Background(), base, "anything"); got != "" {
		t.Fatalf("flag off block = %q, want empty", got)
	}
	base = memToolBase(t, srv.URL, allGatesOpen())
	if got := cerebroMemoryAutoRecallBlock(context.Background(), base, "   "); got != "" {
		t.Fatalf("empty query block = %q, want empty", got)
	}
	if len(*calls) != 0 {
		t.Fatalf("service calls = %d, want 0", len(*calls))
	}
}

func TestMemoryAutoRecallCompanyOnlyWithoutReadSwitch(t *testing.T) {
	srv, calls := memoryServiceCapture(t)
	// Flag on, but no capability / read switch: private store must be skipped.
	base := memToolBase(t, srv.URL, &fakeMemoryGateQuerier{flags: memFlagOn()})

	block := cerebroMemoryAutoRecallBlock(context.Background(), base, "deploy story")
	if block == "" {
		t.Fatal("company recall should inject a block")
	}
	if strings.Contains(block, "[private]") {
		t.Fatalf("block leaks private store without read switch:\n%s", block)
	}
	if len(*calls) != 1 {
		t.Fatalf("service calls = %d, want 1 (company only)", len(*calls))
	}
	got := (*calls)[0]
	if got["scope"] != memoryScopeCompany {
		t.Fatalf("scope = %v, want company", got["scope"])
	}
	if got["subject_id"] != "workspace-"+memTestWorkspace {
		t.Fatalf("subject_id = %v, want stamped workspace subject", got["subject_id"])
	}
}

func TestMemoryAutoRecallBothStoresWhenAllGatesOpen(t *testing.T) {
	srv, calls := memoryServiceCapture(t)
	base := memToolBase(t, srv.URL, allGatesOpen())

	block := cerebroMemoryAutoRecallBlock(context.Background(), base, "deploy story")
	if !strings.Contains(block, "[private]") || !strings.Contains(block, "[company]") {
		t.Fatalf("block missing a store label:\n%s", block)
	}
	if !strings.Contains(block, "## Recalled memories (automatic)") {
		t.Fatalf("block missing heading:\n%s", block)
	}
	if len(*calls) != 2 {
		t.Fatalf("service calls = %d, want 2", len(*calls))
	}
	private := (*calls)[0]
	if private["scope"] != memoryScopePrivate {
		t.Fatalf("first call scope = %v, want private", private["scope"])
	}
	if private["subject_id"] != "user-"+memTestUser+"-agent-"+memTestAgent {
		t.Fatalf("private subject_id = %v, want stamped user+agent subject", private["subject_id"])
	}
	if lim, ok := private["limit"].(float64); !ok || int(lim) != memoryAutoRecallLimit {
		t.Fatalf("limit = %v, want %d", private["limit"], memoryAutoRecallLimit)
	}
}

func TestMemoryAutoRecallFailsOpenOnServiceError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	base := memToolBase(t, srv.URL, allGatesOpen())
	if got := cerebroMemoryAutoRecallBlock(context.Background(), base, "deploy story"); got != "" {
		t.Fatalf("service error block = %q, want empty (fail open)", got)
	}
}

func TestMemoryAutoRecallEmptyResultsInjectNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"[]"}]}}`))
	}))
	t.Cleanup(srv.Close)

	base := memToolBase(t, srv.URL, allGatesOpen())
	if got := cerebroMemoryAutoRecallBlock(context.Background(), base, "deploy story"); got != "" {
		t.Fatalf("empty-result block = %q, want empty", got)
	}
}

func TestMemoryAutoRecallNoOriginUserSearchesCompanyOnly(t *testing.T) {
	srv, calls := memoryServiceCapture(t)
	base := memToolBase(t, srv.URL, allGatesOpen())
	base.origin = pgtype.UUID{}

	block := cerebroMemoryAutoRecallBlock(context.Background(), base, "deploy story")
	if strings.Contains(block, "[private]") {
		t.Fatalf("block leaks private store without an originating user:\n%s", block)
	}
	if len(*calls) != 1 {
		t.Fatalf("service calls = %d, want 1 (company only)", len(*calls))
	}
}
