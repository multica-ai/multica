package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Pure unit tests (no DB, always run) ─────────────────────────────────────

func TestNormalizeADOURL(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantNorm   string
		wantOrg    string
		wantProj   string
		wantRepo   string
	}{
		{
			name:     "canonical dev.azure.com",
			input:    "https://dev.azure.com/myorg/myproject/_git/myrepo",
			wantNorm: "https://dev.azure.com/myorg/myproject/_git/myrepo",
			wantOrg:  "myorg",
			wantProj: "myproject",
			wantRepo: "myrepo",
		},
		{
			name:     "legacy visualstudio.com normalized",
			input:    "https://myorg.visualstudio.com/myproject/_git/myrepo",
			wantNorm: "https://dev.azure.com/myorg/myproject/_git/myrepo",
			wantOrg:  "myorg",
			wantProj: "myproject",
			wantRepo: "myrepo",
		},
		{
			name:     "SSH URL kept as-is",
			input:    "git@ssh.dev.azure.com:v3/myorg/myproject/myrepo",
			wantNorm: "git@ssh.dev.azure.com:v3/myorg/myproject/myrepo",
			wantOrg:  "myorg",
			wantProj: "myproject",
			wantRepo: "myrepo",
		},
		{
			name:     "not an ADO URL returns empty",
			input:    "https://github.com/owner/repo",
			wantNorm: "",
			wantOrg:  "",
			wantProj: "",
			wantRepo: "",
		},
		{
			name:     "dev.azure.com without _git segment",
			input:    "https://dev.azure.com/myorg",
			wantNorm: "",
		},
		{
			name:     "multi-segment org round-trip",
			input:    "https://dev.azure.com/contoso-corp/MyProject/_git/MyRepo",
			wantNorm: "https://dev.azure.com/contoso-corp/MyProject/_git/MyRepo",
			wantOrg:  "contoso-corp",
			wantProj: "MyProject",
			wantRepo: "MyRepo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			norm, org, proj, repo := normalizeADOURL(tc.input)
			if norm != tc.wantNorm {
				t.Errorf("normalized = %q, want %q", norm, tc.wantNorm)
			}
			if org != tc.wantOrg {
				t.Errorf("org = %q, want %q", org, tc.wantOrg)
			}
			if proj != tc.wantProj {
				t.Errorf("project = %q, want %q", proj, tc.wantProj)
			}
			if repo != tc.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tc.wantRepo)
			}
		})
	}
}

func TestDeriveADOPRState(t *testing.T) {
	cases := []struct {
		status  string
		isDraft bool
		want    string
	}{
		{"active", false, "open"},
		{"active", true, "draft"},
		{"completed", false, "merged"},
		{"completed", true, "merged"}, // completed trumps draft
		{"abandoned", false, "abandoned"},
		{"abandoned", true, "abandoned"},
	}
	for _, tc := range cases {
		got := deriveADOPRState(tc.status, tc.isDraft)
		if got != tc.want {
			t.Errorf("deriveADOPRState(%q, draft=%v) = %q, want %q",
				tc.status, tc.isDraft, got, tc.want)
		}
	}
}

func TestDeriveADOPolicyStatus(t *testing.T) {
	type reviewer struct {
		Vote int `json:"vote"`
	}
	cases := []struct {
		name        string
		mergeStatus string
		reviewers   []reviewer
		wantValid   bool
		wantStatus  string
	}{
		{
			name:        "succeeded → approved",
			mergeStatus: "succeeded",
			wantValid:   true,
			wantStatus:  "approved",
		},
		{
			name:        "noVote → approved",
			mergeStatus: "noVote",
			wantValid:   true,
			wantStatus:  "approved",
		},
		{
			name:        "blocked → blocked",
			mergeStatus: "blocked",
			wantValid:   true,
			wantStatus:  "blocked",
		},
		{
			name:        "rejectedByPolicy → blocked",
			mergeStatus: "rejectedByPolicy",
			wantValid:   true,
			wantStatus:  "blocked",
		},
		{
			name:        "queued → pending",
			mergeStatus: "queued",
			wantValid:   true,
			wantStatus:  "pending",
		},
		{
			name:        "unknown status → nil",
			mergeStatus: "conflicts",
			wantValid:   false,
		},
		{
			name:        "reviewer vote -10 always blocks",
			mergeStatus: "succeeded",
			reviewers:   []reviewer{{Vote: -10}},
			wantValid:   true,
			wantStatus:  "blocked",
		},
		{
			name:        "reviewer vote -10 overrides pending",
			mergeStatus: "queued",
			reviewers:   []reviewer{{Vote: 10}, {Vote: -10}},
			wantValid:   true,
			wantStatus:  "blocked",
		},
		{
			name:        "positive votes do not block",
			mergeStatus: "succeeded",
			reviewers:   []reviewer{{Vote: 10}, {Vote: 5}},
			wantValid:   true,
			wantStatus:  "approved",
		},
		{
			name:        "empty reviewers + empty mergeStatus → nil",
			mergeStatus: "",
			reviewers:   nil,
			wantValid:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Convert to the anonymous struct type expected by deriveADOPolicyStatus.
			reviewers := make([]struct {
				Vote int `json:"vote"`
			}, len(tc.reviewers))
			for i, r := range tc.reviewers {
				reviewers[i].Vote = r.Vote
			}
			got := deriveADOPolicyStatus(tc.mergeStatus, reviewers)
			if got.Valid != tc.wantValid {
				t.Errorf("deriveADOPolicyStatus(%q, ...).Valid = %v, want %v",
					tc.mergeStatus, got.Valid, tc.wantValid)
			}
			if tc.wantValid && got.String != tc.wantStatus {
				t.Errorf("deriveADOPolicyStatus(%q, ...).String = %q, want %q",
					tc.mergeStatus, got.String, tc.wantStatus)
			}
		})
	}
}

func TestADOAggregateChecksConclusion(t *testing.T) {
	ptr := func(s string) string { return s }
	_ = ptr
	asStr := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	cases := []struct {
		name                   string
		failed, passed, pending, total int64
		want                   string
	}{
		{"no_suites_nil", 0, 0, 0, 0, "<nil>"},
		{"any_failure_wins", 1, 5, 0, 6, "failed"},
		{"failure_beats_pending", 1, 0, 3, 4, "failed"},
		{"pending_when_no_failure", 0, 1, 2, 3, "pending"},
		{"all_passed", 0, 3, 0, 3, "passed"},
		{"counts_zero_but_total_nonzero", 0, 0, 0, 1, "<nil>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := adoAggregateChecksConclusion(tc.failed, tc.passed, tc.pending, tc.total)
			if asStr(got) != tc.want {
				t.Errorf("adoAggregateChecksConclusion(%d,%d,%d,%d) = %s, want %s",
					tc.failed, tc.passed, tc.pending, tc.total, asStr(got), tc.want)
			}
		})
	}
}

func TestEncryptDecryptPAT(t *testing.T) {
	t.Setenv("ADO_ENCRYPTION_KEY", "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	pat := "my-secret-personal-access-token-abc123"

	encrypted, err := encryptPAT(pat)
	if err != nil {
		t.Fatalf("encryptPAT: %v", err)
	}
	if len(encrypted) == 0 {
		t.Fatal("encryptPAT returned empty ciphertext")
	}

	decrypted, err := decryptPAT(encrypted)
	if err != nil {
		t.Fatalf("decryptPAT: %v", err)
	}
	if decrypted != pat {
		t.Errorf("round-trip: got %q, want %q", decrypted, pat)
	}

	// Tampered ciphertext must fail.
	tampered := make([]byte, len(encrypted))
	copy(tampered, encrypted)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := decryptPAT(tampered); err == nil {
		t.Error("decryptPAT should reject tampered ciphertext")
	}

	// Too-short ciphertext must fail.
	if _, err := decryptPAT([]byte{0x01}); err == nil {
		t.Error("decryptPAT should reject too-short ciphertext")
	}
}

func TestBuildADOPRURL(t *testing.T) {
	cases := []struct {
		orgURL, project, repo string
		prID                  int32
		want                  string
	}{
		{
			"https://dev.azure.com/myorg", "MyProject", "MyRepo", 42,
			"https://dev.azure.com/myorg/MyProject/_git/MyRepo/pullrequest/42",
		},
		{
			"https://dev.azure.com/contoso-corp", "Platform", "Core", 1,
			"https://dev.azure.com/contoso-corp/Platform/_git/Core/pullrequest/1",
		},
	}
	for _, tc := range cases {
		got := buildADOPRURL(tc.orgURL, tc.project, tc.repo, tc.prID)
		if got != tc.want {
			t.Errorf("buildADOPRURL = %q, want %q", got, tc.want)
		}
	}
}

func TestParseADOTime(t *testing.T) {
	cases := []struct {
		input     string
		wantValid bool
	}{
		{"2026-05-01T12:00:00Z", true},
		{"2026-05-01T12:00:00.000Z", true},
		{"2026-05-01T12:00:00+02:00", true},
		{"", false},
		{"not-a-date", false},
	}
	for _, tc := range cases {
		got := parseADOTime(tc.input)
		if got.Valid != tc.wantValid {
			t.Errorf("parseADOTime(%q).Valid = %v, want %v", tc.input, got.Valid, tc.wantValid)
		}
		if tc.wantValid && got.Time.IsZero() {
			t.Errorf("parseADOTime(%q).Time is zero", tc.input)
		}
	}
}

func TestParseADOTimeRequired(t *testing.T) {
	// Empty input must fall back to now, not zero.
	before := time.Now().UTC().Add(-time.Second)
	got := parseADOTimeRequired("")
	if !got.Valid {
		t.Fatal("parseADOTimeRequired('').Valid = false, want true")
	}
	if got.Time.Before(before) {
		t.Errorf("parseADOTimeRequired('') returned time in the past: %v", got.Time)
	}
}

// TestAzureDevOpsRepoRefValidation verifies the CreateProjectResource endpoint
// validates and normalizes azure_devops_repo refs correctly, including silent
// normalization of legacy visualstudio.com URLs.
func TestAzureDevOpsRepoRefValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "ADO ref validation project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)
	defer func() {
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	}()

	attach := func(ref map[string]any) (ProjectResourceResponse, int) {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
			"resource_type": "azure_devops_repo",
			"resource_ref":  ref,
		})
		req = withURLParam(req, "id", project.ID)
		testHandler.CreateProjectResource(w, req)
		var resp ProjectResourceResponse
		json.NewDecoder(w.Body).Decode(&resp)
		return resp, w.Code
	}
	deleteResource := func(resourceID string) {
		r := newRequest("DELETE", "/api/projects/"+project.ID+"/resources/"+resourceID, nil)
		r = withURLParams(r, "id", project.ID, "resourceId", resourceID)
		testHandler.DeleteProjectResource(httptest.NewRecorder(), r)
	}

	// Canonical dev.azure.com URL must succeed.
	resp, code := attach(map[string]any{"url": "https://dev.azure.com/myorg/myproject/_git/myrepo"})
	if code != http.StatusCreated {
		t.Fatalf("canonical URL: expected 201, got %d", code)
	}
	var ref azureDevOpsRepoRef
	if err := json.Unmarshal(resp.ResourceRef, &ref); err != nil {
		t.Fatalf("unmarshal ref: %v", err)
	}
	if ref.Organization != "myorg" || ref.Project != "myproject" || ref.Repository != "myrepo" {
		t.Errorf("parsed fields mismatch: org=%q proj=%q repo=%q", ref.Organization, ref.Project, ref.Repository)
	}
	deleteResource(resp.ID)

	// Legacy visualstudio.com URL must be normalized silently.
	resp, code = attach(map[string]any{"url": "https://myorg.visualstudio.com/myproject/_git/myrepo"})
	if code != http.StatusCreated {
		t.Fatalf("legacy URL: expected 201, got %d", code)
	}
	if err := json.Unmarshal(resp.ResourceRef, &ref); err != nil {
		t.Fatalf("unmarshal legacy ref: %v", err)
	}
	if ref.URL != "https://dev.azure.com/myorg/myproject/_git/myrepo" {
		t.Errorf("legacy URL not normalized: got %q", ref.URL)
	}
	deleteResource(resp.ID)

	// SSH URL must be accepted as-is.
	_, code = attach(map[string]any{"url": "git@ssh.dev.azure.com:v3/myorg/myproject/myrepo"})
	if code != http.StatusCreated {
		t.Errorf("SSH URL: expected 201, got %d", code)
	}

	// Missing URL must reject.
	_, code = attach(map[string]any{})
	if code != http.StatusBadRequest {
		t.Errorf("missing URL: expected 400, got %d", code)
	}

	// Blank URL must reject.
	_, code = attach(map[string]any{"url": "   "})
	if code != http.StatusBadRequest {
		t.Errorf("blank URL: expected 400, got %d", code)
	}

	// Completely invalid URL must reject.
	_, code = attach(map[string]any{"url": "not-a-url"})
	if code != http.StatusBadRequest {
		t.Errorf("invalid URL: expected 400, got %d", code)
	}
}

// ── Integration tests (require DB) ───────────────────────────────────────────

// seedADOInstallation inserts an ado_installation row directly via the DB and
// returns the row including the auto-generated webhook_secret.
func seedADOInstallation(t *testing.T, ctx context.Context, wsID string) db.AdoInstallation {
	t.Helper()
	wsUUID := parseUUID(wsID)
	connectedBy := pgtype.UUID{}
	if u, err := parseStrictUUID(testUserID); err == nil {
		connectedBy = u
	}
	encrypted, err := encryptPAT("test-pat-not-real")
	if err != nil {
		t.Fatalf("encryptPAT: %v", err)
	}
	inst, err := testHandler.Queries.CreateADOInstallation(ctx, db.CreateADOInstallationParams{
		WorkspaceID:   wsUUID,
		OrgURL:        "https://dev.azure.com/test-org",
		DisplayName:   "test-org",
		PatEncrypted:  encrypted,
		ConnectedByID: connectedBy,
	})
	if err != nil {
		t.Fatalf("CreateADOInstallation: %v", err)
	}
	return inst
}

// fireADOWebhook fires an ADO webhook payload to the test handler and asserts the
// expected status code is returned.
func fireADOWebhook(t *testing.T, secret string, payload map[string]any, wantCode int) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	url := "/api/webhooks/azuredevops"
	if secret != "" {
		url += "?secret=" + secret
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	testHandler.HandleADOWebhook(rec, req)
	if rec.Code != wantCode {
		t.Fatalf("HandleADOWebhook: expected %d, got %d (%s)", wantCode, rec.Code, rec.Body.String())
	}
}

// adoPRPayload builds a minimal ADO pull request webhook envelope for testing.
// orgURL is intentionally omitted from the payload because the handler reads it
// from the installation row; only project and repo come from the resource body.
func adoPRPayload(prID int32, identifier, project, repo, eventType, status, description string) map[string]any {
	return map[string]any{
		"eventType": eventType,
		"resource": map[string]any{
			"pullRequestId": prID,
			"title":         "Fix " + identifier,
			"description":   description,
			"status":        status,
			"isDraft":       false,
			"mergeStatus":   "noVote",
			"sourceRefName": "refs/heads/feature/test",
			"creationDate":  "2026-05-01T00:00:00Z",
			"createdBy": map[string]any{
				"displayName": "Test User",
				"uniqueName":  "test@example.com",
				"imageUrl":    "",
			},
			"repository": map[string]any{
				"id":   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
				"name": repo,
				"project": map[string]any{
					"name": project,
				},
			},
			"reviewers": []any{},
		},
	}
}

func TestADOWebhook_MissingSecret(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	raw, _ := json.Marshal(map[string]any{"eventType": "git.pullrequest.created"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/azuredevops", bytes.NewReader(raw))
	testHandler.HandleADOWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing secret: expected 401, got %d", rec.Code)
	}
}

func TestADOWebhook_UnknownSecret(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	fireADOWebhook(t, "definitely-not-a-valid-secret", map[string]any{"eventType": "git.pullrequest.created"}, http.StatusUnauthorized)
}

func TestADOWebhook_UnknownEventTypeAccepted(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	inst := seedADOInstallation(t, ctx, testWorkspaceID)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM ado_installation WHERE id = $1`, uuidToString(inst.ID))
	})
	// An unknown eventType must be accepted (202) without error — the handler
	// ignores unrecognised event types instead of returning 4xx.
	fireADOWebhook(t, inst.WebhookSecret, map[string]any{
		"eventType": "some.future.event",
		"resource":  map[string]any{},
	}, http.StatusAccepted)
}

// TestADOWebhook_PRCreated_LinksIssue fires a git.pullrequest.created event
// with an issue identifier in the description and verifies the PR is upserted
// and the issue linked.
func TestADOWebhook_PRCreated_LinksIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	inst := seedADOInstallation(t, ctx, testWorkspaceID)

	// Create an issue to link.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "ADO webhook link test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_ado_pull_request WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM ado_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM ado_installation WHERE id = $1`, uuidToString(inst.ID))
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	payload := adoPRPayload(101, issue.Identifier,
		"TestProject", "TestRepo",
		"git.pullrequest.created", "active",
		"This fixes "+issue.Identifier)
	fireADOWebhook(t, inst.WebhookSecret, payload, http.StatusAccepted)

	// Verify the issue is linked to an ADO PR.
	rows, err := testHandler.Queries.ListADOPullRequestsByIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("ListADOPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 linked ADO PR, got %d", len(rows))
	}
	if rows[0].PrIDAdo != 101 {
		t.Errorf("linked PR id = %d, want 101", rows[0].PrIDAdo)
	}
}

// TestADOWebhook_PRMerged_AdvancesIssue verifies the full merged-PR flow:
// a created event links the issue, then a merged event with close intent
// advances it to "done".
func TestADOWebhook_PRMerged_AdvancesIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	inst := seedADOInstallation(t, ctx, testWorkspaceID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "ADO merge auto-close test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_ado_pull_request WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM ado_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM ado_installation WHERE id = $1`, uuidToString(inst.ID))
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	// Step 1: PR created — links issue with close_intent=true (Closes keyword).
	created := adoPRPayload(202, issue.Identifier,
		"TestProject", "TestRepo",
		"git.pullrequest.created", "active",
		"Closes "+issue.Identifier)
	fireADOWebhook(t, inst.WebhookSecret, created, http.StatusAccepted)

	// Issue must still be in_progress (PR not yet merged).
	intermediate, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if intermediate.Status != "in_progress" {
		t.Fatalf("issue should stay in_progress before merge, got %q", intermediate.Status)
	}

	// Step 2: PR merged — issue should advance to done.
	mergedPayload := adoPRPayload(202, issue.Identifier,
		"TestProject", "TestRepo",
		"git.pullrequest.merged", "completed",
		"Closes "+issue.Identifier)
	fireADOWebhook(t, inst.WebhookSecret, mergedPayload, http.StatusAccepted)

	final, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("GetIssue after merge: %v", err)
	}
	if final.Status != "done" {
		t.Errorf("expected issue 'done' after PR merged with close intent, got %q", final.Status)
	}
}

// TestADOWebhook_PRMerged_PreservesCancelled guards that a merged PR does not
// re-open or overwrite an already-cancelled issue.
func TestADOWebhook_PRMerged_PreservesCancelled(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	inst := seedADOInstallation(t, ctx, testWorkspaceID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "ADO cancelled issue",
		"status": "cancelled",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_ado_pull_request WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM ado_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM ado_installation WHERE id = $1`, uuidToString(inst.ID))
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	// PR created links the issue.
	fireADOWebhook(t, inst.WebhookSecret, adoPRPayload(303, issue.Identifier,
		"TestProject", "TestRepo",
		"git.pullrequest.created", "active",
		"Closes "+issue.Identifier), http.StatusAccepted)

	// PR merged — cancelled status must survive.
	fireADOWebhook(t, inst.WebhookSecret, adoPRPayload(303, issue.Identifier,
		"TestProject", "TestRepo",
		"git.pullrequest.merged", "completed",
		"Closes "+issue.Identifier), http.StatusAccepted)

	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("expected cancelled status to survive PR merge, got %q", got.Status)
	}
}

// TestADOWebhook_PolicyStatus verifies that the policy_status field on the
// upserted PR row reflects the mergeStatus and reviewer vote aggregation.
func TestADOWebhook_PolicyStatus(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	inst := seedADOInstallation(t, ctx, testWorkspaceID)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM ado_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM ado_installation WHERE id = $1`, uuidToString(inst.ID))
	})

	// PR with a rejected reviewer should be "blocked".
	payload := map[string]any{
		"eventType": "git.pullrequest.created",
		"resource": map[string]any{
			"pullRequestId": 404,
			"title":         "Policy test PR",
			"description":   "",
			"status":        "active",
			"isDraft":       false,
			"mergeStatus":   "noVote",
			"sourceRefName": "refs/heads/feature/policy",
			"creationDate":  "2026-05-01T00:00:00Z",
			"createdBy":     map[string]any{"displayName": "u", "uniqueName": "u@test", "imageUrl": ""},
			"repository": map[string]any{
				"id":   "repo-id-uuid",
				"name": "PolicyRepo",
				"project": map[string]any{
					"name": "PolicyProject",
				},
			},
			"reviewers": []map[string]any{
				{"vote": -10}, // rejected
			},
		},
	}
	raw, _ := json.Marshal(payload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/azuredevops?secret="+inst.WebhookSecret, bytes.NewReader(raw))
	testHandler.HandleADOWebhook(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("HandleADOWebhook: %d %s", rec.Code, rec.Body.String())
	}

	// Look up the PR row.
	pr, err := testHandler.Queries.GetADOPullRequest(ctx, db.GetADOPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		OrgURL:      inst.OrgURL,
		Project:     "PolicyProject",
		RepoName:    "PolicyRepo",
		PrIDAdo:     404,
	})
	if err != nil {
		t.Fatalf("GetADOPullRequest: %v", err)
	}
	if !pr.PolicyStatus.Valid || pr.PolicyStatus.String != "blocked" {
		t.Errorf("policy_status = %v, want blocked", pr.PolicyStatus)
	}
}

// TestADOBuildComplete_UpdatesBuildCheck verifies that a build.complete webhook
// event persists a build check row linked to the correct PR.
func TestADOBuildComplete_UpdatesBuildCheck(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	inst := seedADOInstallation(t, ctx, testWorkspaceID)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM ado_pull_request_build_check WHERE pr_id IN (SELECT id FROM ado_pull_request WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM ado_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM ado_installation WHERE id = $1`, uuidToString(inst.ID))
	})

	// Seed a PR row first (created event).
	createPayload := adoPRPayload(505, "TEST-0",
		"BuildProject", "BuildRepo",
		"git.pullrequest.created", "active", "")
	fireADOWebhook(t, inst.WebhookSecret, createPayload, http.StatusAccepted)

	// Now fire a build.complete for that PR.
	buildPayload := map[string]any{
		"eventType": "build.complete",
		"resource": map[string]any{
			"id":          int64(9001),
			"buildNumber": "20260501.1",
			"status":      "completed",
			"result":      "failed",
			"finishTime":  "2026-05-01T01:00:00Z",
			"definition": map[string]any{
				"id":   42,
				"name": "CI Pipeline",
			},
			"repository": map[string]any{
				"id":   "repo-id-uuid",
				"name": "BuildRepo",
			},
			"triggerInfo": map[string]any{
				"pr.id": fmt.Sprintf("%d", 505),
			},
			"project": map[string]any{
				"name": "BuildProject",
			},
		},
	}
	fireADOWebhook(t, inst.WebhookSecret, buildPayload, http.StatusAccepted)

	// Verify the PR row has build check data via the list query.
	pr, err := testHandler.Queries.GetADOPullRequest(ctx, db.GetADOPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		OrgURL:      inst.OrgURL,
		Project:     "BuildProject",
		RepoName:    "BuildRepo",
		PrIDAdo:     505,
	})
	if err != nil {
		t.Fatalf("GetADOPullRequest: %v", err)
	}

	// Confirm the build check row exists.
	var checkCount int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM ado_pull_request_build_check WHERE pr_id = $1`, pr.ID,
	).Scan(&checkCount); err != nil {
		t.Fatalf("count build checks: %v", err)
	}
	if checkCount == 0 {
		t.Error("expected at least one ado_pull_request_build_check row after build.complete event")
	}
}
