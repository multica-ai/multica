package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestUpdateSpaceKeyWithIssuesWritesAliases verifies that renaming a space's
// identifier is allowed even after issues exist, and that every existing
// OLDKEY-N keeps resolving through the recorded aliases.
func TestUpdateSpaceKeyWithIssuesWritesAliases(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano() % 100000
	oldKey := fmt.Sprintf("OK%05d", suffix)
	newKey := fmt.Sprintf("NK%05d", suffix)

	var spaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, issue_counter, created_by)
		VALUES ($1, $2, $3, 2, $4)
		RETURNING id
	`, testWorkspaceID, "Rename Space", oldKey, testUserID).Scan(&spaceID); err != nil {
		t.Fatalf("insert space: %v", err)
	}

	issueIDs := make([]string, 0, 2)
	issueNumbers := []int32{1, 2}
	for _, n := range issueNumbers {
		var issueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, space_id, title, status, priority,
				creator_type, creator_id, number, position
			)
			VALUES ($1, $2, $3, 'todo', 'none', 'member', $4, $5, 0)
			RETURNING id
		`, testWorkspaceID, spaceID, fmt.Sprintf("rename issue %d", n), testUserID, n).Scan(&issueID); err != nil {
			t.Fatalf("insert issue %d: %v", n, err)
		}
		issueIDs = append(issueIDs, issueID)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_identifier_alias WHERE issue_id = ANY($1)`, issueIDs)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE space_id = $1`, spaceID)
		testPool.Exec(context.Background(), `DELETE FROM workspace_space WHERE id = $1`, spaceID)
	})

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/spaces/"+spaceID, map[string]any{"key": newKey})
	req = withURLParam(req, "id", spaceID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: "owner"}))
	testHandler.UpdateSpace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSpace: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated SpaceResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if updated.Key != newKey {
		t.Fatalf("space key = %q, want %q", updated.Key, newKey)
	}

	// Every pre-rename OLDKEY-N must still resolve to its issue via the alias.
	for i, n := range issueNumbers {
		got, err := testHandler.Queries.GetIssueByIdentifierAlias(ctx, db.GetIssueByIdentifierAliasParams{
			WorkspaceID:   parseUUID(testWorkspaceID),
			SpaceKeyLower: fmt.Sprintf("ok%05d", suffix),
			Number:        n,
		})
		if err != nil {
			t.Fatalf("alias for %s-%d did not resolve: %v", oldKey, n, err)
		}
		if uuidToString(got.ID) != issueIDs[i] {
			t.Fatalf("alias %s-%d resolved to %q, want %q", oldKey, n, uuidToString(got.ID), issueIDs[i])
		}
	}
}
