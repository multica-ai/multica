package contextguard

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubQueries is a QueryRunner test double that records the workspace
// lookup and lets each test pin the project-resource list. Lives in
// the test file so production code never references it; the package
// surface is the QueryRunner interface, so swapping in a fake is
// one-liner-friendly.
type stubQueries struct {
	workspace        db.Workspace
	workspaceErr     error
	workspaceCalled  int
	resources        []db.ProjectResource
	resourcesErr     error
	resourcesCalls   int
	lastProjectIDArg pgtype.UUID
}

func (s *stubQueries) GetWorkspace(_ context.Context, _ pgtype.UUID) (db.Workspace, error) {
	s.workspaceCalled++
	return s.workspace, s.workspaceErr
}

func (s *stubQueries) ListProjectResources(_ context.Context, projectID pgtype.UUID) ([]db.ProjectResource, error) {
	s.resourcesCalls++
	s.lastProjectIDArg = projectID
	return s.resources, s.resourcesErr
}

func newWorkspace(id pgtype.UUID, repos string) db.Workspace {
	return db.Workspace{
		ID:    id,
		Repos: []byte(repos),
	}
}

func TestHasUsableContext_WorkspaceReposPasses(t *testing.T) {
	wid := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	stub := &stubQueries{workspace: newWorkspace(wid, `[{"url":"https://example.com/repo.git"}]`)}

	svc := NewService(stub, Defaults{Policy: PolicyDefault})
	reason, err := svc.HasUsableContext(context.Background(), wid, pgtype.UUID{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reason.OK {
		t.Fatalf("expected OK=true with repos, got %+v", reason)
	}
	if !reason.HasWorkspaceRepos {
		t.Fatal("expected HasWorkspaceRepos=true")
	}
	if stub.resourcesCalls != 0 {
		t.Fatalf("expected zero ListProjectResources calls (projectID invalid), got %d", stub.resourcesCalls)
	}
}

func TestHasUsableContext_NoContextFails(t *testing.T) {
	wid := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	stub := &stubQueries{workspace: newWorkspace(wid, `[]`)}

	svc := NewService(stub, Defaults{})
	reason, err := svc.HasUsableContext(context.Background(), wid, pgtype.UUID{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason.OK {
		t.Fatalf("expected OK=false with empty repos, got %+v", reason)
	}
	if reason.Hint == "" {
		t.Fatal("expected non-empty hint to surface to the user")
	}
}

func TestHasUsableContext_ProjectLocalDirectoryPasses(t *testing.T) {
	wid := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	pid := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	stub := &stubQueries{
		workspace: newWorkspace(wid, `[]`),
		resources: []db.ProjectResource{
			{ResourceType: "github_repo"},
			{ResourceType: "local_directory"},
		},
	}

	svc := NewService(stub, Defaults{})
	reason, err := svc.HasUsableContext(context.Background(), wid, pid)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reason.OK {
		t.Fatalf("expected OK=true via project local_directory, got %+v", reason)
	}
	if !reason.HasLocalDirectory {
		t.Fatal("expected HasLocalDirectory=true")
	}
	if !stub.lastProjectIDArg.Valid {
		t.Fatal("ListProjectResources was called with invalid projectID")
	}
}

func TestHasUsableContext_WorkspaceLoadError(t *testing.T) {
	wid := pgtype.UUID{Bytes: [16]byte{5}, Valid: true}
	stub := &stubQueries{workspaceErr: errors.New("workspace blew up")}

	svc := NewService(stub, Defaults{})
	reason, err := svc.HasUsableContext(context.Background(), wid, pgtype.UUID{})

	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if reason.OK {
		t.Fatal("expected OK=false on workspace load failure")
	}
	if reason.Hint == "" {
		t.Fatal("expected hint to be set even on error")
	}
}

func TestHasUsableContext_MalformedReposTreatedAsEmpty(t *testing.T) {
	wid := pgtype.UUID{Bytes: [16]byte{6}, Valid: true}
	stub := &stubQueries{workspace: newWorkspace(wid, `not-an-array`)}

	svc := NewService(stub, Defaults{})
	reason, err := svc.HasUsableContext(context.Background(), wid, pgtype.UUID{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason.HasWorkspaceRepos {
		t.Fatal("expected HasWorkspaceRepos=false on malformed JSON")
	}
}

func TestResolvePolicy_Default(t *testing.T) {
	svc := NewService(&stubQueries{}, Defaults{})
	workspace := db.Workspace{Settings: []byte(`{}`)}
	if got := svc.ResolvePolicy(workspace); got != PolicyDefault {
		t.Fatalf("expected default policy %q, got %q", PolicyDefault, got)
	}
}

func TestResolvePolicy_WorkspaceOverride(t *testing.T) {
	svc := NewService(&stubQueries{}, Defaults{Policy: PolicyOff})
	workspace := db.Workspace{Settings: []byte(`{"context_guard_policy":"warn"}`)}
	if got := svc.ResolvePolicy(workspace); got != PolicyWarn {
		t.Fatalf("expected workspace override %q, got %q", PolicyWarn, got)
	}
}

func TestResolvePolicy_DefaultsOverridesEmptySettings(t *testing.T) {
	svc := NewService(&stubQueries{}, Defaults{Policy: PolicyReject})
	workspace := db.Workspace{Settings: []byte(`{}`)}
	if got := svc.ResolvePolicy(workspace); got != PolicyReject {
		t.Fatalf("expected defaults-supplied policy %q, got %q", PolicyReject, got)
	}
}

func TestResolvePolicy_UnknownValueFallsThrough(t *testing.T) {
	svc := NewService(&stubQueries{}, Defaults{Policy: PolicyWarn})
	workspace := db.Workspace{Settings: []byte(`{"context_guard_policy":"block_and_notiyf"}`)}
	// Typo should NOT silently become a fallback; the workspace-level
	// value is ignored and we move to defaults.
	if got := svc.ResolvePolicy(workspace); got != PolicyWarn {
		t.Fatalf("expected defaults policy %q after typo, got %q", PolicyWarn, got)
	}
}

func TestEncodeReason_RoundTrip(t *testing.T) {
	reason := Reason{
		Policy:            PolicyBlockAndNotify,
		OK:                false,
		WorkspaceID:       "ws-1",
		ProjectID:         "proj-1",
		HasWorkspaceRepos: false,
		HasLocalDirectory: false,
		Hint:              "no repos",
	}
	bytes, err := EncodeReason(reason)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	var roundTripped Reason
	if err := json.Unmarshal(bytes, &roundTripped); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	// ProjectResources is a []string which makes struct equality
	// non-comparable; assert field-by-field on the ones we care
	// about for round-trip integrity.
	if roundTripped.Policy != reason.Policy {
		t.Fatalf("Policy mismatch: got %q want %q", roundTripped.Policy, reason.Policy)
	}
	if roundTripped.OK != reason.OK {
		t.Fatalf("OK mismatch: got %v want %v", roundTripped.OK, reason.OK)
	}
	if roundTripped.WorkspaceID != reason.WorkspaceID {
		t.Fatalf("WorkspaceID mismatch: got %q want %q", roundTripped.WorkspaceID, reason.WorkspaceID)
	}
	if roundTripped.ProjectID != reason.ProjectID {
		t.Fatalf("ProjectID mismatch: got %q want %q", roundTripped.ProjectID, reason.ProjectID)
	}
	if roundTripped.Hint != reason.Hint {
		t.Fatalf("Hint mismatch: got %q want %q", roundTripped.Hint, reason.Hint)
	}
	if roundTripped.HasWorkspaceRepos != reason.HasWorkspaceRepos {
		t.Fatalf("HasWorkspaceRepos mismatch: got %v want %v", roundTripped.HasWorkspaceRepos, reason.HasWorkspaceRepos)
	}
	if roundTripped.HasLocalDirectory != reason.HasLocalDirectory {
		t.Fatalf("HasLocalDirectory mismatch: got %v want %v", roundTripped.HasLocalDirectory, reason.HasLocalDirectory)
	}
}