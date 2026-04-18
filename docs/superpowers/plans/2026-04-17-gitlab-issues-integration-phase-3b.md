# GitLab Issues Integration — Phase 3b Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable write-through for issue CRUD on GitLab-connected workspaces — `PATCH /api/issues/{id}`, `DELETE /api/issues/{id}`, `POST /api/issues/batch-update`, `POST /api/issues/batch-delete`.

**Architecture:** Mirror the Phase 3a write-through pattern from `CreateIssue`: resolver picks actor-appropriate PAT → GitLab REST call → transactional cache upsert. For batch endpoints, loop per-item with **continue-on-error**: collect `{succeeded, failed}` and return HTTP 207 Multi-Status. PATCH translates status/priority/agent changes into GitLab label diffs (`add_labels`/`remove_labels`) plus `state_event` for close/reopen. Member-assignee writes stay cache-only in Phase 3b (no GitLab user mapping yet — Phase 4 territory); this matches Phase 3a's create behavior.

**Tech Stack:** Go 1.26, Chi router, pgx/v5, sqlc, httptest for fake GitLab server, standard `testing` package.

---

## Scope

**In scope for Phase 3b:**
- `PATCH /api/issues/{id}` — write-through UPDATE with label diffing
- `DELETE /api/issues/{id}` — write-through DELETE
- `POST /api/issues/batch-update` — continue-on-error loop, 207 Multi-Status
- `POST /api/issues/batch-delete` — continue-on-error loop, 207 Multi-Status
- GitLab REST client methods: `UpdateIssue`, `DeleteIssue`
- Translator function: `BuildUpdateIssueInput(old, req, agentSlugByUUID)` — label diff + state event

**Out of scope (deferred to 3c / 4 / 5):**
- Comments, subscriptions, reactions, `tasks cancel` unblock → Phase 3c
- Member-assignee → GitLab-user-ID mapping → Phase 4
- Backfill migration for pre-connection issues → never (fresh installation per Phase 0 design)
- Legacy code removal → Phase 5

## File Structure

**New files:**
- None — all logic extends existing files.

**Files to modify:**
| File | Responsibility |
|---|---|
| `server/pkg/gitlab/issues.go` | Add `UpdateIssueInput` struct + `UpdateIssue` method + `DeleteIssue` method |
| `server/pkg/gitlab/issues_test.go` | Tests for new client methods |
| `server/internal/gitlab/translator.go` | Add `UpdateIssueRequest` type + `BuildUpdateIssueInput` function |
| `server/internal/gitlab/translator_test.go` | Table-driven tests for label diffing + state event selection |
| `server/internal/handler/issue.go` | Add write-through branches to `UpdateIssue`, `DeleteIssue`, `BatchUpdateIssues`, `BatchDeleteIssues` handlers |
| `server/internal/handler/issue_test.go` | Write-through tests for all four handlers + batch partial-success |
| `server/cmd/server/router.go` | Remove `gw` middleware wrap from PATCH/DELETE/batch-update/batch-delete routes |
| `server/internal/handler/handler.go` | Add `BatchResult`/`BatchSucceeded`/`BatchFailed` response types (near existing `IssueResponse`) if not already present |

## Hard rules

1. **Write-through on connected workspaces is authoritative.** If the GitLab API call fails, return error (do NOT fall through to legacy direct-DB path — that would create cache rows orphaned from GitLab).
2. **Preserve bare-narg fields on `UpdateIssue`.** `assignee_type`, `assignee_id`, `due_date`, `parent_issue_id`, `project_id` are not COALESCE — passing zero pgtype values wipes them. Always pre-fill with current cache values unless the request explicitly changes the field.
3. **Use `ResolveTokenForWrite` for every outbound GitLab call.** Never hardcode the service PAT; the resolver handles the user-vs-service choice and fails loud on unknown actor types (Phase 3a M5).
4. **Continue-on-error for batch endpoints.** Each item's failure does NOT abort the batch. Collect failures with machine-readable `error_code` strings (`"GITLAB_404"`, `"GITLAB_403"`, `"NOT_FOUND"`, `"VALIDATION_FAILED"`) so clients can retry.
5. **Respect the clobber guard.** `UpsertIssueFromGitlab`'s `ON CONFLICT` check on `external_updated_at` prevents webhook → write-through races. Always pass the GitLab response's `updated_at` to the upsert.

---

## Task 1: GitLab Client — `UpdateIssue` REST method

**Files:**
- Modify: `server/pkg/gitlab/issues.go` (add type + method after existing `CreateIssue` around line 32)
- Test: `server/pkg/gitlab/issues_test.go` (add alongside existing `CreateIssue` tests)

- [ ] **Step 1: Write the failing test**

Add to `server/pkg/gitlab/issues_test.go`:

```go
func TestUpdateIssue_SendsPUTWithFields(t *testing.T) {
	var capturedPath, capturedMethod, capturedToken string
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":901,"iid":42,"title":"updated","state":"opened","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	newTitle := "updated"
	stateEvent := "close"
	input := UpdateIssueInput{
		Title:        &newTitle,
		AddLabels:    []string{"status::done"},
		RemoveLabels: []string{"status::in_progress"},
		StateEvent:   &stateEvent,
	}
	issue, err := c.UpdateIssue(context.Background(), "tok", 7, 42, input)
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/7/issues/42" {
		t.Errorf("path = %s, want /api/v4/projects/7/issues/42", capturedPath)
	}
	if capturedToken != "tok" {
		t.Errorf("token header = %s, want tok", capturedToken)
	}
	if capturedBody["title"] != "updated" {
		t.Errorf("body title = %v, want updated", capturedBody["title"])
	}
	if capturedBody["add_labels"] != "status::done" {
		t.Errorf("body add_labels = %v, want status::done", capturedBody["add_labels"])
	}
	if capturedBody["remove_labels"] != "status::in_progress" {
		t.Errorf("body remove_labels = %v, want status::in_progress", capturedBody["remove_labels"])
	}
	if capturedBody["state_event"] != "close" {
		t.Errorf("body state_event = %v, want close", capturedBody["state_event"])
	}
	if issue.IID != 42 || issue.Title != "updated" {
		t.Errorf("issue = %+v, want IID=42 title=updated", issue)
	}
}

func TestUpdateIssue_OmitsUnsetFields(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"iid":1,"state":"opened","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	newTitle := "only title"
	_, err := c.UpdateIssue(context.Background(), "tok", 1, 1, UpdateIssueInput{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if _, ok := capturedBody["description"]; ok {
		t.Errorf("description should be omitted, body = %+v", capturedBody)
	}
	if _, ok := capturedBody["add_labels"]; ok {
		t.Errorf("add_labels should be omitted when empty, body = %+v", capturedBody)
	}
	if _, ok := capturedBody["state_event"]; ok {
		t.Errorf("state_event should be omitted when nil, body = %+v", capturedBody)
	}
}

func TestUpdateIssue_PropagatesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.UpdateIssue(context.Background(), "tok", 1, 1, UpdateIssueInput{})
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want to contain '403'", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./pkg/gitlab/ -run TestUpdateIssue -v`
Expected: FAIL (UpdateIssue undefined).

- [ ] **Step 3: Add `UpdateIssueInput` struct + `UpdateIssue` method**

Add to `server/pkg/gitlab/issues.go` (right after `CreateIssue`, before `ListIssues`):

```go
// UpdateIssueInput mirrors GitLab's PUT /projects/:id/issues/:iid body.
// All fields are optional: omitted (nil / empty) means "do not touch".
//
// Labels use GitLab's additive/subtractive flags (add_labels / remove_labels)
// rather than the full-replacement "labels" field, so non-scoped labels the
// user has attached directly in GitLab survive a Multica-originated update.
type UpdateIssueInput struct {
	Title        *string  `json:"title,omitempty"`
	Description  *string  `json:"description,omitempty"`
	AddLabels    []string `json:"-"`
	RemoveLabels []string `json:"-"`
	AssigneeIDs  *[]int   `json:"assignee_ids,omitempty"`
	DueDate      *string  `json:"due_date,omitempty"`
	StateEvent   *string  `json:"state_event,omitempty"`
}

// UpdateIssue sends PUT /api/v4/projects/:id/issues/:iid. Comma-joins label
// slices so GitLab accepts them (the API expects comma-separated strings for
// add_labels / remove_labels, not arrays).
func (c *Client) UpdateIssue(ctx context.Context, token string, projectID int, iid int, in UpdateIssueInput) (*Issue, error) {
	payload := map[string]any{}
	if in.Title != nil {
		payload["title"] = *in.Title
	}
	if in.Description != nil {
		payload["description"] = *in.Description
	}
	if len(in.AddLabels) > 0 {
		payload["add_labels"] = strings.Join(in.AddLabels, ",")
	}
	if len(in.RemoveLabels) > 0 {
		payload["remove_labels"] = strings.Join(in.RemoveLabels, ",")
	}
	if in.AssigneeIDs != nil {
		payload["assignee_ids"] = *in.AssigneeIDs
	}
	if in.DueDate != nil {
		payload["due_date"] = *in.DueDate
	}
	if in.StateEvent != nil {
		payload["state_event"] = *in.StateEvent
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d", projectID, iid)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab update issue: %d %s", resp.StatusCode, string(b))
	}
	var issue Issue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, err
	}
	return &issue, nil
}
```

Confirm the imports at the top of the file include `bytes`, `context`, `encoding/json`, `fmt`, `io`, `net/http`, `strings`. If any are missing, add them.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd server && go test ./pkg/gitlab/ -run TestUpdateIssue -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add server/pkg/gitlab/issues.go server/pkg/gitlab/issues_test.go
git commit -m "feat(gitlab): UpdateIssue REST client method"
```

---

## Task 2: GitLab Client — `DeleteIssue` REST method

**Files:**
- Modify: `server/pkg/gitlab/issues.go` (add method after `UpdateIssue`)
- Test: `server/pkg/gitlab/issues_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestDeleteIssue_SendsDELETE(t *testing.T) {
	var capturedPath, capturedMethod, capturedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteIssue(context.Background(), "tok", 7, 42); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/7/issues/42" {
		t.Errorf("path = %s, want /api/v4/projects/7/issues/42", capturedPath)
	}
	if capturedToken != "tok" {
		t.Errorf("token header = %s, want tok", capturedToken)
	}
}

func TestDeleteIssue_404IsSuccessIdempotent(t *testing.T) {
	// GitLab returns 404 if the issue is already gone. For a delete, that's
	// the desired terminal state, so we treat it as success (idempotent).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteIssue(context.Background(), "tok", 7, 42); err != nil {
		t.Fatalf("DeleteIssue: expected 404 to be treated as success, got %v", err)
	}
}

func TestDeleteIssue_PropagatesNon2xxNon404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	err := c.DeleteIssue(context.Background(), "tok", 7, 42)
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want to contain '403'", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && go test ./pkg/gitlab/ -run TestDeleteIssue -v`
Expected: FAIL (DeleteIssue undefined).

- [ ] **Step 3: Add method**

```go
// DeleteIssue sends DELETE /api/v4/projects/:id/issues/:iid. Treats 404 as
// success (idempotent delete — if the issue is already gone, that's the
// desired state).
func (c *Client) DeleteIssue(ctx context.Context, token string, projectID int, iid int) error {
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d", projectID, iid)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitlab delete issue: %d %s", resp.StatusCode, string(b))
	}
	return nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd server && go test ./pkg/gitlab/ -run TestDeleteIssue -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add server/pkg/gitlab/issues.go server/pkg/gitlab/issues_test.go
git commit -m "feat(gitlab): DeleteIssue REST client method"
```

---

## Task 3: Translator — `BuildUpdateIssueInput` (label diffing + state event)

**Files:**
- Modify: `server/internal/gitlab/translator.go` (add types + function)
- Test: `server/internal/gitlab/translator_test.go`

This is the hairiest translator function in Phase 3b. It diffs the cached issue state against a PATCH request and emits add/remove label lists plus an optional `state_event`.

**Semantics:**
- Title/description/due-date: simple pass-through when present in the request.
- Status transition (e.g. `in_progress` → `done`): emit `remove_labels=["status::in_progress"]` + `add_labels=["status::done"]`. Additionally, if the new status is `done` or `cancelled` and the old was not, emit `state_event="close"`. If the new status is neither and the old was, emit `state_event="reopen"`.
- Priority transition: same label swap. Priority `"none"` means no label (so transitioning to `"none"` removes the old priority label without adding a new one).
- Agent assignee transition: remove `agent::<old-slug>` if present, add `agent::<new-slug>` if the new assignee is an agent. Transitioning to a member assignee or clearing the assignee just removes the old `agent::*` label (member-assignee writes are out of Phase 3b scope for GitLab; cache-only).

- [ ] **Step 1: Write the failing tests (table-driven)**

Add to `server/internal/gitlab/translator_test.go`:

```go
func TestBuildUpdateIssueInput(t *testing.T) {
	agentSlugByUUID := map[string]string{
		"11111111-1111-1111-1111-111111111111": "builder",
		"22222222-2222-2222-2222-222222222222": "reviewer",
	}

	statusClosed := "done"
	statusOpen := "in_progress"
	statusCancelled := "cancelled"
	prioHigh := "high"
	prioNone := "none"
	titleNew := "new title"
	descNew := "new desc"
	due := "2026-05-01"

	type oldSnap struct {
		status       string
		priority     string
		assigneeType string
		assigneeUUID string
	}
	cases := []struct {
		name          string
		old           oldSnap
		req           UpdateIssueRequest
		wantAddLabels []string
		wantRemove    []string
		wantTitle     *string
		wantDesc      *string
		wantDue       *string
		wantState     *string
	}{
		{
			name:          "title-only",
			old:           oldSnap{status: "todo", priority: "none"},
			req:           UpdateIssueRequest{Title: &titleNew},
			wantTitle:     &titleNew,
			wantAddLabels: nil,
			wantRemove:    nil,
		},
		{
			name:          "status transition in_progress → done closes",
			old:           oldSnap{status: "in_progress", priority: "none"},
			req:           UpdateIssueRequest{Status: &statusClosed},
			wantAddLabels: []string{"status::done"},
			wantRemove:    []string{"status::in_progress"},
			wantState:     ptr("close"),
		},
		{
			name:          "status transition done → in_progress reopens",
			old:           oldSnap{status: "done", priority: "none"},
			req:           UpdateIssueRequest{Status: &statusOpen},
			wantAddLabels: []string{"status::in_progress"},
			wantRemove:    []string{"status::done"},
			wantState:     ptr("reopen"),
		},
		{
			name:          "status cancelled closes",
			old:           oldSnap{status: "todo", priority: "none"},
			req:           UpdateIssueRequest{Status: &statusCancelled},
			wantAddLabels: []string{"status::cancelled"},
			wantRemove:    []string{"status::todo"},
			wantState:     ptr("close"),
		},
		{
			name:          "priority none → high",
			old:           oldSnap{status: "todo", priority: "none"},
			req:           UpdateIssueRequest{Priority: &prioHigh},
			wantAddLabels: []string{"priority::high"},
			wantRemove:    nil,
		},
		{
			name:          "priority high → none removes without adding",
			old:           oldSnap{status: "todo", priority: "high"},
			req:           UpdateIssueRequest{Priority: &prioNone},
			wantAddLabels: nil,
			wantRemove:    []string{"priority::high"},
		},
		{
			name:          "agent assignee change",
			old:           oldSnap{status: "todo", priority: "none", assigneeType: "agent", assigneeUUID: "11111111-1111-1111-1111-111111111111"},
			req:           UpdateIssueRequest{AssigneeType: ptr("agent"), AssigneeID: ptr("22222222-2222-2222-2222-222222222222")},
			wantAddLabels: []string{"agent::reviewer"},
			wantRemove:    []string{"agent::builder"},
		},
		{
			name:          "clear agent assignee",
			old:           oldSnap{status: "todo", priority: "none", assigneeType: "agent", assigneeUUID: "11111111-1111-1111-1111-111111111111"},
			req:           UpdateIssueRequest{AssigneeType: ptr(""), AssigneeID: ptr("")},
			wantAddLabels: nil,
			wantRemove:    []string{"agent::builder"},
		},
		{
			name:          "switch from agent to member removes agent label (member is cache-only)",
			old:           oldSnap{status: "todo", priority: "none", assigneeType: "agent", assigneeUUID: "11111111-1111-1111-1111-111111111111"},
			req:           UpdateIssueRequest{AssigneeType: ptr("member"), AssigneeID: ptr("99999999-9999-9999-9999-999999999999")},
			wantAddLabels: nil,
			wantRemove:    []string{"agent::builder"},
		},
		{
			name:      "description + due date pass through",
			old:       oldSnap{status: "todo", priority: "none"},
			req:       UpdateIssueRequest{Description: &descNew, DueDate: &due},
			wantDesc:  &descNew,
			wantDue:   &due,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildUpdateIssueInput(
				OldIssueSnapshot{
					Status:       tc.old.status,
					Priority:     tc.old.priority,
					AssigneeType: tc.old.assigneeType,
					AssigneeUUID: tc.old.assigneeUUID,
				},
				tc.req,
				agentSlugByUUID,
			)
			if !stringSliceEq(got.AddLabels, tc.wantAddLabels) {
				t.Errorf("AddLabels = %v, want %v", got.AddLabels, tc.wantAddLabels)
			}
			if !stringSliceEq(got.RemoveLabels, tc.wantRemove) {
				t.Errorf("RemoveLabels = %v, want %v", got.RemoveLabels, tc.wantRemove)
			}
			if !strPtrEq(got.Title, tc.wantTitle) {
				t.Errorf("Title = %v, want %v", got.Title, tc.wantTitle)
			}
			if !strPtrEq(got.Description, tc.wantDesc) {
				t.Errorf("Description = %v, want %v", got.Description, tc.wantDesc)
			}
			if !strPtrEq(got.DueDate, tc.wantDue) {
				t.Errorf("DueDate = %v, want %v", got.DueDate, tc.wantDue)
			}
			if !strPtrEq(got.StateEvent, tc.wantState) {
				t.Errorf("StateEvent = %v, want %v", got.StateEvent, tc.wantState)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func stringSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func strPtrEq(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && go test ./internal/gitlab/ -run TestBuildUpdateIssueInput -v`
Expected: FAIL (UpdateIssueRequest + OldIssueSnapshot + BuildUpdateIssueInput undefined).

- [ ] **Step 3: Add types + function**

Add to `server/internal/gitlab/translator.go` (below `BuildCreateIssueInput`):

```go
// UpdateIssueRequest is the translator-facing input for PATCH /api/issues/{id}.
// Fields are pointers so the translator can distinguish "not in request"
// (nil) from "cleared" (pointer to zero value).
type UpdateIssueRequest struct {
	Title        *string
	Description  *string
	Status       *string
	Priority     *string
	AssigneeType *string // "agent", "member", or "" to clear
	AssigneeID   *string // UUID string, or "" to clear
	DueDate      *string
}

// OldIssueSnapshot is the view of the cache row's current state needed to
// compute a label diff. The handler populates this from the cache row before
// calling the translator.
type OldIssueSnapshot struct {
	Status       string
	Priority     string
	AssigneeType string
	AssigneeUUID string // UUID string; empty if unassigned or member (for Phase 3b member-assignees are cache-only)
}

// BuildUpdateIssueInput diffs the old cache state against the PATCH request
// and emits the GitLab-side update payload — add/remove labels for status,
// priority, and agent-assignee changes; state_event for close/reopen; plus
// pass-through of title/description/due_date when present.
func BuildUpdateIssueInput(old OldIssueSnapshot, req UpdateIssueRequest, agentSlugByUUID map[string]string) gitlabapi.UpdateIssueInput {
	out := gitlabapi.UpdateIssueInput{
		Title:       req.Title,
		Description: req.Description,
		DueDate:     req.DueDate,
	}

	// Status transitions.
	if req.Status != nil && *req.Status != old.Status {
		out.RemoveLabels = append(out.RemoveLabels, "status::"+old.Status)
		out.AddLabels = append(out.AddLabels, "status::"+*req.Status)
		wasClosed := old.Status == "done" || old.Status == "cancelled"
		isClosed := *req.Status == "done" || *req.Status == "cancelled"
		switch {
		case !wasClosed && isClosed:
			ev := "close"
			out.StateEvent = &ev
		case wasClosed && !isClosed:
			ev := "reopen"
			out.StateEvent = &ev
		}
	}

	// Priority transitions. "none" means no label.
	if req.Priority != nil && *req.Priority != old.Priority {
		if old.Priority != "none" && old.Priority != "" {
			out.RemoveLabels = append(out.RemoveLabels, "priority::"+old.Priority)
		}
		if *req.Priority != "none" {
			out.AddLabels = append(out.AddLabels, "priority::"+*req.Priority)
		}
	}

	// Agent assignee transitions. Any change away from the current agent
	// assignee removes the current agent::<slug> label. A new agent assignee
	// adds agent::<new-slug>. Member assignees and unassignment just remove.
	oldAgentSlug := ""
	if old.AssigneeType == "agent" && old.AssigneeUUID != "" {
		oldAgentSlug = agentSlugByUUID[old.AssigneeUUID]
	}
	if req.AssigneeType != nil || req.AssigneeID != nil {
		newType := ""
		if req.AssigneeType != nil {
			newType = *req.AssigneeType
		} else {
			newType = old.AssigneeType
		}
		newID := ""
		if req.AssigneeID != nil {
			newID = *req.AssigneeID
		} else {
			newID = old.AssigneeUUID
		}

		newAgentSlug := ""
		if newType == "agent" && newID != "" {
			newAgentSlug = agentSlugByUUID[newID]
		}

		if oldAgentSlug != newAgentSlug {
			if oldAgentSlug != "" {
				out.RemoveLabels = append(out.RemoveLabels, "agent::"+oldAgentSlug)
			}
			if newAgentSlug != "" {
				out.AddLabels = append(out.AddLabels, "agent::"+newAgentSlug)
			}
		}
	}

	return out
}
```

Ensure the imports include `gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"` (alias used elsewhere in this file).

- [ ] **Step 4: Run to verify pass**

Run: `cd server && go test ./internal/gitlab/ -run TestBuildUpdateIssueInput -v`
Expected: PASS (10 subcases).

- [ ] **Step 5: Commit**

```bash
git add server/internal/gitlab/translator.go server/internal/gitlab/translator_test.go
git commit -m "feat(gitlab): BuildUpdateIssueInput translates Multica PATCH → GitLab update diff"
```

---

## Task 4: Handler — PATCH write-through branch

**Files:**
- Modify: `server/internal/handler/issue.go` (`UpdateIssue` handler around lines 1158–1330)
- Test: `server/internal/handler/issue_test.go`

Write-through pattern mirrors Phase 3a `CreateIssue`:
1. Load cache row (already done in legacy path).
2. Guard: `if h.GitlabEnabled && h.GitlabResolver != nil && <workspace-connected>`.
3. Resolve token for actor.
4. Build agent slug map.
5. Build `UpdateIssueInput` from old snapshot + request.
6. Call `h.Gitlab.UpdateIssue(ctx, token, projectID, iid, input)`.
7. On failure: return error (do NOT fall through).
8. On success: open txn, call `UpsertIssueFromGitlab` (handles clobber guard via `external_updated_at`), then apply the Multica-native fields (`parent_issue_id`, `project_id`, `assignee_type`/`assignee_id` for members) via `UpdateIssue` sqlc query, preserving untouched bare-narg fields from the cache row.
9. Commit, enqueue agent task if assignee changed to agent, publish WS event, return response.

- [ ] **Step 1: Write the failing test (happy path: status change write-through)**

Add to `server/internal/handler/issue_test.go`:

```go
func TestUpdateIssue_WriteThroughStatusChangeSendsLabelDiff(t *testing.T) {
	ctx := context.Background()

	var capturedAddLabels, capturedRemoveLabels, capturedStateEvent string
	var capturedMethod, capturedPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if s, ok := body["add_labels"].(string); ok {
			capturedAddLabels = s
		}
		if s, ok := body["remove_labels"].(string); ok {
			capturedRemoveLabels = s
		}
		if s, ok := body["state_event"].(string); ok {
			capturedStateEvent = s
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":9001,"iid":201,"title":"T","state":"closed","updated_at":"2026-04-17T13:00:00Z","labels":["status::done"]}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	// Seed a cache row in status=in_progress with gitlab_iid=201.
	issueID := uuid.New().String()
	_, err := testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1, $2, 1, 'T', '', 'in_progress', 'none', 201, 42, 9001, '2026-04-17T12:00:00Z', 'member', $3, 0)`,
		issueID, parseUUID(testWorkspaceID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	req := httptest.NewRequest(http.MethodPut, "/api/issues/"+issueID, strings.NewReader(`{"status":"done"}`))
	req = authedRequest(req, testUserID, testWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.UpdateIssue(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/201" {
		t.Errorf("path = %s", capturedPath)
	}
	if capturedAddLabels != "status::done" {
		t.Errorf("add_labels = %q, want status::done", capturedAddLabels)
	}
	if capturedRemoveLabels != "status::in_progress" {
		t.Errorf("remove_labels = %q, want status::in_progress", capturedRemoveLabels)
	}
	if capturedStateEvent != "close" {
		t.Errorf("state_event = %q, want close", capturedStateEvent)
	}

	// Cache should reflect the new status (via the webhook-side clobber guard
	// + our UpsertIssueFromGitlab call).
	var cachedStatus string
	err = testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&cachedStatus)
	if err != nil {
		t.Fatalf("scan cache: %v", err)
	}
	if cachedStatus != "done" {
		t.Errorf("cached status = %s, want done", cachedStatus)
	}
}
```

- [ ] **Step 2: Write the failing test (GitLab error does NOT fall through)**

```go
func TestUpdateIssue_WriteThroughErrorReturnsNonZeroStatus(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1, $2, 1, 'T', '', 'in_progress', 'none', 202, 42, 9002, '2026-04-17T12:00:00Z', 'member', $3, 0)`,
		issueID, parseUUID(testWorkspaceID), parseUUID(testUserID))
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	req := httptest.NewRequest(http.MethodPut, "/api/issues/"+issueID, strings.NewReader(`{"status":"done"}`))
	req = authedRequest(req, testUserID, testWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.UpdateIssue(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want >=400 (GitLab 403 must surface), body = %s", rec.Code, rec.Body.String())
	}

	// Cache must NOT have been updated.
	var cachedStatus string
	_ = testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&cachedStatus)
	if cachedStatus != "in_progress" {
		t.Errorf("cache was touched: status = %s, want in_progress", cachedStatus)
	}
}
```

- [ ] **Step 3: Run to verify both fail**

Run: `cd server && go test ./internal/handler/ -run 'TestUpdateIssue_WriteThrough' -v`
Expected: FAIL (handler doesn't branch on GitLab connection yet).

- [ ] **Step 4: Add the write-through branch to the handler**

In `server/internal/handler/issue.go`, at the top of `UpdateIssue` (after parsing the request body, after loading the existing cache row, BEFORE the legacy `h.Queries.UpdateIssue(...)` call), insert:

```go
	if h.GitlabEnabled && h.GitlabResolver != nil {
		wsConn, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID))
		if wsErr == nil {
			// Workspace is GitLab-connected → write-through is authoritative.
			actorType, actorID := h.resolveActor(r, existing.CreatorID.String(), workspaceID)
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
			if tokErr != nil {
				http.Error(w, tokErr.Error(), http.StatusInternalServerError)
				return
			}

			agentSlugByUUID, agentErr := h.buildAgentUUIDSlugMap(r.Context(), parseUUID(workspaceID))
			if agentErr != nil {
				http.Error(w, agentErr.Error(), http.StatusInternalServerError)
				return
			}

			oldSnap := gitlabsync.OldIssueSnapshot{
				Status:       existing.Status,
				Priority:     existing.Priority,
				AssigneeType: pgTextToString(existing.AssigneeType),
				AssigneeUUID: uuidToString(existing.AssigneeID),
			}
			glInput := gitlabsync.BuildUpdateIssueInput(oldSnap, gitlabsync.UpdateIssueRequest{
				Title:        req.Title,
				Description:  req.Description,
				Status:       req.Status,
				Priority:     req.Priority,
				AssigneeType: req.AssigneeType,
				AssigneeID:   req.AssigneeID,
				DueDate:      req.DueDate,
			}, agentSlugByUUID)

			glIssue, glErr := h.Gitlab.UpdateIssue(r.Context(), token, int(wsConn.GitlabProjectID), int(existing.GitlabIid.Int64), glInput)
			if glErr != nil {
				http.Error(w, glErr.Error(), http.StatusBadGateway)
				return
			}

			// Translate the GitLab response → cache values.
			agentBySlug := invertAgentMap(agentSlugByUUID)
			values := gitlabsync.TranslateIssue(*glIssue, &gitlabsync.TranslateContext{AgentBySlug: agentBySlug})

			glTx, txErr := h.TxStarter.Begin(r.Context())
			if txErr != nil {
				http.Error(w, txErr.Error(), http.StatusInternalServerError)
				return
			}
			defer glTx.Rollback(r.Context())
			qtxGL := h.Queries.WithTx(glTx)

			cacheRow, upErr := qtxGL.UpsertIssueFromGitlab(r.Context(), buildUpsertParamsFromTranslated(existing.WorkspaceID, existing.GitlabProjectID, *glIssue, values))
			if upErr != nil && !errors.Is(upErr, pgx.ErrNoRows) {
				http.Error(w, upErr.Error(), http.StatusInternalServerError)
				return
			}
			if errors.Is(upErr, pgx.ErrNoRows) {
				// Clobber guard rejected the upsert (cache is newer or equal).
				// The webhook event that superseded us will win. Load the
				// cache row as-is to build the response.
				cacheRow = existing
			}

			// Apply Multica-native fields that GitLab doesn't track.
			// Pre-fill bare-narg fields from the cache row so we don't wipe
			// fields we aren't explicitly changing.
			updParams := db.UpdateIssueParams{
				ID:            cacheRow.ID,
				AssigneeType:  cacheRow.AssigneeType,
				AssigneeID:    cacheRow.AssigneeID,
				DueDate:       cacheRow.DueDate,
				ParentIssueID: cacheRow.ParentIssueID,
				ProjectID:     cacheRow.ProjectID,
			}
			touched := false
			if req.ParentIssueID != nil {
				updParams.ParentIssueID = pgUUID(*req.ParentIssueID)
				touched = true
			}
			if req.ProjectID != nil {
				updParams.ProjectID = pgUUID(*req.ProjectID)
				touched = true
			}
			// Member assignees are cache-only in Phase 3b.
			if req.AssigneeType != nil && *req.AssigneeType == "member" {
				updParams.AssigneeType = pgtype.Text{String: "member", Valid: true}
				if req.AssigneeID != nil {
					updParams.AssigneeID = pgUUID(*req.AssigneeID)
				}
				touched = true
			}
			if touched {
				updated, updErr := qtxGL.UpdateIssue(r.Context(), updParams)
				if updErr != nil {
					http.Error(w, updErr.Error(), http.StatusInternalServerError)
					return
				}
				cacheRow = updated
			}

			if err := glTx.Commit(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Post-commit: enqueue agent task if assignee is now an agent and
			// it changed.
			if cacheRow.AssigneeType.Valid && cacheRow.AssigneeID.Valid && h.shouldEnqueueAgentTask(r.Context(), cacheRow) {
				h.TaskService.EnqueueTaskForIssue(r.Context(), cacheRow)
			}

			resp := issueToResponse(cacheRow, h.getIssuePrefix(r.Context(), cacheRow.WorkspaceID), nil)
			h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{"issue": resp})
			writeJSON(w, http.StatusOK, resp)
			return
		}
		// else: workspace not connected → fall through to legacy path
	}
```

Helper `buildUpsertParamsFromTranslated` should be added to `server/internal/handler/issue.go` near `buildUpsertParamsFromCreate` if it doesn't already exist — it maps a translated values struct + GitLab issue onto `db.UpsertIssueFromGitlabParams`. Keep the helper minimal; it's a mapping function, not a business logic one. If a similar helper from Phase 3a already exists that accepts a translated values struct, reuse it.

Helper `pgTextToString(t pgtype.Text) string` returns `t.String` when `t.Valid`, else `""`. Helper `uuidToString(u pgtype.UUID) string` returns the UUID's string form when valid, else `""`. Helper `invertAgentMap(m map[string]string) map[string]string` swaps keys and values. Helper `pgUUID(s string) pgtype.UUID` parses a string into a pgtype.UUID. All of these either already exist (grep for them) or are trivial one-liners; add any missing ones adjacent to where they're first used.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd server && go test ./internal/handler/ -run 'TestUpdateIssue_WriteThrough' -v`
Expected: PASS (2 tests).

- [ ] **Step 6: Run the full handler suite to catch regressions**

Run: `cd server && go test ./internal/handler/`
Expected: PASS. Pre-existing date-bucket flake (`TestGetRuntimeUsage_BucketsByUsageTime`, `TestWorkspaceUsage_BucketsByUsageTime`) may fail — confirm they fail on `main` too before dismissing.

- [ ] **Step 7: Commit**

```bash
git add server/internal/handler/issue.go server/internal/handler/issue_test.go
git commit -m "feat(handler): PATCH /api/issues/{id} writes through GitLab when connected"
```

---

## Task 5: Unmount 501 from PATCH route

**Files:**
- Modify: `server/cmd/server/router.go` (the `/api/issues/{id}` subroute around line 273)

- [ ] **Step 1: Inspect current routing**

Open `server/cmd/server/router.go`. Locate the `/api/issues/{id}` subroute. Confirm the PATCH is currently wrapped:

```go
r.With(gw).Put("/", h.UpdateIssue)
```

- [ ] **Step 2: Remove the `gw` wrap**

Change the line to:

```go
r.Put("/", h.UpdateIssue)
```

Do NOT change any other `r.With(gw)` wraps in this file. Only PATCH.

- [ ] **Step 3: Run the full server test suite**

Run: `cd server && go test ./cmd/server/ ./internal/handler/ ./internal/middleware/`
Expected: PASS. The 501-middleware tests should still hit (for the other write routes).

- [ ] **Step 4: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(server): unmount 501 stopgap from PATCH /api/issues/{id}"
```

---

## Task 6: Handler — DELETE write-through branch

**Files:**
- Modify: `server/internal/handler/issue.go` (`DeleteIssue` around lines 1440–1466)
- Test: `server/internal/handler/issue_test.go`

- [ ] **Step 1: Write the failing test (happy path)**

```go
func TestDeleteIssue_WriteThroughSendsDELETE(t *testing.T) {
	ctx := context.Background()
	var capturedMethod, capturedPath, capturedToken string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1, $2, 1, 'T', '', 'todo', 'none', 301, 42, 9101, '2026-04-17T12:00:00Z', 'member', $3, 0)`,
		issueID, parseUUID(testWorkspaceID), parseUUID(testUserID))
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/"+issueID, nil)
	req = authedRequest(req, testUserID, testWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.DeleteIssue(rec, req)

	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %s", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/301" {
		t.Errorf("path = %s", capturedPath)
	}
	if capturedToken == "" {
		t.Errorf("token header missing")
	}

	// Cache row must be gone.
	var count int
	_ = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE id = $1`, issueID).Scan(&count)
	if count != 0 {
		t.Errorf("issue row not deleted, count = %d", count)
	}
}
```

- [ ] **Step 2: Write the failing test (GitLab error does NOT touch cache)**

```go
func TestDeleteIssue_WriteThroughErrorPreservesCache(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1, $2, 1, 'T', '', 'todo', 'none', 302, 42, 9102, '2026-04-17T12:00:00Z', 'member', $3, 0)`,
		issueID, parseUUID(testWorkspaceID), parseUUID(testUserID))
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	req := httptest.NewRequest(http.MethodDelete, "/api/issues/"+issueID, nil)
	req = authedRequest(req, testUserID, testWorkspaceID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.DeleteIssue(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want >=400", rec.Code)
	}

	var count int
	_ = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE id = $1`, issueID).Scan(&count)
	if count != 1 {
		t.Errorf("cache was mutated on GitLab failure, count = %d", count)
	}
}
```

- [ ] **Step 3: Run to verify both fail**

Run: `cd server && go test ./internal/handler/ -run 'TestDeleteIssue_WriteThrough' -v`
Expected: FAIL.

- [ ] **Step 4: Add write-through branch to `DeleteIssue`**

In `server/internal/handler/issue.go`, at the top of `DeleteIssue` (after loading `existing`, BEFORE any of the legacy cleanup), insert:

```go
	if h.GitlabEnabled && h.GitlabResolver != nil {
		wsConn, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID))
		if wsErr == nil {
			actorType, actorID := h.resolveActor(r, existing.CreatorID.String(), workspaceID)
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
			if tokErr != nil {
				http.Error(w, tokErr.Error(), http.StatusInternalServerError)
				return
			}

			if err := h.Gitlab.DeleteIssue(r.Context(), token, int(wsConn.GitlabProjectID), int(existing.GitlabIid.Int64)); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}

			// GitLab acknowledged the delete (or was already gone via 404 →
			// treated as success inside the client). Now remove the cache row
			// and related cleanup via the same legacy path.
			if err := h.cleanupAndDeleteIssueRow(r.Context(), existing); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": existing.ID.String()})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// else: not connected → fall through to legacy path
	}
```

Extract the existing legacy cleanup (cancel agent tasks, fail autopilot runs, collect attachment URLs, `DeleteIssue` sqlc, S3 object deletion, WS event) into a new private method:

```go
// cleanupAndDeleteIssueRow performs the Multica-side cleanup for a deleted
// issue: cancels agent tasks, fails autopilot runs, deletes S3 attachments,
// and removes the cache row. Shared between the legacy DeleteIssue path and
// the GitLab write-through branch.
func (h *Handler) cleanupAndDeleteIssueRow(ctx context.Context, issue db.Issue) error {
	// (move the body of the existing legacy cleanup code here)
}
```

Then in the legacy path, replace the inline cleanup with a single `cleanupAndDeleteIssueRow` call + the existing WS event. Don't duplicate the logic — DRY.

- [ ] **Step 5: Run to verify tests pass**

Run: `cd server && go test ./internal/handler/ -run 'TestDeleteIssue' -v`
Expected: PASS (both new write-through tests + legacy tests unchanged).

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/issue.go server/internal/handler/issue_test.go
git commit -m "feat(handler): DELETE /api/issues/{id} writes through GitLab when connected"
```

---

## Task 7: Unmount 501 from DELETE route

**Files:**
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Remove `gw` wrap from DELETE**

Change:

```go
r.With(gw).Delete("/", h.DeleteIssue)
```

to:

```go
r.Delete("/", h.DeleteIssue)
```

- [ ] **Step 2: Run targeted tests**

Run: `cd server && go test ./cmd/server/ ./internal/handler/ -run 'Issue'`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(server): unmount 501 stopgap from DELETE /api/issues/{id}"
```

---

## Task 8: Batch response types + helper

**Files:**
- Modify: `server/internal/handler/issue.go` (add types near the existing request/response types)

- [ ] **Step 1: Write the failing test**

```go
func TestBatchResult_ShapeAndJSON(t *testing.T) {
	r := BatchWriteResult{
		Succeeded: []BatchSucceeded{{ID: "abc", Issue: nil}},
		Failed:    []BatchFailed{{ID: "def", ErrorCode: "GITLAB_403", Message: "forbidden"}},
	}
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"succeeded":[{"id":"abc","issue":null}],"failed":[{"id":"def","error_code":"GITLAB_403","message":"forbidden"}]}`
	if string(body) != want {
		t.Errorf("json = %s\nwant  %s", body, want)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && go test ./internal/handler/ -run TestBatchResult -v`
Expected: FAIL (types undefined).

- [ ] **Step 3: Add types**

```go
// BatchWriteResult is the continue-on-error shape returned by
// /api/issues/batch-update and /api/issues/batch-delete when at least one
// item succeeded AND at least one failed — HTTP 207 Multi-Status.
//
// When all items succeed → HTTP 200 and Failed is empty.
// When all items fail → HTTP 200 with Succeeded empty (client inspects Failed).
// Individual failures never abort the batch.
type BatchWriteResult struct {
	Succeeded []BatchSucceeded `json:"succeeded"`
	Failed    []BatchFailed    `json:"failed"`
}

type BatchSucceeded struct {
	ID    string         `json:"id"`
	Issue *IssueResponse `json:"issue"` // nil for batch-delete
}

type BatchFailed struct {
	ID        string `json:"id"`
	ErrorCode string `json:"error_code"` // e.g. "GITLAB_403", "NOT_FOUND", "VALIDATION_FAILED"
	Message   string `json:"message"`
}

// classifyBatchError maps a GitLab-or-handler error to a stable error_code
// string for the BatchFailed response. Stability matters — clients
// key retry logic off these codes.
func classifyBatchError(err error) (code, msg string) {
	if err == nil {
		return "", ""
	}
	m := err.Error()
	switch {
	case strings.Contains(m, "403"):
		return "GITLAB_403", m
	case strings.Contains(m, "404"):
		return "GITLAB_404", m
	case strings.Contains(m, "429"):
		return "GITLAB_429", m
	case errors.Is(err, pgx.ErrNoRows):
		return "NOT_FOUND", "issue not found"
	default:
		return "WRITE_FAILED", m
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd server && go test ./internal/handler/ -run TestBatchResult -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/issue.go server/internal/handler/issue_test.go
git commit -m "feat(handler): batch-write result types + error classification"
```

---

## Task 9: Handler — `BatchUpdateIssues` continue-on-error loop

**Files:**
- Modify: `server/internal/handler/issue.go` (`BatchUpdateIssues` around lines 1477+)
- Test: `server/internal/handler/issue_test.go`

The legacy `BatchUpdateIssues` loops and calls the single-issue update logic inline. For Phase 3b, factor the single-issue write-through path (from Task 4) into an internal helper `updateSingleIssueForBatch(ctx, issueID, req, actorType, actorID, wsConn, token, agentSlugByUUID) (IssueResponse, error)` that both the single-issue `UpdateIssue` handler and `BatchUpdateIssues` can call. The helper handles one issue end-to-end (GitLab call + cache txn + agent enqueue) and returns either the IssueResponse or an error.

- [ ] **Step 1: Extract the per-issue write-through path into a helper**

In `server/internal/handler/issue.go`, pull the entire write-through branch from Task 4 (the code that runs once we've decided "workspace is connected, we're doing write-through") into a new method:

```go
// updateSingleIssueWriteThrough executes the Phase 3b write-through for a
// single issue. Used by both UpdateIssue (direct handler) and
// BatchUpdateIssues (loops over this).
//
// actorType/actorID come from the request's auth context. wsConn/token are
// resolved once per batch to avoid redundant DB + GitLab calls.
func (h *Handler) updateSingleIssueWriteThrough(
	ctx context.Context,
	issueID string,
	req UpdateIssueRequest,
	actorType, actorID, workspaceID string,
	wsConn db.WorkspaceGitlabConnection,
	token string,
	agentSlugByUUID map[string]string,
) (*IssueResponse, db.Issue, error) {
	// (move the body of the write-through branch from Task 4 here, adjusting
	// to use the passed-in helpers instead of re-resolving them)
}
```

Have the direct `UpdateIssue` handler call this helper once, passing its own resolved `wsConn`/`token`/`agentSlugByUUID`. No duplication.

- [ ] **Step 2: Write the failing test (mixed success/failure)**

```go
func TestBatchUpdateIssues_ContinueOnError(t *testing.T) {
	ctx := context.Background()
	var gitlabCalls int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gitlabCalls++
		if strings.Contains(r.URL.Path, "/issues/401") {
			// Fail on this specific issue.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":9001,"iid":400,"title":"T","state":"opened","updated_at":"2026-04-17T13:00:00Z","labels":["status::done"]}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	goodID := uuid.New().String()
	badID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1, $2, 1, 'A', '', 'todo', 'none', 400, 42, 9001, '2026-04-17T12:00:00Z', 'member', $3, 0),
		        ($4, $2, 2, 'B', '', 'todo', 'none', 401, 42, 9002, '2026-04-17T12:00:00Z', 'member', $3, 0)`,
		goodID, parseUUID(testWorkspaceID), parseUUID(testUserID), badID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id IN ($1, $2)`, goodID, badID)
	})

	body := fmt.Sprintf(`{"issue_ids":["%s","%s"],"updates":{"status":"done"}}`, goodID, badID)
	req := httptest.NewRequest(http.MethodPost, "/api/issues/batch-update", strings.NewReader(body))
	req = authedRequest(req, testUserID, testWorkspaceID)
	rec := httptest.NewRecorder()

	h.BatchUpdateIssues(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207, body = %s", rec.Code, rec.Body.String())
	}
	var result BatchWriteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Succeeded) != 1 || result.Succeeded[0].ID != goodID {
		t.Errorf("succeeded = %+v, want 1 item with id=%s", result.Succeeded, goodID)
	}
	if len(result.Failed) != 1 || result.Failed[0].ID != badID {
		t.Errorf("failed = %+v, want 1 item with id=%s", result.Failed, badID)
	}
	if result.Failed[0].ErrorCode != "GITLAB_403" {
		t.Errorf("error_code = %s, want GITLAB_403", result.Failed[0].ErrorCode)
	}
	if gitlabCalls != 2 {
		t.Errorf("gitlab call count = %d, want 2 (both items attempted)", gitlabCalls)
	}
}

func TestBatchUpdateIssues_AllSuccessReturns200(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":9001,"iid":402,"title":"T","state":"opened","updated_at":"2026-04-17T13:00:00Z","labels":["status::done"]}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	aID := uuid.New().String()
	bID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1, $2, 1, 'A', '', 'todo', 'none', 402, 42, 9001, '2026-04-17T12:00:00Z', 'member', $3, 0),
		        ($4, $2, 2, 'B', '', 'todo', 'none', 403, 42, 9002, '2026-04-17T12:00:00Z', 'member', $3, 0)`,
		aID, parseUUID(testWorkspaceID), parseUUID(testUserID), bID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id IN ($1, $2)`, aID, bID)
	})

	body := fmt.Sprintf(`{"issue_ids":["%s","%s"],"updates":{"status":"done"}}`, aID, bID)
	req := httptest.NewRequest(http.MethodPost, "/api/issues/batch-update", strings.NewReader(body))
	req = authedRequest(req, testUserID, testWorkspaceID)
	rec := httptest.NewRecorder()

	h.BatchUpdateIssues(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (all-success), body = %s", rec.Code, rec.Body.String())
	}
	var result BatchWriteResult
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if len(result.Succeeded) != 2 {
		t.Errorf("succeeded = %d, want 2", len(result.Succeeded))
	}
	if len(result.Failed) != 0 {
		t.Errorf("failed = %+v, want empty on all-success", result.Failed)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `cd server && go test ./internal/handler/ -run 'TestBatchUpdateIssues_' -v`
Expected: FAIL.

- [ ] **Step 4: Rewrite `BatchUpdateIssues` to continue-on-error**

Replace the legacy `BatchUpdateIssues` body with the write-through-aware loop:

```go
func (h *Handler) BatchUpdateIssues(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.Context().Value(middleware.WorkspaceIDCtxKey).(string)

	var req BatchUpdateIssuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if h.GitlabEnabled && h.GitlabResolver != nil {
		wsConn, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID))
		if wsErr == nil {
			actorType, actorID := h.resolveActor(r, "", workspaceID)
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
			if tokErr != nil {
				http.Error(w, tokErr.Error(), http.StatusInternalServerError)
				return
			}
			agentSlugByUUID, _ := h.buildAgentUUIDSlugMap(r.Context(), parseUUID(workspaceID))

			result := BatchWriteResult{}
			for _, id := range req.IssueIDs {
				resp, _, perr := h.updateSingleIssueWriteThrough(r.Context(), id, req.Updates, actorType, actorID, workspaceID, wsConn, token, agentSlugByUUID)
				if perr != nil {
					code, msg := classifyBatchError(perr)
					result.Failed = append(result.Failed, BatchFailed{ID: id, ErrorCode: code, Message: msg})
					continue
				}
				result.Succeeded = append(result.Succeeded, BatchSucceeded{ID: id, Issue: resp})
			}

			status := http.StatusOK
			if len(result.Failed) > 0 && len(result.Succeeded) > 0 {
				status = http.StatusMultiStatus // 207
			}
			writeJSON(w, status, result)
			return
		}
		// else: fall through to legacy batch path
	}

	// Legacy batch path (unchanged from before).
	h.legacyBatchUpdateIssues(w, r, req)
}
```

Extract the existing legacy body into `legacyBatchUpdateIssues` (no logic change — just a rename/move).

- [ ] **Step 5: Run tests to verify pass**

Run: `cd server && go test ./internal/handler/ -run 'TestBatchUpdateIssues' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/issue.go server/internal/handler/issue_test.go
git commit -m "feat(handler): batch-update writes through GitLab with continue-on-error"
```

---

## Task 10: Unmount 501 from batch-update route

**Files:**
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Remove `gw` wrap**

Change:

```go
r.With(gw).Post("/batch-update", h.BatchUpdateIssues)
```

to:

```go
r.Post("/batch-update", h.BatchUpdateIssues)
```

- [ ] **Step 2: Run server tests**

Run: `cd server && go test ./cmd/server/ ./internal/handler/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(server): unmount 501 stopgap from POST /api/issues/batch-update"
```

---

## Task 11: Handler — `BatchDeleteIssues` continue-on-error loop

**Files:**
- Modify: `server/internal/handler/issue.go`
- Test: `server/internal/handler/issue_test.go`

- [ ] **Step 1: Extract `deleteSingleIssueWriteThrough` helper**

Factor the DELETE write-through body (from Task 6) into an internal helper:

```go
func (h *Handler) deleteSingleIssueWriteThrough(
	ctx context.Context,
	issueID string,
	actorType, actorID, workspaceID string,
	wsConn db.WorkspaceGitlabConnection,
	token string,
) error {
	existing, err := h.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		return err
	}
	if err := h.Gitlab.DeleteIssue(ctx, token, int(wsConn.GitlabProjectID), int(existing.GitlabIid.Int64)); err != nil {
		return err
	}
	if err := h.cleanupAndDeleteIssueRow(ctx, existing); err != nil {
		return err
	}
	return nil
}
```

Have the direct `DeleteIssue` handler call this helper.

- [ ] **Step 2: Write the failing test (mixed success/failure)**

```go
func TestBatchDeleteIssues_ContinueOnError(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/issues/501") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	goodID := uuid.New().String()
	badID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1, $2, 1, 'A', '', 'todo', 'none', 500, 42, 9500, '2026-04-17T12:00:00Z', 'member', $3, 0),
		        ($4, $2, 2, 'B', '', 'todo', 'none', 501, 42, 9501, '2026-04-17T12:00:00Z', 'member', $3, 0)`,
		goodID, parseUUID(testWorkspaceID), parseUUID(testUserID), badID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id IN ($1, $2)`, goodID, badID)
	})

	body := fmt.Sprintf(`{"issue_ids":["%s","%s"]}`, goodID, badID)
	req := httptest.NewRequest(http.MethodPost, "/api/issues/batch-delete", strings.NewReader(body))
	req = authedRequest(req, testUserID, testWorkspaceID)
	rec := httptest.NewRecorder()

	h.BatchDeleteIssues(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207", rec.Code)
	}
	var result BatchWriteResult
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if len(result.Succeeded) != 1 || result.Succeeded[0].ID != goodID {
		t.Errorf("succeeded = %+v, want 1 item (id=%s)", result.Succeeded, goodID)
	}
	if len(result.Failed) != 1 || result.Failed[0].ID != badID {
		t.Errorf("failed = %+v, want 1 item (id=%s)", result.Failed, badID)
	}

	// Good issue should be gone, bad one should remain.
	var goodCount, badCount int
	_ = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE id = $1`, goodID).Scan(&goodCount)
	_ = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE id = $1`, badID).Scan(&badCount)
	if goodCount != 0 {
		t.Errorf("good issue not deleted, count = %d", goodCount)
	}
	if badCount != 1 {
		t.Errorf("bad issue unexpectedly deleted, count = %d", badCount)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `cd server && go test ./internal/handler/ -run 'TestBatchDeleteIssues' -v`
Expected: FAIL.

- [ ] **Step 4: Implement the write-through batch loop**

```go
func (h *Handler) BatchDeleteIssues(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.Context().Value(middleware.WorkspaceIDCtxKey).(string)

	var req BatchDeleteIssuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if h.GitlabEnabled && h.GitlabResolver != nil {
		wsConn, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID))
		if wsErr == nil {
			actorType, actorID := h.resolveActor(r, "", workspaceID)
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
			if tokErr != nil {
				http.Error(w, tokErr.Error(), http.StatusInternalServerError)
				return
			}

			result := BatchWriteResult{}
			for _, id := range req.IssueIDs {
				if err := h.deleteSingleIssueWriteThrough(r.Context(), id, actorType, actorID, workspaceID, wsConn, token); err != nil {
					code, msg := classifyBatchError(err)
					result.Failed = append(result.Failed, BatchFailed{ID: id, ErrorCode: code, Message: msg})
					continue
				}
				result.Succeeded = append(result.Succeeded, BatchSucceeded{ID: id})
				h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": id})
			}

			status := http.StatusOK
			if len(result.Failed) > 0 && len(result.Succeeded) > 0 {
				status = http.StatusMultiStatus
			}
			writeJSON(w, status, result)
			return
		}
	}

	h.legacyBatchDeleteIssues(w, r, req)
}
```

Same legacy-extraction pattern as Task 9 — move the existing batch-delete body to `legacyBatchDeleteIssues(w, r, req)`.

- [ ] **Step 5: Run tests to verify pass**

Run: `cd server && go test ./internal/handler/ -run 'TestBatchDeleteIssues' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/issue.go server/internal/handler/issue_test.go
git commit -m "feat(handler): batch-delete writes through GitLab with continue-on-error"
```

---

## Task 12: Unmount 501 from batch-delete route

**Files:**
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Remove `gw` wrap**

Change:

```go
r.With(gw).Post("/batch-delete", h.BatchDeleteIssues)
```

to:

```go
r.Post("/batch-delete", h.BatchDeleteIssues)
```

- [ ] **Step 2: Run server tests**

Run: `cd server && go test ./cmd/server/ ./internal/handler/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(server): unmount 501 stopgap from POST /api/issues/batch-delete"
```

---

## Task 13: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full Go test suite**

Run: `cd server && go test ./...`
Expected: all packages PASS (pre-existing date-bucket flake is fine; verify it fails on `main` too).

- [ ] **Step 2: Frontend typecheck + tests**

Run (from repo root): `pnpm typecheck && pnpm test`
Expected: all green. Phase 3b didn't touch frontend, but run anyway to confirm nothing was accidentally broken.

- [ ] **Step 3: Confirm routing cleanup**

Grep router.go and confirm that PATCH/DELETE/batch-update/batch-delete are NOT wrapped with `r.With(gw)`:

Run: `grep -n "gw" server/cmd/server/router.go`
Expected output: should show `gw` wraps for remaining write routes (comments, reactions, subscribe, task cancel), but NOT for the four routes Phase 3b unblocked.

- [ ] **Step 4: Confirm the middleware still fires for a non-PATCH write**

Write a quick integration test (or verify existing ones cover this): on a GitLab-connected workspace, `POST /api/issues/{id}/comments` still returns 501.

Run: `cd server && go test ./internal/middleware/ -run GitlabWritesBlocked`
Expected: PASS.

- [ ] **Step 5: Smoke test build**

Run: `cd server && go build ./cmd/server/`
Expected: binary builds with no error.

---

## Self-Review Checklist

1. **Spec coverage.** Every Phase 3b in-scope endpoint (PATCH, DELETE, batch-update, batch-delete) has a write-through task + a route-unblocking task. ✓
2. **Placeholder scan.** No "TBD", "similar to Task N", or "handle edge cases" in steps. ✓
3. **Type consistency.**
   - `UpdateIssueInput` defined in Task 1, used in Task 3 (translator) and Task 4 (handler). ✓
   - `UpdateIssueRequest` / `OldIssueSnapshot` defined in Task 3, used in Task 4. ✓
   - `BatchWriteResult` / `BatchSucceeded` / `BatchFailed` defined in Task 8, used in Tasks 9 and 11. ✓
   - `classifyBatchError` defined in Task 8, used in Tasks 9 and 11. ✓
   - Helper method names (`updateSingleIssueWriteThrough`, `deleteSingleIssueWriteThrough`, `cleanupAndDeleteIssueRow`, `legacyBatchUpdateIssues`, `legacyBatchDeleteIssues`) are consistent across tasks. ✓
4. **Hard rules enforced.**
   - Write-through is authoritative: `http.StatusBadGateway` on GitLab error, no fallback to legacy. ✓ (Tasks 4, 6)
   - Bare-narg fields pre-filled from cache row before `UpdateIssue`. ✓ (Task 4 Step 4)
   - `ResolveTokenForWrite` used for every outbound call. ✓ (Tasks 4, 6, 9, 11)
   - Continue-on-error: individual item failure caught and classified, loop continues. ✓ (Tasks 9, 11)
   - Clobber guard respected: `UpsertIssueFromGitlab`'s ON CONFLICT check on `external_updated_at`, `pgx.ErrNoRows` handled as "webhook won". ✓ (Task 4 Step 4)
5. **Test discipline.** Every behavior has a failing-test-first step. ✓

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-17-gitlab-issues-integration-phase-3b.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
