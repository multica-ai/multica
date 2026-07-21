package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
)

func cleanupProvisionedEmails(t *testing.T, emails ...string) {
	t.Helper()
	ctx := context.Background()
	for _, email := range emails {
		normalized := strings.ToLower(strings.TrimSpace(email))
		if _, err := testPool.Exec(ctx, `DELETE FROM workspace_invitation WHERE workspace_id = $1 AND invitee_email = $2`, testWorkspaceID, normalized); err != nil {
			t.Fatalf("clean invitation for %s: %v", normalized, err)
		}
		if _, err := testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id IN (SELECT id FROM "user" WHERE email = $2)`, testWorkspaceID, normalized); err != nil {
			t.Fatalf("clean member for %s: %v", normalized, err)
		}
		if _, err := testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, normalized); err != nil {
			t.Fatalf("clean user for %s: %v", normalized, err)
		}
	}
	t.Cleanup(func() {
		for _, email := range emails {
			normalized := strings.ToLower(strings.TrimSpace(email))
			testPool.Exec(context.Background(), `DELETE FROM workspace_invitation WHERE workspace_id = $1 AND invitee_email = $2`, testWorkspaceID, normalized)
			testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id IN (SELECT id FROM "user" WHERE email = $2)`, testWorkspaceID, normalized)
			testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, normalized)
		}
	})
}

func provisionMembersRequest(entries []ProvisionMemberEntry) *http.Request {
	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members/provision", ProvisionMembersRequest{Entries: entries})
	return withURLParam(req, "id", testWorkspaceID)
}

func decodeProvisionMembersResponse(t *testing.T, w *httptest.ResponseRecorder) ProvisionMembersResponse {
	t.Helper()
	var resp ProvisionMembersResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	return resp
}

func TestNormalizeProvisioningEmail(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "normalizes", input: " Seed@Company.COM ", want: "seed@company.com"},
		{name: "empty", input: "  ", wantErr: true},
		{name: "invalid", input: "not-an-email", wantErr: true},
		{name: "display name rejected", input: "Seed <seed@company.com>", wantErr: true},
		{name: "too long", input: strings.Repeat("a", 250) + "@x.com", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeProvisioningEmail(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeProvisioningEmail(%q) error=%v, wantErr=%v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("normalizeProvisioningEmail(%q)=%q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeProvisioningRole(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "", want: "member", ok: true},
		{input: " ADMIN ", want: "admin", ok: true},
		{input: "owner", want: "owner", ok: false},
	} {
		got, ok := normalizeProvisioningRole(tt.input)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("normalizeProvisioningRole(%q)=(%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestProvisioningEmailAllowed(t *testing.T) {
	h := &Handler{}
	if !h.provisioningEmailAllowed("anywhere@example.com") {
		t.Fatal("empty allowlists should permit provisioning")
	}
	h.cfg.AllowedEmails = []string{"seed@company.com"}
	if !h.provisioningEmailAllowed("SEED@company.com") || h.provisioningEmailAllowed("other@company.com") {
		t.Fatal("explicit email allowlist was not enforced")
	}
	h.cfg.AllowedEmails = nil
	h.cfg.AllowedEmailDomains = []string{"company.com"}
	if !h.provisioningEmailAllowed("seed@COMPANY.com") || h.provisioningEmailAllowed("seed@other.com") {
		t.Fatal("domain allowlist was not enforced")
	}
}

func TestProvisionMemberPublicError(t *testing.T) {
	if got := provisionMemberPublicError(errProvisioningEmailNotAllowed); got != errProvisioningEmailNotAllowed.Error() {
		t.Fatalf("allowlist error=%q", got)
	}
	if got := provisionMemberPublicError(errors.New("database details")); got != "failed to provision member" {
		t.Fatalf("internal error leaked as %q", got)
	}
}

func TestProvisionMembers_FeatureFlagDefaultsOff(t *testing.T) {
	req := provisionMembersRequest([]ProvisionMemberEntry{{Email: "seed-off@multica.ai", Role: "member"}})
	w := httptest.NewRecorder()

	testHandler.ProvisionMembers(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 while feature is disabled, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProvisionMembers_CreatesSeedUsersAndMemberships(t *testing.T) {
	newEmail := "seed-new@multica.ai"
	existingEmail := "seed-existing@multica.ai"
	cleanupProvisionedEmails(t, newEmail, existingEmail)
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)

	var existingUserID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ('Existing Seed', $1)
		RETURNING id
	`, existingEmail).Scan(&existingUserID); err != nil {
		t.Fatalf("seed existing user: %v", err)
	}

	req := provisionMembersRequest([]ProvisionMemberEntry{
		{Email: " SEED-NEW@MULTICA.AI ", Role: "member"},
		{Email: existingEmail, Role: "admin"},
	})
	w := httptest.NewRecorder()
	testHandler.ProvisionMembers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeProvisionMembersResponse(t, w)
	if resp.Summary.Total != 2 || resp.Summary.Created != 2 || resp.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", resp.Summary)
	}

	for email, wantRole := range map[string]string{newEmail: "member", existingEmail: "admin"} {
		var gotRole string
		var onboarded bool
		if err := testPool.QueryRow(context.Background(), `
			SELECT m.role, u.onboarded_at IS NOT NULL
			FROM "user" u
			JOIN member m ON m.user_id = u.id
			WHERE u.email = $1 AND m.workspace_id = $2
		`, email, testWorkspaceID).Scan(&gotRole, &onboarded); err != nil {
			t.Fatalf("load provisioned member %s: %v", email, err)
		}
		if gotRole != wantRole || !onboarded {
			t.Fatalf("member %s: role=%q onboarded=%v; want role=%q onboarded=true", email, gotRole, onboarded, wantRole)
		}
	}
}

func TestProvisionMembers_IsIdempotentAndReconcilesPendingInvitation(t *testing.T) {
	email := "seed-retry@multica.ai"
	cleanupProvisionedEmails(t, email)
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace_invitation (workspace_id, inviter_id, invitee_email, role)
		VALUES ($1, $2, $3, 'member')
	`, testWorkspaceID, testUserID, email); err != nil {
		t.Fatalf("seed invitation: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		req := provisionMembersRequest([]ProvisionMemberEntry{{Email: email, Role: "member"}})
		w := httptest.NewRecorder()
		testHandler.ProvisionMembers(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d: expected 200, got %d: %s", attempt, w.Code, w.Body.String())
		}
		resp := decodeProvisionMembersResponse(t, w)
		if attempt == 1 && resp.Results[0].Status != ProvisionMemberStatusCreated {
			t.Fatalf("first attempt status=%q, want created", resp.Results[0].Status)
		}
		if attempt == 2 && resp.Results[0].Status != ProvisionMemberStatusAlreadyMember {
			t.Fatalf("second attempt status=%q, want already_member", resp.Results[0].Status)
		}
	}

	var pending int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM workspace_invitation
		WHERE workspace_id = $1 AND invitee_email = $2 AND status = 'pending'
	`, testWorkspaceID, email).Scan(&pending); err != nil {
		t.Fatalf("count pending invitations: %v", err)
	}
	if pending != 0 {
		t.Fatalf("expected pending invitation to be removed, got %d", pending)
	}
}

func TestProvisionMembers_ConcurrentRetriesRemainIdempotent(t *testing.T) {
	email := "seed-concurrent@multica.ai"
	cleanupProvisionedEmails(t, email)
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)

	const requests = 8
	start := make(chan struct{})
	responses := make([]*httptest.ResponseRecorder, requests)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			testHandler.ProvisionMembers(w, provisionMembersRequest([]ProvisionMemberEntry{{Email: email, Role: "member"}}))
			responses[index] = w
		}(i)
	}
	close(start)
	wg.Wait()

	created := 0
	alreadyMember := 0
	for i, w := range responses {
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
		resp := decodeProvisionMembersResponse(t, w)
		if resp.Summary.Failed != 0 {
			t.Fatalf("request %d failed: %+v", i, resp.Results)
		}
		created += resp.Summary.Created
		alreadyMember += resp.Summary.AlreadyMember
	}
	if created != 1 || alreadyMember != requests-1 {
		t.Fatalf("created=%d already_member=%d, want 1 and %d", created, alreadyMember, requests-1)
	}
}

func TestProvisionMembers_ReturnsPerEntryOutcomes(t *testing.T) {
	email := "seed-dedupe@multica.ai"
	cleanupProvisionedEmails(t, email)
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)

	entries := []ProvisionMemberEntry{
		{Email: email, Role: "member"},
		{Email: strings.ToUpper(email), Role: "member"},
		{Email: "not-an-email", Role: "member"},
		{Email: "owner-role@multica.ai", Role: "owner"},
	}
	cleanupProvisionedEmails(t, "owner-role@multica.ai")
	req := provisionMembersRequest(entries)
	w := httptest.NewRecorder()
	testHandler.ProvisionMembers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeProvisionMembersResponse(t, w)
	if resp.Summary.Total != 4 || resp.Summary.Created != 1 || resp.Summary.Duplicate != 1 || resp.Summary.Invalid != 2 {
		t.Fatalf("unexpected summary: %+v", resp.Summary)
	}
	wantStatuses := []string{ProvisionMemberStatusCreated, ProvisionMemberStatusDuplicate, ProvisionMemberStatusInvalid, ProvisionMemberStatusInvalid}
	for i, want := range wantStatuses {
		if resp.Results[i].Status != want {
			t.Errorf("result[%d].status=%q, want %q", i, resp.Results[i].Status, want)
		}
	}
}

func TestProvisionMembers_ContinuesAfterDisallowedNewAccount(t *testing.T) {
	allowedEmail := "seed-allowed@company.com"
	blockedEmail := "seed-blocked@other.com"
	cleanupProvisionedEmails(t, allowedEmail, blockedEmail)
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)

	originalConfig := testHandler.cfg
	testHandler.cfg.AllowSignup = false
	testHandler.cfg.AllowedEmails = nil
	testHandler.cfg.AllowedEmailDomains = []string{"company.com"}
	t.Cleanup(func() { testHandler.cfg = originalConfig })

	req := provisionMembersRequest([]ProvisionMemberEntry{
		{Email: blockedEmail, Role: "member"},
		{Email: allowedEmail, Role: "member"},
	})
	w := httptest.NewRecorder()
	testHandler.ProvisionMembers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeProvisionMembersResponse(t, w)
	if resp.Summary.Created != 1 || resp.Summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", resp.Summary)
	}
	if resp.Results[0].Status != ProvisionMemberStatusFailed || resp.Results[1].Status != ProvisionMemberStatusCreated {
		t.Fatalf("unexpected outcomes: %+v", resp.Results)
	}

	var blockedUsers int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM "user" WHERE email = $1`, blockedEmail).Scan(&blockedUsers); err != nil {
		t.Fatalf("count blocked users: %v", err)
	}
	if blockedUsers != 0 {
		t.Fatalf("disallowed entry created %d users", blockedUsers)
	}
}

func TestProvisionMembers_RequiresOwner(t *testing.T) {
	email := "seed-admin-denied@multica.ai"
	adminEmail := "seed-admin-actor@multica.ai"
	cleanupProvisionedEmails(t, email, adminEmail)
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)

	var adminUserID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ('Seed Admin', $1) RETURNING id
	`, adminEmail).Scan(&adminUserID); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')
	`, testWorkspaceID, adminUserID); err != nil {
		t.Fatalf("create admin member: %v", err)
	}

	req := provisionMembersRequest([]ProvisionMemberEntry{{Email: email, Role: "member"}})
	req.Header.Set("X-User-ID", adminUserID)
	w := httptest.NewRecorder()
	testHandler.ProvisionMembers(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM "user" WHERE email = $1`, email).Scan(&count); err != nil {
		t.Fatalf("count denied user: %v", err)
	}
	if count != 0 {
		t.Fatalf("denied request created %d users", count)
	}
}

func TestProvisionMembers_RejectsOversizedBatch(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.BulkMemberProvisioning, true)
	entries := make([]ProvisionMemberEntry, maxProvisionMembersPerRequest+1)
	for i := range entries {
		entries[i] = ProvisionMemberEntry{Email: fmt.Sprintf("seed-%03d@multica.ai", i), Role: "member"}
	}
	req := provisionMembersRequest(entries)
	w := httptest.NewRecorder()
	testHandler.ProvisionMembers(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
