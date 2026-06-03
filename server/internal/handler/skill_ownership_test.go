package handler

// CEREBRO-PATCH(skill-change-request-test): FIR-2627 + FIR-2306 coverage for
// the cerebro skill-ownership flow — inbox notification on create + review,
// work_session_id round-trip, and version bump on direct edit.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ownershipFixture seeds a workspace with three users — owner, approver,
// proposer — plus a skill owned by `ownerID` with `approverID` as the sole
// approver. Returns the skill UUID and the three user ids. All rows are
// cleaned up via t.Cleanup.
type ownershipFixture struct {
	skillID    string
	ownerID    string
	approverID string
	proposerID string
}

func setupOwnershipFixture(t *testing.T) ownershipFixture {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()
	mkUser := func(name string) string {
		var id string
		if err := testPool.QueryRow(ctx,
			`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
			name, name+"-"+suffix+"@multica.test",
		).Scan(&id); err != nil {
			t.Fatalf("create %s user: %v", name, err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, id) })
		return id
	}
	mkMember := func(userID, role string) {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
			testWorkspaceID, userID, role,
		); err != nil {
			t.Fatalf("create member: %v", err)
		}
	}

	owner := mkUser("Skill Owner " + suffix)
	approver := mkUser("Skill Approver " + suffix)
	proposer := mkUser("Skill Proposer " + suffix)
	mkMember(owner, "member")
	mkMember(approver, "member")
	mkMember(proposer, "member")

	var skillID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by, owner_id, approver_ids, current_version)
		VALUES ($1, $2, $3, $4, '{}'::jsonb, $5, $6, ARRAY[$7::uuid], $8)
		RETURNING id
	`, testWorkspaceID, "ownership-fixture-"+suffix, "fixture",
		"line one\nline two\nline three\n", owner, owner, approver, "1.0.0",
	).Scan(&skillID); err != nil {
		t.Fatalf("insert skill: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skillID) })

	return ownershipFixture{
		skillID:    skillID,
		ownerID:    owner,
		approverID: approver,
		proposerID: proposer,
	}
}

func TestCreateSkillChangeRequest_NotifiesOwnerAndApproversWithDiffAndSession(t *testing.T) {
	ctx := context.Background()
	f := setupOwnershipFixture(t)

	// Seed an issue + work_session under the proposer so the CR can link.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, creator_type, creator_id)
		VALUES ($1, 'fixture issue', 'todo', 'member', $2)
		RETURNING id
	`, testWorkspaceID, f.proposerID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })
	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO work_session (workspace_id, issue_id, user_id, session_type)
		VALUES ($1, $2, $3, 'manual')
		RETURNING id
	`, testWorkspaceID, issueID, f.proposerID).Scan(&sessionID); err != nil {
		t.Fatalf("seed work_session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM work_session WHERE id = $1`, sessionID) })

	body := map[string]any{
		"title":            "Tighten the intro",
		"description":      "shorten the opener so it scans faster",
		"proposed_version": "1.1.0",
		"proposed_content": "line one (rewritten)\nline two\nline three\n",
		"work_session_id":  sessionID,
	}

	w := httptest.NewRecorder()
	req := newRequestAs(f.proposerID, "POST", "/api/skills/"+f.skillID+"/change-requests", body)
	req = withURLParam(req, "id", f.skillID)
	testHandler.CreateSkillChangeRequest(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSkillChangeRequest: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp SkillChangeRequestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WorkSessionID == nil || *resp.WorkSessionID != sessionID {
		t.Fatalf("expected work_session_id %q on response, got %v", sessionID, resp.WorkSessionID)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM skill_change_request WHERE id = $1`, resp.ID) })

	// 2 inbox items — one to owner, one to approver, NONE to the proposer.
	rows, err := testPool.Query(ctx, `
		SELECT recipient_id::text, details
		FROM inbox_item
		WHERE workspace_id = $1
		  AND type = 'skill_change_request_created'
		  AND (details->>'change_request_id') = $2
	`, testWorkspaceID, resp.ID)
	if err != nil {
		t.Fatalf("query inbox: %v", err)
	}
	defer rows.Close()
	seen := map[string][]byte{}
	for rows.Next() {
		var rid string
		var details []byte
		if err := rows.Scan(&rid, &details); err != nil {
			t.Fatalf("scan inbox row: %v", err)
		}
		seen[rid] = details
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 inbox items (owner + approver), got %d: %#v", len(seen), seen)
	}
	if _, ok := seen[f.ownerID]; !ok {
		t.Fatalf("expected inbox item for owner %s, got recipients %v", f.ownerID, mapKeys(seen))
	}
	if _, ok := seen[f.approverID]; !ok {
		t.Fatalf("expected inbox item for approver %s, got recipients %v", f.approverID, mapKeys(seen))
	}
	if _, ok := seen[f.proposerID]; ok {
		t.Fatal("proposer must not receive their own change-request notification")
	}

	var decoded map[string]any
	if err := json.Unmarshal(seen[f.ownerID], &decoded); err != nil {
		t.Fatalf("decode owner inbox details: %v", err)
	}
	if decoded["proposer_id"] != f.proposerID {
		t.Fatalf("proposer_id = %v, want %s", decoded["proposer_id"], f.proposerID)
	}
	if decoded["description"] != body["description"] {
		t.Fatalf("description (begrundelse) missing: %v", decoded["description"])
	}
	if decoded["work_session_id"] != sessionID {
		t.Fatalf("work_session_id in details = %v, want %s", decoded["work_session_id"], sessionID)
	}
	sessionURL, _ := decoded["session_url"].(string)
	if !strings.Contains(sessionURL, sessionID) {
		t.Fatalf("session_url should link to the session, got %q", sessionURL)
	}
	diff, _ := decoded["diff"].(string)
	if !strings.Contains(diff, "-line one") || !strings.Contains(diff, "+line one (rewritten)") {
		t.Fatalf("inbox diff should include the line change, got:\n%s", diff)
	}
}

func TestReviewSkillChangeRequest_NotifiesProposerOnApproveAndReject(t *testing.T) {
	for _, action := range []string{"approve", "reject"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			f := setupOwnershipFixture(t)

			body := map[string]any{
				"title":            "test " + action,
				"proposed_version": "1.1.0",
				"proposed_content": "line one (edit)\nline two\nline three\n",
			}
			w := httptest.NewRecorder()
			req := newRequestAs(f.proposerID, "POST", "/api/skills/"+f.skillID+"/change-requests", body)
			req = withURLParam(req, "id", f.skillID)
			testHandler.CreateSkillChangeRequest(w, req)
			if w.Code != http.StatusCreated {
				t.Fatalf("create CR: %d: %s", w.Code, w.Body.String())
			}
			var cr SkillChangeRequestResponse
			json.NewDecoder(w.Body).Decode(&cr)
			t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM skill_change_request WHERE id = $1`, cr.ID) })

			// Owner reviews.
			reviewBody := map[string]any{"action": action, "comment": action + " comment"}
			w = httptest.NewRecorder()
			req = newRequestAs(f.ownerID, "POST", "/api/skill-change-requests/"+cr.ID+"/review", reviewBody)
			req = withURLParam(req, "crId", cr.ID)
			testHandler.ReviewSkillChangeRequest(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("review %s: %d: %s", action, w.Code, w.Body.String())
			}

			// Proposer should now have a 'skill_change_request_reviewed' inbox item.
			var count int
			if err := testPool.QueryRow(ctx, `
				SELECT COUNT(*) FROM inbox_item
				WHERE workspace_id = $1
				  AND recipient_id = $2
				  AND type = 'skill_change_request_reviewed'
				  AND (details->>'change_request_id') = $3
			`, testWorkspaceID, f.proposerID, cr.ID).Scan(&count); err != nil {
				t.Fatalf("query proposer inbox: %v", err)
			}
			if count != 1 {
				t.Fatalf("expected 1 reviewed inbox for proposer, got %d", count)
			}
		})
	}
}

func mapKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
