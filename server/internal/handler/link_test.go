package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// --- Helpers ---------------------------------------------------------------

// seedLinkWorkspace creates a workspace + owner user with a unique issue
// prefix, returning ids. Cleanup is registered with t.
func seedLinkWorkspace(t *testing.T, prefix string) (workspaceID, userID string) {
	t.Helper()
	if testPool == nil {
		t.Skip("database not reachable")
	}
	ctx := context.Background()
	userID = uuid.NewString()
	workspaceID = uuid.NewString()
	slug := "link-" + uuid.NewString()[:8]

	if _, err := testPool.Exec(ctx,
		`INSERT INTO "user" (id, name, email) VALUES ($1, 'Link Test', $2)`,
		userID, fmt.Sprintf("link-test-%s@multica.ai", userID[:8])); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO workspace (id, slug, name, issue_prefix, description) VALUES ($1, $2, 'Link Tests', $3, '')`,
		workspaceID, slug, prefix); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return
}

// seedLinkIssue inserts an issue with a sequential number scoped to the
// workspace (so identifiers are deterministic in tests).
func seedLinkIssue(t *testing.T, workspaceID, userID string, number int32, title, status string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, $2, $3, 'medium', 'member', $4, $5) RETURNING id`,
		workspaceID, title, status, userID, number,
	).Scan(&id); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return id
}

// linkReq builds an authenticated link API request.
func linkReq(method, path, userID, workspaceID string, body any, params ...string) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
	if workspaceID != "" {
		req.Header.Set("X-Workspace-ID", workspaceID)
	}
	if len(params) > 0 {
		req = withURLParams(req, params...)
	}
	return req
}

// --- Tests -----------------------------------------------------------------

// 1) Happy path covers all four link types in a single test to keep CI fast.
func TestCreateIssueLink_HappyPath_AllTypes(t *testing.T) {
	wsID, userID := seedLinkWorkspace(t, "LNK")
	a := seedLinkIssue(t, wsID, userID, 1, "A", "todo")
	b := seedLinkIssue(t, wsID, userID, 2, "B", "todo")
	c := seedLinkIssue(t, wsID, userID, 3, "C", "todo")
	d := seedLinkIssue(t, wsID, userID, 4, "D", "todo")

	cases := []struct {
		name     string
		source   string
		target   string
		linkType string
	}{
		{"blocks", a, b, "blocks"},
		{"depends_on", a, c, "depends_on"},
		{"duplicates", a, d, "duplicates"},
		{"relates_to", b, c, "relates_to"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := linkReq("POST",
				fmt.Sprintf("/api/issues/%s/links", tc.source),
				userID, wsID,
				map[string]string{"target_issue_id": tc.target, "link_type": tc.linkType},
				"id", tc.source,
			)
			testHandler.CreateIssueLink(w, req)
			if w.Code != http.StatusCreated {
				t.Fatalf("%s: want 201, got %d %s", tc.name, w.Code, w.Body.String())
			}
		})
	}

	// Both directions must be visible from each side.
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM issue_link WHERE source_issue_id = $1`, a).Scan(&n); err != nil {
		t.Fatalf("count A's links: %v", err)
	}
	if n != 3 {
		t.Fatalf("A should have 3 outgoing-from-A rows; got %d", n)
	}
	// Mirror rows: B/C/D each have one incoming-from-A row.
	for _, peer := range []string{b, c, d} {
		if err := testPool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM issue_link WHERE source_issue_id = $1`, peer).Scan(&n); err != nil {
			t.Fatalf("count peer links: %v", err)
		}
		if n < 1 {
			t.Fatalf("peer %s missing mirror row", peer)
		}
	}
}

func TestCreateIssueLink_RejectSelfLink(t *testing.T) {
	wsID, userID := seedLinkWorkspace(t, "SLF")
	a := seedLinkIssue(t, wsID, userID, 1, "A", "todo")

	w := httptest.NewRecorder()
	req := linkReq("POST",
		fmt.Sprintf("/api/issues/%s/links", a),
		userID, wsID,
		map[string]string{"target_issue_id": a, "link_type": "relates_to"},
		"id", a,
	)
	testHandler.CreateIssueLink(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("self-link should be 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestCreateIssueLink_RejectInvalidLinkType(t *testing.T) {
	wsID, userID := seedLinkWorkspace(t, "INV")
	a := seedLinkIssue(t, wsID, userID, 1, "A", "todo")
	b := seedLinkIssue(t, wsID, userID, 2, "B", "todo")

	w := httptest.NewRecorder()
	req := linkReq("POST",
		fmt.Sprintf("/api/issues/%s/links", a),
		userID, wsID,
		map[string]string{"target_issue_id": b, "link_type": "totally-bogus"},
		"id", a,
	)
	testHandler.CreateIssueLink(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid link_type should be 400, got %d", w.Code)
	}
}

func TestCreateIssueLink_RejectDuplicate(t *testing.T) {
	wsID, userID := seedLinkWorkspace(t, "DUP")
	a := seedLinkIssue(t, wsID, userID, 1, "A", "todo")
	b := seedLinkIssue(t, wsID, userID, 2, "B", "todo")

	body := map[string]string{"target_issue_id": b, "link_type": "blocks"}
	mk := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := linkReq("POST",
			fmt.Sprintf("/api/issues/%s/links", a),
			userID, wsID, body, "id", a)
		testHandler.CreateIssueLink(w, req)
		return w
	}
	if w := mk(); w.Code != http.StatusCreated {
		t.Fatalf("first create should be 201, got %d %s", w.Code, w.Body.String())
	}
	if w := mk(); w.Code != http.StatusConflict {
		t.Fatalf("duplicate create should be 409, got %d %s", w.Code, w.Body.String())
	}
}

// Cycle: A blocks B, B blocks C, then we try C blocks A → must be 409.
func TestCreateIssueLink_RejectBlocksCycle(t *testing.T) {
	wsID, userID := seedLinkWorkspace(t, "CYC")
	a := seedLinkIssue(t, wsID, userID, 1, "A", "todo")
	b := seedLinkIssue(t, wsID, userID, 2, "B", "todo")
	c := seedLinkIssue(t, wsID, userID, 3, "C", "todo")

	post := func(src, tgt string) int {
		w := httptest.NewRecorder()
		req := linkReq("POST",
			fmt.Sprintf("/api/issues/%s/links", src),
			userID, wsID,
			map[string]string{"target_issue_id": tgt, "link_type": "blocks"},
			"id", src,
		)
		testHandler.CreateIssueLink(w, req)
		return w.Code
	}
	if got := post(a, b); got != http.StatusCreated {
		t.Fatalf("A blocks B: %d", got)
	}
	if got := post(b, c); got != http.StatusCreated {
		t.Fatalf("B blocks C: %d", got)
	}
	if got := post(c, a); got != http.StatusConflict {
		t.Fatalf("C blocks A (cycle) should be 409; got %d", got)
	}
}

func TestCreateIssueLink_CrossWorkspace(t *testing.T) {
	wsA, userA := seedLinkWorkspace(t, "WSA")
	wsB, userB := seedLinkWorkspace(t, "WSB")
	a := seedLinkIssue(t, wsA, userA, 1, "A", "todo")
	b := seedLinkIssue(t, wsB, userB, 1, "B", "todo")

	w := httptest.NewRecorder()
	req := linkReq("POST",
		fmt.Sprintf("/api/issues/%s/links", a),
		userA, wsA,
		map[string]string{"target_issue_id": b, "link_type": "relates_to"},
		"id", a,
	)
	testHandler.CreateIssueLink(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("cross-workspace link should succeed; got %d %s", w.Code, w.Body.String())
	}

	// userA can list links from A even though target lives in wsB.
	wl := httptest.NewRecorder()
	rl := linkReq("GET",
		fmt.Sprintf("/api/issues/%s/links", a),
		userA, wsA, nil, "id", a)
	testHandler.ListIssueLinks(wl, rl)
	if wl.Code != http.StatusOK {
		t.Fatalf("list links: %d", wl.Code)
	}
	var links []map[string]any
	if err := json.Unmarshal(wl.Body.Bytes(), &links); err != nil {
		t.Fatalf("unmarshal links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("want 1 link from A, got %d", len(links))
	}
	if got := links[0]["target_workspace_id"]; got != wsB {
		t.Fatalf("target_workspace_id: want %s, got %v", wsB, got)
	}
}

func TestDeleteIssueLink_RemovesBothMirrors(t *testing.T) {
	wsID, userID := seedLinkWorkspace(t, "DEL")
	a := seedLinkIssue(t, wsID, userID, 1, "A", "todo")
	b := seedLinkIssue(t, wsID, userID, 2, "B", "todo")

	// Create.
	w := httptest.NewRecorder()
	req := linkReq("POST",
		fmt.Sprintf("/api/issues/%s/links", a),
		userID, wsID,
		map[string]string{"target_issue_id": b, "link_type": "duplicates"},
		"id", a,
	)
	testHandler.CreateIssueLink(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d", w.Code)
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created: %v", err)
	}
	linkID := created["id"].(string)

	// Delete via A.
	wd := httptest.NewRecorder()
	rd := linkReq("DELETE",
		fmt.Sprintf("/api/issues/%s/links/%s", a, linkID),
		userID, wsID, nil, "id", a, "linkId", linkID)
	testHandler.DeleteIssueLink(wd, rd)
	if wd.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", wd.Code, wd.Body.String())
	}

	// Both mirror rows must be gone.
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM issue_link WHERE source_issue_id IN ($1,$2)`,
		a, b).Scan(&n); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 link rows after delete; got %d", n)
	}
}

func TestListBlockers_OnlyOpen(t *testing.T) {
	wsID, userID := seedLinkWorkspace(t, "BLK")
	target := seedLinkIssue(t, wsID, userID, 1, "Target", "todo")
	blockerOpen := seedLinkIssue(t, wsID, userID, 2, "Open Blocker", "in_progress")
	blockerDone := seedLinkIssue(t, wsID, userID, 3, "Done Blocker", "done")

	mk := func(src, tgt string) {
		w := httptest.NewRecorder()
		req := linkReq("POST",
			fmt.Sprintf("/api/issues/%s/links", src),
			userID, wsID,
			map[string]string{"target_issue_id": tgt, "link_type": "blocks"},
			"id", src,
		)
		testHandler.CreateIssueLink(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
	}
	mk(blockerOpen, target)
	mk(blockerDone, target)

	// Blockers for target — only the open one.
	w := httptest.NewRecorder()
	req := linkReq("GET",
		fmt.Sprintf("/api/issues/%s/blockers", target),
		userID, wsID, nil, "id", target)
	testHandler.ListIssueBlockers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("blockers: %d %s", w.Code, w.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 open blocker; got %d (%s)", len(out), w.Body.String())
	}
	if got := out[0]["blocker_issue_id"]; got != blockerOpen {
		t.Fatalf("blocker id: want %s; got %v", blockerOpen, got)
	}
}

// Verifies that creating and deleting a link writes activity_log rows on
// BOTH the source and target issues with the correct action + JSON details.
// The frontend's formatActivity branches off these rows.
func TestActivityRowsInsertedOnLinkCreateAndDelete(t *testing.T) {
	wsID, userID := seedLinkWorkspace(t, "ACT")
	a := seedLinkIssue(t, wsID, userID, 1, "Source", "todo")
	b := seedLinkIssue(t, wsID, userID, 2, "Target", "todo")

	// --- CREATE ------------------------------------------------------------
	w := httptest.NewRecorder()
	req := linkReq("POST",
		fmt.Sprintf("/api/issues/%s/links", a),
		userID, wsID,
		map[string]string{"target_issue_id": b, "link_type": "blocks"},
		"id", a,
	)
	testHandler.CreateIssueLink(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create link: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created: %v", err)
	}
	linkID := created["id"].(string)

	// Inspect activity rows for both issues.
	type actRow struct {
		issueID  string
		action   string
		details  []byte
	}
	rows := func(action string) []actRow {
		t.Helper()
		r, err := testPool.Query(context.Background(),
			`SELECT issue_id::text, action, details
			   FROM activity_log
			  WHERE issue_id IN ($1,$2) AND action = $3
			  ORDER BY created_at`,
			a, b, action)
		if err != nil {
			t.Fatalf("query activity (%s): %v", action, err)
		}
		defer r.Close()
		var out []actRow
		for r.Next() {
			var row actRow
			if err := r.Scan(&row.issueID, &row.action, &row.details); err != nil {
				t.Fatalf("scan activity: %v", err)
			}
			out = append(out, row)
		}
		return out
	}

	added := rows("link_added")
	if len(added) != 2 {
		t.Fatalf("link_added: want 2 rows (source+target); got %d", len(added))
	}

	// Index rows by issue id for direction-aware assertions.
	bySrc, byTgt := map[string][]byte{}, map[string][]byte{}
	for _, row := range added {
		switch row.issueID {
		case a:
			bySrc["link_added"] = row.details
		case b:
			byTgt["link_added"] = row.details
		default:
			t.Fatalf("unexpected issue_id %s in activity row", row.issueID)
		}
	}
	if bySrc["link_added"] == nil || byTgt["link_added"] == nil {
		t.Fatalf("missing source or target activity row: src=%v tgt=%v",
			bySrc["link_added"], byTgt["link_added"])
	}

	srcDetails := map[string]any{}
	tgtDetails := map[string]any{}
	if err := json.Unmarshal(bySrc["link_added"], &srcDetails); err != nil {
		t.Fatalf("unmarshal src details: %v", err)
	}
	if err := json.Unmarshal(byTgt["link_added"], &tgtDetails); err != nil {
		t.Fatalf("unmarshal tgt details: %v", err)
	}

	// Source row: outgoing, link_type=blocks, points at target.
	if got := srcDetails["link_type"]; got != "blocks" {
		t.Fatalf("src link_type: want blocks; got %v", got)
	}
	if got := srcDetails["direction"]; got != "outgoing" {
		t.Fatalf("src direction: want outgoing; got %v", got)
	}
	if got := srcDetails["target_issue_id"]; got != b {
		t.Fatalf("src target_issue_id: want %s; got %v", b, got)
	}
	if _, ok := srcDetails["target_identifier"].(string); !ok {
		t.Fatalf("src target_identifier missing or not a string: %v", srcDetails["target_identifier"])
	}

	// Target row: incoming, points back at source.
	if got := tgtDetails["link_type"]; got != "blocks" {
		t.Fatalf("tgt link_type: want blocks; got %v", got)
	}
	if got := tgtDetails["direction"]; got != "incoming" {
		t.Fatalf("tgt direction: want incoming; got %v", got)
	}
	if got := tgtDetails["target_issue_id"]; got != a {
		t.Fatalf("tgt target_issue_id: want %s; got %v", a, got)
	}

	// --- DELETE ------------------------------------------------------------
	wd := httptest.NewRecorder()
	rd := linkReq("DELETE",
		fmt.Sprintf("/api/issues/%s/links/%s", a, linkID),
		userID, wsID, nil, "id", a, "linkId", linkID)
	testHandler.DeleteIssueLink(wd, rd)
	if wd.Code != http.StatusNoContent {
		t.Fatalf("delete link: %d %s", wd.Code, wd.Body.String())
	}

	removed := rows("link_removed")
	if len(removed) != 2 {
		t.Fatalf("link_removed: want 2 rows (source+target); got %d", len(removed))
	}
	sawSrc, sawTgt := false, false
	for _, row := range removed {
		var d map[string]any
		if err := json.Unmarshal(row.details, &d); err != nil {
			t.Fatalf("unmarshal removed details: %v", err)
		}
		if d["link_type"] != "blocks" {
			t.Fatalf("removed link_type: want blocks; got %v", d["link_type"])
		}
		switch row.issueID {
		case a:
			sawSrc = true
			if d["direction"] != "outgoing" {
				t.Fatalf("src removed direction: want outgoing; got %v", d["direction"])
			}
			if d["target_issue_id"] != b {
				t.Fatalf("src removed target: want %s; got %v", b, d["target_issue_id"])
			}
			if _, ok := d["target_identifier"].(string); !ok || d["target_identifier"] == "" {
				t.Fatalf("src removed missing target_identifier: %v", d["target_identifier"])
			}
		case b:
			sawTgt = true
			if d["direction"] != "incoming" {
				t.Fatalf("tgt removed direction: want incoming; got %v", d["direction"])
			}
			if d["target_issue_id"] != a {
				t.Fatalf("tgt removed target: want %s; got %v", a, d["target_issue_id"])
			}
			if _, ok := d["target_identifier"].(string); !ok || d["target_identifier"] == "" {
				t.Fatalf("tgt removed missing target_identifier: %v", d["target_identifier"])
			}
		}
	}
	if !sawSrc || !sawTgt {
		t.Fatalf("missing removed row: src=%v tgt=%v", sawSrc, sawTgt)
	}
}
