package issueidentifier

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeQueries implements Queries entirely in memory, with call counters so
// Resolver memoization tests can assert a lookup happened at most once.
type fakeQueries struct {
	// spaces is keyed by space ID. GetWorkspaceSpace additionally checks that
	// the stored space's WorkspaceID matches the requested WorkspaceID, the
	// same way the real "WHERE id = $1 AND workspace_id = $2" query would.
	spaces         map[[16]byte]db.WorkspaceSpace
	spaceCalls     int
	defaultSpaces  map[[16]byte]db.WorkspaceSpace // keyed by workspace ID
	defaultCalls   int
	workspaces     map[[16]byte]db.Workspace
	workspaceCalls int
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{
		spaces:        make(map[[16]byte]db.WorkspaceSpace),
		defaultSpaces: make(map[[16]byte]db.WorkspaceSpace),
		workspaces:    make(map[[16]byte]db.Workspace),
	}
}

var errNotFound = errors.New("not found")

func (f *fakeQueries) GetWorkspaceSpace(_ context.Context, arg db.GetWorkspaceSpaceParams) (db.WorkspaceSpace, error) {
	f.spaceCalls++
	space, ok := f.spaces[arg.ID.Bytes]
	if !ok || space.WorkspaceID.Bytes != arg.WorkspaceID.Bytes {
		return db.WorkspaceSpace{}, errNotFound
	}
	return space, nil
}

func (f *fakeQueries) GetDefaultWorkspaceSpace(_ context.Context, workspaceID pgtype.UUID) (db.WorkspaceSpace, error) {
	f.defaultCalls++
	space, ok := f.defaultSpaces[workspaceID.Bytes]
	if !ok {
		return db.WorkspaceSpace{}, errNotFound
	}
	return space, nil
}

func (f *fakeQueries) GetWorkspace(_ context.Context, id pgtype.UUID) (db.Workspace, error) {
	f.workspaceCalls++
	ws, ok := f.workspaces[id.Bytes]
	if !ok {
		return db.Workspace{}, errNotFound
	}
	return ws, nil
}

func newUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	u, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("uuid.NewRandom: %v", err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}

// TestGeneratePrefix covers the pure name -> prefix derivation: strip
// non-ASCII-letters, uppercase, truncate to 3, fall back to "WS" when nothing
// usable remains. This function is unrelated to the workspace_space CHECK
// constraint's normalize_space_key (migration 131) — it keeps digits out
// entirely and falls back to "WS", not "SPACE" — so cases below assert only
// what identifier.go itself does, not the migration's SQL semantics.
func TestGeneratePrefix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"two words with apostrophe and space", "Jiayuan's Workspace", "JIA"},
		{"two words", "My Space", "MYS"},
		{"already short stays as-is", "AB", "AB"},
		{"single letter stays as-is", "X", "X"},
		{"empty string falls back to WS", "", "WS"},
		{"digits and symbols only fall back to WS", "123!@#", "WS"},
		{"whitespace only falls back to WS", "   ", "WS"},
		{"non-ASCII letters are stripped, falls back to WS", "日本語", "WS"},
		{"mixed non-ASCII and ASCII keeps only ASCII letters", "日本Space", "SPA"},
		{"long single word truncates to first three letters", "Supercalifragilisticexpialidocious", "SUP"},
		{"hyphen and underscore are stripped", "Multi-Ca_Workspace", "MUL"},
		{"leading digits are stripped, not just non-leading", "42Widgets", "WID"},
		{"already uppercase truncates to three", "ACME", "ACM"},
		{"lowercase word is uppercased and truncated", "widgets", "WID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GeneratePrefix(tt.in)
			if got != tt.want {
				t.Errorf("GeneratePrefix(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestPrefixForIssue_UsesSpaceKeyWhenSpaceValidAndFound is step 1 of the
// fallback chain: an issue with a valid, resolvable Space uses that Space's key
// and never touches the workspace-level fallbacks.
func TestPrefixForIssue_UsesSpaceKeyWhenSpaceValidAndFound(t *testing.T) {
	wsID := newUUID(t)
	spaceID := newUUID(t)
	f := newFakeQueries()
	f.spaces[spaceID.Bytes] = db.WorkspaceSpace{ID: spaceID, WorkspaceID: wsID, Key: "ACME"}
	// If the chain fell through to workspace level it would find this and
	// the test would still (wrongly) pass, so also make it wrong to catch
	// a chain that skips the Space step.
	f.defaultSpaces[wsID.Bytes] = db.WorkspaceSpace{Key: "WRONG"}

	issue := db.Issue{SpaceID: spaceID, WorkspaceID: wsID}
	got := PrefixForIssue(context.Background(), f, issue)
	if got != "ACME" {
		t.Errorf("PrefixForIssue() = %q, want %q (space key)", got, "ACME")
	}
}

// TestPrefixForIssue_FallsBackToWorkspaceWhenSpaceIDInvalid is step 2: an
// issue with no Space (SpaceID.Valid == false) skips the Space lookup entirely
// and resolves via PrefixForWorkspace.
func TestPrefixForIssue_FallsBackToWorkspaceWhenSpaceIDInvalid(t *testing.T) {
	wsID := newUUID(t)
	f := newFakeQueries()
	f.defaultSpaces[wsID.Bytes] = db.WorkspaceSpace{Key: "DEFT"}

	issue := db.Issue{WorkspaceID: wsID} // SpaceID left zero-value: Valid == false
	got := PrefixForIssue(context.Background(), f, issue)
	if got != "DEFT" {
		t.Errorf("PrefixForIssue() = %q, want %q (default space key)", got, "DEFT")
	}
	if f.spaceCalls != 0 {
		t.Errorf("GetWorkspaceSpace called %d times, want 0 for an issue with no Space", f.spaceCalls)
	}
}

// TestPrefixForIssue_FallsBackToWorkspaceWhenSpaceLookupFails covers a Space
// that is set on the issue but no longer resolvable (deleted/cross-workspace
// row) — the chain must still fall back to workspace level rather than
// propagate the error or return an empty prefix.
func TestPrefixForIssue_FallsBackToWorkspaceWhenSpaceLookupFails(t *testing.T) {
	wsID := newUUID(t)
	danglingSpaceID := newUUID(t)
	f := newFakeQueries()
	f.defaultSpaces[wsID.Bytes] = db.WorkspaceSpace{Key: "DEFT"}
	// danglingSpaceID is intentionally absent from f.spaces.

	issue := db.Issue{SpaceID: danglingSpaceID, WorkspaceID: wsID}
	got := PrefixForIssue(context.Background(), f, issue)
	if got != "DEFT" {
		t.Errorf("PrefixForIssue() = %q, want %q (default space key) when Space lookup fails", got, "DEFT")
	}
}

// TestPrefixForIssue_FallsBackToWorkspaceWhenSpaceKeyEmpty covers a Space row
// that resolves successfully but has an empty key (defensive against bad
// data), which must be treated the same as "not found".
func TestPrefixForIssue_FallsBackToWorkspaceWhenSpaceKeyEmpty(t *testing.T) {
	wsID := newUUID(t)
	spaceID := newUUID(t)
	f := newFakeQueries()
	f.spaces[spaceID.Bytes] = db.WorkspaceSpace{ID: spaceID, WorkspaceID: wsID, Key: ""}
	f.defaultSpaces[wsID.Bytes] = db.WorkspaceSpace{Key: "DEFT"}

	issue := db.Issue{SpaceID: spaceID, WorkspaceID: wsID}
	got := PrefixForIssue(context.Background(), f, issue)
	if got != "DEFT" {
		t.Errorf("PrefixForIssue() = %q, want %q (default space key) when Space key is empty", got, "DEFT")
	}
}

// TestPrefixForWorkspace_UsesDefaultSpaceKeyWhenFound is step 2 in isolation.
func TestPrefixForWorkspace_UsesDefaultSpaceKeyWhenFound(t *testing.T) {
	wsID := newUUID(t)
	f := newFakeQueries()
	f.defaultSpaces[wsID.Bytes] = db.WorkspaceSpace{Key: "DEFT"}
	f.workspaces[wsID.Bytes] = db.Workspace{ID: wsID, Name: "Ignored Workspace", IssuePrefix: "IGN"}

	got := PrefixForWorkspace(context.Background(), f, wsID)
	if got != "DEFT" {
		t.Errorf("PrefixForWorkspace() = %q, want %q (default space key)", got, "DEFT")
	}
}

// TestPrefixForWorkspace_FallsBackToLegacyIssuePrefixWhenNoDefaultSpace is
// step 3: no default Space exists (e.g. pre-migration workspace), so the
// legacy workspace.issue_prefix compatibility column is used.
func TestPrefixForWorkspace_FallsBackToLegacyIssuePrefixWhenNoDefaultSpace(t *testing.T) {
	wsID := newUUID(t)
	f := newFakeQueries()
	f.workspaces[wsID.Bytes] = db.Workspace{ID: wsID, Name: "Ignored Name", IssuePrefix: "LEGACY"}

	got := PrefixForWorkspace(context.Background(), f, wsID)
	if got != "LEGACY" {
		t.Errorf("PrefixForWorkspace() = %q, want %q (legacy issue_prefix)", got, "LEGACY")
	}
}

// TestPrefixForWorkspace_FallsBackToGeneratePrefixWhenNoIssuePrefix is step 4,
// the fallback-of-last-resort: no default Space and no legacy issue_prefix.
func TestPrefixForWorkspace_FallsBackToGeneratePrefixWhenNoIssuePrefix(t *testing.T) {
	wsID := newUUID(t)
	f := newFakeQueries()
	f.workspaces[wsID.Bytes] = db.Workspace{ID: wsID, Name: "Jiayuan's Workspace", IssuePrefix: ""}

	got := PrefixForWorkspace(context.Background(), f, wsID)
	want := GeneratePrefix("Jiayuan's Workspace")
	if got != want {
		t.Errorf("PrefixForWorkspace() = %q, want %q (GeneratePrefix fallback)", got, want)
	}
}

// TestPrefixForWorkspace_ReturnsEmptyWhenWorkspaceLookupFails documents the
// one case where the chain gives up: no default Space AND the workspace row
// itself cannot be read (e.g. deleted workspace). Callers must be defensive
// about an empty prefix rather than the chain fabricating one.
func TestPrefixForWorkspace_ReturnsEmptyWhenWorkspaceLookupFails(t *testing.T) {
	wsID := newUUID(t) // absent from both f.defaultSpaces and f.workspaces
	f := newFakeQueries()

	got := PrefixForWorkspace(context.Background(), f, wsID)
	if got != "" {
		t.Errorf("PrefixForWorkspace() = %q, want empty string when workspace lookup also fails", got)
	}
}

// TestForIssue_FormatsAsPrefixDashNumber pins the "PREFIX-NUMBER" identifier
// shape across both a Space-key resolution and a fully-fallen-back
// GeneratePrefix resolution, so an issue whose Space/workspace lookups all
// fail still never renders as a bare "-42" or "#42".
func TestForIssue_FormatsAsPrefixDashNumber(t *testing.T) {
	identifierShape := regexp.MustCompile(`^[A-Z0-9]+-[0-9]+$`)

	t.Run("space key resolves", func(t *testing.T) {
		wsID := newUUID(t)
		spaceID := newUUID(t)
		f := newFakeQueries()
		f.spaces[spaceID.Bytes] = db.WorkspaceSpace{ID: spaceID, WorkspaceID: wsID, Key: "ACME"}
		issue := db.Issue{SpaceID: spaceID, WorkspaceID: wsID, Number: 42}

		got := ForIssue(context.Background(), f, issue)
		if got != "ACME-42" {
			t.Errorf("ForIssue() = %q, want %q", got, "ACME-42")
		}
		if !identifierShape.MatchString(got) {
			t.Errorf("ForIssue() = %q does not match PREFIX-NUMBER shape", got)
		}
	})

	t.Run("every lookup fails, still no bare -42 or #42", func(t *testing.T) {
		wsID := newUUID(t)
		danglingSpaceID := newUUID(t)
		f := newFakeQueries()
		f.workspaces[wsID.Bytes] = db.Workspace{ID: wsID, Name: "Jiayuan's Workspace"}
		issue := db.Issue{SpaceID: danglingSpaceID, WorkspaceID: wsID, Number: 42}

		got := ForIssue(context.Background(), f, issue)
		want := GeneratePrefix("Jiayuan's Workspace") + "-42"
		if got != want {
			t.Errorf("ForIssue() = %q, want %q", got, want)
		}
		if got == "-42" || got == "#42" {
			t.Errorf("ForIssue() = %q, must never be a bare number identifier", got)
		}
		if !identifierShape.MatchString(got) {
			t.Errorf("ForIssue() = %q does not match PREFIX-NUMBER shape", got)
		}
	})
}

// TestResolver_PrefixForIssue_CachesSpaceKeyLookupAcrossCalls asserts the
// memoization contract stated in the Resolver doc comment: resolving two
// issues that share a Space must only call GetWorkspaceSpace once.
func TestResolver_PrefixForIssue_CachesSpaceKeyLookupAcrossCalls(t *testing.T) {
	wsID := newUUID(t)
	spaceID := newUUID(t)
	f := newFakeQueries()
	f.spaces[spaceID.Bytes] = db.WorkspaceSpace{ID: spaceID, WorkspaceID: wsID, Key: "ACME"}
	r := NewResolver(f)

	issue1 := db.Issue{SpaceID: spaceID, WorkspaceID: wsID, Number: 1}
	issue2 := db.Issue{SpaceID: spaceID, WorkspaceID: wsID, Number: 2}

	if got := r.PrefixForIssue(context.Background(), issue1); got != "ACME" {
		t.Fatalf("first PrefixForIssue() = %q, want %q", got, "ACME")
	}
	if got := r.PrefixForIssue(context.Background(), issue2); got != "ACME" {
		t.Fatalf("second PrefixForIssue() = %q, want %q", got, "ACME")
	}
	if f.spaceCalls != 1 {
		t.Errorf("GetWorkspaceSpace called %d times for two issues sharing a Space, want 1 (memoized)", f.spaceCalls)
	}
}

// TestResolver_PrefixForIssue_CachesWorkspaceFallbackAcrossCalls covers
// memoization of the workspace-level fallback path (no Space on either
// issue): GetDefaultWorkspaceSpace must only be called once per workspace.
func TestResolver_PrefixForIssue_CachesWorkspaceFallbackAcrossCalls(t *testing.T) {
	wsID := newUUID(t)
	f := newFakeQueries()
	f.defaultSpaces[wsID.Bytes] = db.WorkspaceSpace{Key: "DEFT"}
	r := NewResolver(f)

	issue1 := db.Issue{WorkspaceID: wsID, Number: 1}
	issue2 := db.Issue{WorkspaceID: wsID, Number: 2}

	if got := r.PrefixForIssue(context.Background(), issue1); got != "DEFT" {
		t.Fatalf("first PrefixForIssue() = %q, want %q", got, "DEFT")
	}
	if got := r.PrefixForIssue(context.Background(), issue2); got != "DEFT" {
		t.Fatalf("second PrefixForIssue() = %q, want %q", got, "DEFT")
	}
	if f.defaultCalls != 1 {
		t.Errorf("GetDefaultWorkspaceSpace called %d times for two issues sharing a workspace, want 1 (memoized)", f.defaultCalls)
	}
}

// TestResolver_CachesFailedSpaceLookupToAvoidRepeatedQueries covers negative
// caching: a Space ID that fails to resolve is cached as "" so a batch of
// issues all pointing at the same dangling Space does not re-issue the failed
// query per issue.
func TestResolver_CachesFailedSpaceLookupToAvoidRepeatedQueries(t *testing.T) {
	wsID := newUUID(t)
	danglingSpaceID := newUUID(t)
	f := newFakeQueries()
	f.defaultSpaces[wsID.Bytes] = db.WorkspaceSpace{Key: "DEFT"}
	r := NewResolver(f)

	issue1 := db.Issue{SpaceID: danglingSpaceID, WorkspaceID: wsID, Number: 1}
	issue2 := db.Issue{SpaceID: danglingSpaceID, WorkspaceID: wsID, Number: 2}

	if got := r.PrefixForIssue(context.Background(), issue1); got != "DEFT" {
		t.Fatalf("first PrefixForIssue() = %q, want %q", got, "DEFT")
	}
	if got := r.PrefixForIssue(context.Background(), issue2); got != "DEFT" {
		t.Fatalf("second PrefixForIssue() = %q, want %q", got, "DEFT")
	}
	if f.spaceCalls != 1 {
		t.Errorf("GetWorkspaceSpace called %d times for two issues sharing a dangling Space, want 1 (negative-cached)", f.spaceCalls)
	}
}
