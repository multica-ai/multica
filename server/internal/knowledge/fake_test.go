package knowledge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFakeConformance(t *testing.T) {
	conformance{
		TenantName: "fake-run",
		Model:      EmbeddingModelSpec{Name: "bge-m3", Provider: "ollama", BaseURL: "http://ollama.test:11434", Dimension: 1024},
		SearchWait: time.Second,
		Token:      "zephyrquartz",
	}.run(t, NewFake())
}

func TestFakeIsolatesTenants(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	setup := func(name string) (Workspace, KnowledgeBase) {
		tenant, _ := f.EnsureTenant(ctx, name, "")
		key, _ := f.CreateWorkspaceKey(ctx, tenant.ID, "k")
		ws := f.Workspace(key)
		model, _ := ws.EnsureEmbeddingModel(ctx, EmbeddingModelSpec{Name: "m", Dimension: 8})
		kb, err := ws.CreateKnowledgeBase(ctx, KnowledgeBaseSpec{Name: "kb", EmbeddingModelID: model.ID})
		if err != nil {
			t.Fatalf("CreateKnowledgeBase: %v", err)
		}
		return ws, kb
	}
	wsA, kbA := setup("a")
	wsB, _ := setup("b")
	if _, err := wsA.CreateManual(ctx, kbA.ID, ManualDocument{Title: "secret", Content: "alpha only", Publish: true}); err != nil {
		t.Fatal(err)
	}
	hits, err := wsB.Search(ctx, SearchRequest{Query: "alpha"})
	if err != nil || len(hits) != 0 {
		t.Fatalf("tenant B saw tenant A's document: %+v, %v", hits, err)
	}
	if _, err := wsB.CreateManual(ctx, kbA.ID, ManualDocument{Title: "x", Content: "y"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("writing into another tenant's base: %v", err)
	}
}

func TestFakeDraftsAreNotSearchableUntilPublished(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	tenant, _ := f.EnsureTenant(ctx, "t", "")
	key, _ := f.CreateWorkspaceKey(ctx, tenant.ID, "k")
	ws := f.Workspace(key)
	model, _ := ws.EnsureEmbeddingModel(ctx, EmbeddingModelSpec{Name: "m", Dimension: 8})
	kb, _ := ws.CreateKnowledgeBase(ctx, KnowledgeBaseSpec{Name: "kb", EmbeddingModelID: model.ID})

	d, _ := ws.CreateManual(ctx, kb.ID, ManualDocument{Title: "d", Content: "refund policy"})
	if hits, _ := ws.Search(ctx, SearchRequest{Query: "refund"}); len(hits) != 0 {
		t.Fatalf("draft found: %+v", hits)
	}
	if _, err := ws.Publish(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if hits, _ := ws.Search(ctx, SearchRequest{Query: "refund"}); len(hits) != 1 {
		t.Fatalf("published document not found: %+v", hits)
	}
	if hits, _ := ws.Search(ctx, SearchRequest{Query: "refund", MinScore: 1.5}); len(hits) != 0 {
		t.Fatalf("MinScore ignored: %+v", hits)
	}
}

func TestFakeMCPTokenIsShownOnce(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	tenant, _ := f.EnsureTenant(ctx, "t", "")
	key, _ := f.CreateWorkspaceKey(ctx, tenant.ID, "k")
	ws := f.Workspace(key)
	model, _ := ws.EnsureEmbeddingModel(ctx, EmbeddingModelSpec{Name: "m", Dimension: 8})
	kb, _ := ws.CreateKnowledgeBase(ctx, KnowledgeBaseSpec{Name: "kb", EmbeddingModelID: model.ID})
	ep, err := ws.CreateMCPEndpoint(ctx, MCPEndpointSpec{Name: "e", KnowledgeBaseIDs: []string{kb.ID}, Tools: ReadOnlyMCPTools})
	if err != nil || ep.Token == "" {
		t.Fatalf("create: %+v, %v", ep, err)
	}
	listed, _ := ws.ListMCPEndpoints(ctx)
	if len(listed) != 1 || listed[0].Token != "" {
		t.Fatalf("listing exposed a token: %+v", listed)
	}
	rotated, _ := ws.RotateMCPEndpointToken(ctx, ep.ID)
	if rotated.Token == "" || rotated.Token == ep.Token {
		t.Fatalf("rotation did not issue a new token: %q vs %q", rotated.Token, ep.Token)
	}
	if got, want := f.MCPURL(ep.ID), "http://knowledge.test/mcp/"+ep.ID; got != want {
		t.Fatalf("MCPURL = %q, want %q", got, want)
	}
}

func TestFakeFailureInjection(t *testing.T) {
	ctx := context.Background()
	f := NewFake()

	f.SetUnavailable(true)
	if err := f.Ping(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Ping while down = %v", err)
	}
	f.ClearFailures()
	if err := f.Ping(ctx); err != nil {
		t.Fatalf("Ping after recovery = %v", err)
	}

	boom := errors.New("boom")
	f.FailNext(OpEnsureTenant, boom)
	if _, err := f.EnsureTenant(ctx, "t", ""); !errors.Is(err, boom) {
		t.Fatalf("first call = %v, want the injected error", err)
	}
	if _, err := f.EnsureTenant(ctx, "t", ""); err != nil {
		t.Fatalf("second call = %v, want success (FailNext is once)", err)
	}
	if got := f.Calls(OpEnsureTenant); got != 2 {
		t.Fatalf("Calls = %d, want 2", got)
	}

	f.FailAlways(OpCreateWorkspaceKey, boom)
	for i := 0; i < 2; i++ {
		if _, err := f.CreateWorkspaceKey(ctx, "10000", "k"); !errors.Is(err, boom) {
			t.Fatalf("FailAlways call %d = %v", i, err)
		}
	}
}

func TestFakeIsSafeForConcurrentUse(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	tenant, _ := f.EnsureTenant(ctx, "t", "")
	key, _ := f.CreateWorkspaceKey(ctx, tenant.ID, "k")
	ws := f.Workspace(key)
	model, _ := ws.EnsureEmbeddingModel(ctx, EmbeddingModelSpec{Name: "m", Dimension: 8})
	kb, _ := ws.CreateKnowledgeBase(ctx, KnowledgeBaseSpec{Name: "kb", EmbeddingModelID: model.ID})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ws.CreateManual(ctx, kb.ID, ManualDocument{Title: "t", Content: strings.Repeat("x ", 3), Publish: true})
			_, _ = ws.Search(ctx, SearchRequest{Query: "x"})
			_, _ = ws.ListDocuments(ctx, kb.ID, ListOptions{})
		}()
	}
	wg.Wait()
	if page, _ := ws.ListDocuments(ctx, kb.ID, ListOptions{PageSize: 100}); page.Total != 20 {
		t.Fatalf("documents = %d, want 20", page.Total)
	}
}
