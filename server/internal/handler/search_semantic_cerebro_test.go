// CEREBRO-PATCH(search-semantic-test-fir-2604): handler-level integration
// tests for the FIR-2604 hybrid (FTS + vector) pipeline.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/semantic"
)

// installLookupProvider points the test handler at a LookupProvider with the
// given vocabulary, runs the worker once to populate cerebro_search_embedding
// for any pending queue rows, and restores the previous SemanticSearch on
// cleanup. Skips the test when the semantic store is unavailable (pgvector
// missing) — the handler then transparently runs Del 1's keyword-only path,
// which the existing FTS tests already cover.
func installLookupProvider(t *testing.T, vectors map[string][]float32) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("test DB not available")
	}
	if !semantic.StoreAvailable(context.Background(), testPool) {
		t.Skip("pgvector store unavailable — hybrid path not exercised")
	}
	provider := semantic.NewLookupProvider("test:hybrid", vectors)
	cfg := semantic.Config{Enabled: true, Provider: "deterministic", BatchSize: 32}
	prev := testHandler.SemanticSearch
	testHandler.SemanticSearch = NewSemanticSearch(testPool, provider, cfg)
	t.Cleanup(func() { testHandler.SemanticSearch = prev })

	// Drain any pending queue entries the migration triggers enqueued for
	// the seeded fixtures. The worker is the same code path that runs in
	// production.
	w := semantic.NewWorker(testPool, provider, cfg)
	for i := 0; i < 5; i++ {
		processed, err := w.Tick(context.Background())
		if err != nil {
			t.Fatalf("worker tick (round %d): %v", i, err)
		}
		if processed == 0 {
			break
		}
	}
}

// hybridSearch is a small helper that bypasses the SearchIssues router shim
// and goes straight to SearchIssuesCerebro. Keeps the test focused on the
// hybrid behaviour rather than auth/middleware.
func hybridSearch(t *testing.T, q string, includeClosed bool) SearchIssuesResponseEnvelope {
	t.Helper()
	params := url.Values{}
	params.Set("q", q)
	// FIR-2664: semantic ranking is now opt-in. These hybrid-pipeline tests
	// must request it explicitly; the bare /api/issues/search path runs the
	// keyword-only Build now.
	params.Set("semantic", "true")
	if includeClosed {
		params.Set("include_closed", "true")
	}
	req := httptest.NewRequest("GET", "/api/issues/search?"+params.Encode(), nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.SearchIssuesCerebro(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchIssuesCerebro: %d %s", w.Code, w.Body.String())
	}
	var env SearchIssuesResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, w.Body.String())
	}
	return env
}

// unitVecAt returns an EmbeddingDim-length unit vector with weight on a
// single bucket. Tests that want "A and B mean the same thing" point them
// at the same bucket; tests that want them disjoint pick far-apart buckets.
func unitVecAt(bucket int) []float32 {
	v := make([]float32, semantic.EmbeddingDim)
	v[bucket%semantic.EmbeddingDim] = 1
	return v
}

// CEREBRO-PATCH(search-perf-test-fir-2664): no-semantic counterpart to hybridSearch.
// keywordSearch is the no-`semantic=true` counterpart to hybridSearch — it
// exercises the FIR-2664 gate that keeps a wired semantic provider from
// silently turning every text query into the heavy pgvector pipeline.
func keywordSearch(t *testing.T, q string, includeClosed bool) SearchIssuesResponseEnvelope {
	t.Helper()
	params := url.Values{}
	params.Set("q", q)
	if includeClosed {
		params.Set("include_closed", "true")
	}
	req := httptest.NewRequest("GET", "/api/issues/search?"+params.Encode(), nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.SearchIssuesCerebro(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchIssuesCerebro: %d %s", w.Code, w.Body.String())
	}
	var env SearchIssuesResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, w.Body.String())
	}
	return env
}

// TestSearch_Semantic_OptInRequired asserts the FIR-2664 gate: even with a
// semantic provider wired AND an issue whose embedding matches the query
// bucket, the bare /api/issues/search (no ?semantic=true) must NOT surface a
// match that exists only in vector space — that path returns a slow pgvector
// + RRF query and was the cause of the ~1.5s baseline.
func TestSearch_Semantic_OptInRequired(t *testing.T) {
	id := seedIssueWithComment(t, "Quiet maintenance window", "", "")

	installLookupProvider(t, map[string][]float32{
		"quiet maintenance window\n": unitVecAt(7),
		"rollout":                    unitVecAt(7), // same bucket as the issue
	})

	env := keywordSearch(t, "rollout", false)
	for _, hit := range env.Issues {
		if hit.ID == id {
			t.Errorf("semantic-only match leaked through without ?semantic=true — gate (FIR-2664) is not enforced")
		}
	}
}

func TestSearch_Semantic_FindsSynonymWithoutLexicalOverlap(t *testing.T) {
	// "Deployment fails on Friday" — title has no overlap with "rollout".
	// Without semantic search the query "rollout" returns nothing here.
	id := seedIssueWithComment(t, "Deployment fails on Friday", "", "")

	installLookupProvider(t, map[string][]float32{
		"deployment fails on friday\n": unitVecAt(11), // title + blank desc — see store.FetchPending
		"rollout":                      unitVecAt(11), // same bucket as the issue
	})

	env := hybridSearch(t, "rollout", false)
	found := false
	for _, hit := range env.Issues {
		if hit.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected semantic search to find issue %q for query 'rollout' "+
			"(no lexical overlap with 'Deployment fails on Friday'), got %d hits",
			id, len(env.Issues))
	}
}

func TestSearch_Semantic_HybridRanksBothPathsAboveUnrelated(t *testing.T) {
	// Three issues:
	//   A — keyword-only hit for "deploy" (FTS finds it; embedding distant)
	//   B — semantic-only hit (no lexical overlap; same embedding bucket as
	//       the query)
	//   C — neither hit (embedding distant; no FTS match either)
	//
	// On a small fixture every embedded row makes it into the vector
	// candidate pool, so what matters for hybrid quality is RANK, not
	// presence. We assert that A and B rank STRICTLY above C in the
	// returned order — that is the property RRF actually delivers.
	idA := seedIssueWithComment(t, "Deploy pipeline regression", "", "")
	idB := seedIssueWithComment(t, "Production release outage", "", "")
	idC := seedIssueWithComment(t, "Lunch menu rotation", "", "")

	installLookupProvider(t, map[string][]float32{
		"deploy pipeline regression\n": unitVecAt(7),
		"production release outage\n":  unitVecAt(21),
		"lunch menu rotation\n":        unitVecAt(40),
		"deploy":                       unitVecAt(21),
	})

	env := hybridSearch(t, "deploy", false)

	posA, posB, posC := -1, -1, -1
	for i, hit := range env.Issues {
		switch hit.ID {
		case idA:
			posA = i
		case idB:
			posB = i
		case idC:
			posC = i
		}
	}
	if posA < 0 {
		t.Errorf("keyword hit (A) missing from hybrid result")
	}
	if posB < 0 {
		t.Errorf("semantic-only hit (B) missing from hybrid result — RRF should surface vector candidates")
	}
	if posC >= 0 && (posA < 0 || posC < posA) {
		t.Errorf("unrelated issue (C, pos=%d) outranked keyword hit (A, pos=%d) — RRF blended wrong", posC, posA)
	}
	if posC >= 0 && (posB < 0 || posC < posB) {
		t.Errorf("unrelated issue (C, pos=%d) outranked semantic hit (B, pos=%d) — RRF blended wrong", posC, posB)
	}
}

func TestSearch_Semantic_RespectsAccessFilters(t *testing.T) {
	// Filter must apply to vector-only candidates too. Seed an issue whose
	// title doesn't FTS-match the query but whose vector does, then narrow
	// with status:done — the issue is status=todo, so it must not appear.
	id := seedIssueWithComment(t, "Quiet maintenance window", "", "")

	installLookupProvider(t, map[string][]float32{
		"quiet maintenance window\n": unitVecAt(33),
		"deploy":                     unitVecAt(33),
	})

	env := hybridSearch(t, "deploy status:done", false)
	for _, hit := range env.Issues {
		if hit.ID == id {
			t.Errorf("issue with status=todo leaked through status:done filter via semantic path")
		}
	}
}

func TestSearch_Semantic_WorkspaceIsolated(t *testing.T) {
	// pgvector NearestNeighbours is scoped by workspace_id — verify that an
	// issue in a foreign workspace cannot surface in this workspace's
	// semantic search, even when the vectors collide.
	myIssueID := seedIssueWithComment(t, "Local deploy work", "", "")

	// Foreign workspace.
	var foreignWsID string
	ctx := context.Background()
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ($1, $2)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, "Foreign WS for Hybrid", "foreign-ws-hybrid").Scan(&foreignWsID); err != nil {
		t.Fatalf("seed foreign ws: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWsID)
	})

	var foreignIssueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, description, status, priority, kind, creator_type, creator_id, number)
		VALUES ($1, $2, '', 'todo', 'none', 'issue', 'member', $3,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id`,
		foreignWsID, "Foreign deploy work", testUserID,
	).Scan(&foreignIssueID); err != nil {
		t.Fatalf("seed foreign issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, foreignIssueID)
	})

	installLookupProvider(t, map[string][]float32{
		"local deploy work\n":   unitVecAt(50),
		"foreign deploy work\n": unitVecAt(50),
		"deploy":                unitVecAt(50),
	})

	env := hybridSearch(t, "deploy", false)
	for _, hit := range env.Issues {
		if hit.ID == foreignIssueID {
			t.Errorf("foreign-workspace issue leaked into local semantic search results")
		}
	}
	// Sanity — the local one should still surface so the test verifies the
	// scope rather than just an empty result.
	found := false
	for _, hit := range env.Issues {
		if hit.ID == myIssueID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected local issue to surface; got %d hits", len(env.Issues))
	}
}
