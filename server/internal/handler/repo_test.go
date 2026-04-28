package handler

import (
	"context"
	"sort"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// All tests in this file exercise the repo / repo_binding tables added in
// migration 060. The tests run against the live test database, so the
// migration backfill itself is exercised at process start by the CI step that
// runs `go run ./cmd/migrate up` before `go test`.

// TestSetRepoBindingsForScope_RoundTrip validates the wipe-and-replace write
// path exposed via setRepoBindingsForScope, plus the read path via
// loadWorkspaceRepoData. This is the primary contract handlers depend on:
// PATCH /workspaces/:id with a `repos` array must produce exactly that set on
// the next GET, with URL trim/dedup applied.
func TestSetRepoBindingsForScope_RoundTrip(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)

	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	// Empty initial state.
	got, err := testHandler.loadWorkspaceRepoData(ctx, wsUUID)
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty initial repos, got %d", len(got))
	}

	// Write two repos. Whitespace and a duplicate URL exercise the
	// normalizeWorkspaceRepos path.
	want := []RepoData{
		{URL: "  git@example.com:team/api.git  ", Description: " API "},
		{URL: "git@example.com:team/web.git", Description: "Web"},
		{URL: "git@example.com:team/api.git", Description: "duplicate, dropped"},
	}
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, want); err != nil {
		t.Fatalf("setRepoBindingsForScope: %v", err)
	}

	got, err = testHandler.loadWorkspaceRepoData(ctx, wsUUID)
	if err != nil {
		t.Fatalf("load after write: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 repos after dedup, got %d: %+v", len(got), got)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].URL < got[j].URL })
	if got[0].URL != "git@example.com:team/api.git" || got[0].Description != "API" {
		t.Fatalf("unexpected first repo: %+v", got[0])
	}
	if got[1].URL != "git@example.com:team/web.git" || got[1].Description != "Web" {
		t.Fatalf("unexpected second repo: %+v", got[1])
	}

	// Replace with a different set: the previous bindings must be wiped.
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: "git@example.com:team/mobile.git", Description: "Mobile"},
	}); err != nil {
		t.Fatalf("setRepoBindingsForScope replace: %v", err)
	}
	got, err = testHandler.loadWorkspaceRepoData(ctx, wsUUID)
	if err != nil {
		t.Fatalf("load after replace: %v", err)
	}
	if len(got) != 1 || got[0].URL != "git@example.com:team/mobile.git" {
		t.Fatalf("expected only mobile.git after replace, got %+v", got)
	}

	// Empty payload clears the set.
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, nil); err != nil {
		t.Fatalf("setRepoBindingsForScope clear: %v", err)
	}
	got, err = testHandler.loadWorkspaceRepoData(ctx, wsUUID)
	if err != nil {
		t.Fatalf("load after clear: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty after clear, got %+v", got)
	}
}

// TestSetRepoBindingsForScope_OrphanGC asserts that repo rows with no
// remaining binding are pruned at the end of a setRepoBindingsForScope call.
// Without this the catalog accumulates stale entries every time a user edits
// their workspace settings.
func TestSetRepoBindingsForScope_OrphanGC(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)

	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	const orphanURL = "git@example.com:team/will-orphan.git"
	const keepURL = "git@example.com:team/will-keep.git"

	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: orphanURL, Description: "orphan"},
		{URL: keepURL, Description: "keep"},
	}); err != nil {
		t.Fatalf("initial bind: %v", err)
	}

	// Drop the first repo; setRepoBindingsForScope replaces the whole binding
	// set, then GCs anything that no scope references anymore.
	if err := testHandler.setRepoBindingsForScope(ctx, repoScopeWorkspace, wsUUID, []RepoData{
		{URL: keepURL, Description: "keep"},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// The orphan row should be gone.
	if _, err := testHandler.Queries.GetRepoByURL(ctx, orphanURL); err == nil {
		t.Fatalf("expected orphan repo %q to be GC'd, but it still exists", orphanURL)
	}
	// The kept row should still be there.
	if _, err := testHandler.Queries.GetRepoByURL(ctx, keepURL); err != nil {
		t.Fatalf("expected kept repo %q to survive, got error: %v", keepURL, err)
	}
}

// TestRepoBinding_DescriptionIsPerScope asserts that two scopes binding the
// same git URL each keep their own description. The legacy JSONB column
// stored description per-workspace; if description had stayed on the `repo`
// catalog row instead of moving to `repo_binding`, workspace B writing the
// same URL would silently overwrite workspace A's description on the next
// read. This test pins the contract.
func TestRepoBinding_DescriptionIsPerScope(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	pretendScope := parseUUID("22222222-2222-4222-8222-222222222222")
	const sharedURL = "git@example.com:team/per-scope-desc.git"

	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: "project",
			ScopeID:   pretendScope,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	repo, err := testHandler.Queries.UpsertRepoByURL(ctx, sharedURL)
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	if _, err := testHandler.Queries.CreateRepoBinding(ctx, db.CreateRepoBindingParams{
		RepoID:      repo.ID,
		ScopeType:   repoScopeWorkspace,
		ScopeID:     wsUUID,
		Description: "workspace's view",
	}); err != nil {
		t.Fatalf("workspace binding: %v", err)
	}
	if _, err := testHandler.Queries.CreateRepoBinding(ctx, db.CreateRepoBindingParams{
		RepoID:      repo.ID,
		ScopeType:   "project",
		ScopeID:     pretendScope,
		Description: "project's view",
	}); err != nil {
		t.Fatalf("project binding: %v", err)
	}

	wsRepos, err := testHandler.loadWorkspaceRepoData(ctx, wsUUID)
	if err != nil {
		t.Fatalf("load workspace repos: %v", err)
	}
	if len(wsRepos) != 1 || wsRepos[0].Description != "workspace's view" {
		t.Fatalf("workspace description leaked from project: %+v", wsRepos)
	}

	projRepos, err := testHandler.loadRepoDataByScope(ctx, "project", pretendScope)
	if err != nil {
		t.Fatalf("load project repos: %v", err)
	}
	if len(projRepos) != 1 || projRepos[0].Description != "project's view" {
		t.Fatalf("project description leaked from workspace: %+v", projRepos)
	}
}

// TestRepoBinding_SharedAcrossScopes asserts that a repo bound to multiple
// scopes survives when one scope releases it. This is the contract Step 2 / 3
// will rely on once project- and issue-scoped bindings exist; the same repo
// catalog row should outlive any single binding.
func TestRepoBinding_SharedAcrossScopes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)

	const sharedURL = "git@example.com:team/shared.git"
	// A throwaway UUID standing in for a future project/issue scope. The
	// scope_id column is intentionally polymorphic — see migration 060 — so
	// any UUID works for this test.
	pretendScope := parseUUID("11111111-1111-4111-8111-111111111111")

	t.Cleanup(func() {
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: repoScopeWorkspace,
			ScopeID:   wsUUID,
		})
		testHandler.Queries.DeleteRepoBindingsForScope(ctx, db.DeleteRepoBindingsForScopeParams{
			ScopeType: "project",
			ScopeID:   pretendScope,
		})
		testHandler.Queries.DeleteOrphanRepos(ctx)
	})

	repo, err := testHandler.Queries.UpsertRepoByURL(ctx, sharedURL)
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	for _, params := range []db.CreateRepoBindingParams{
		{RepoID: repo.ID, ScopeType: repoScopeWorkspace, ScopeID: wsUUID, Description: "workspace's view"},
		{RepoID: repo.ID, ScopeType: "project", ScopeID: pretendScope, Description: "project's view"},
	} {
		if _, err := testHandler.Queries.CreateRepoBinding(ctx, params); err != nil {
			t.Fatalf("create binding %+v: %v", params, err)
		}
	}

	// Drop the workspace binding only. The repo row must survive because the
	// project binding still references it.
	if err := testHandler.Queries.DeleteRepoBinding(ctx, db.DeleteRepoBindingParams{
		RepoID:    repo.ID,
		ScopeType: repoScopeWorkspace,
		ScopeID:   wsUUID,
	}); err != nil {
		t.Fatalf("delete workspace binding: %v", err)
	}
	if err := testHandler.Queries.DeleteOrphanRepos(ctx); err != nil {
		t.Fatalf("orphan GC: %v", err)
	}
	if _, err := testHandler.Queries.GetRepoByURL(ctx, sharedURL); err != nil {
		t.Fatalf("shared repo should survive while project binding exists, got: %v", err)
	}

	// Drop the project binding too. Now the repo is unreferenced and should
	// be GC'd on the next sweep.
	if err := testHandler.Queries.DeleteRepoBinding(ctx, db.DeleteRepoBindingParams{
		RepoID:    repo.ID,
		ScopeType: "project",
		ScopeID:   pretendScope,
	}); err != nil {
		t.Fatalf("delete project binding: %v", err)
	}
	if err := testHandler.Queries.DeleteOrphanRepos(ctx); err != nil {
		t.Fatalf("orphan GC: %v", err)
	}
	if _, err := testHandler.Queries.GetRepoByURL(ctx, sharedURL); err == nil {
		t.Fatalf("expected shared repo to be GC'd after last binding dropped")
	}
}
