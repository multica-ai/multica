package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// This deliberately uses the production router, PAT authentication, real HTTP,
// and the managed worktree PostgreSQL database, including a source-built CLI.
func TestMemberProvisionRouteAndCLI(t *testing.T) {
	t.Setenv("MULTICA_CLOUD_URL", "")
	t.Setenv("ALLOW_SIGNUP", "false")
	t.Setenv("ALLOWED_EMAIL_DOMAINS", "example.test")
	t.Setenv("ALLOWED_EMAILS", "")
	t.Setenv("MULTICA_DEV_VERIFICATION_CODE", "888888")
	smtp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer smtp.Close()
	var mailAttempts atomic.Int32
	go func() {
		for {
			c, e := smtp.Accept()
			if e != nil {
				return
			}
			mailAttempts.Add(1)
			c.Close()
		}
	}()
	host, port, _ := net.SplitHostPort(smtp.Addr().String())
	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", port)
	t.Setenv("SMTP_FROM_EMAIL", "fixture@example.test")
	t.Setenv("RESEND_API_KEY", "")
	f := testutil.New(testPool, testWorkspaceID, testUserID)
	flags := featureflag.NewStaticProvider()
	flags.Set(featureflags.BulkMemberProvisioning, featureflag.Rule{Default: true})
	router, h := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{FeatureFlags: featureflag.NewService(flags)})
	srv := httptest.NewServer(router)
	defer srv.Close()
	pat := func(userID string) string {
		t.Helper()
		token := "mul_" + uuid.NewString()
		f.Insert(t, "personal_access_token", testutil.Cols{"user_id": userID, "name": "provision fixture", "token_hash": auth.HashToken(token), "token_prefix": token[:12]})
		return token
	}
	ownerToken := pat(testUserID)
	path := "/api/workspaces/" + testWorkspaceID + "/members/provision"
	request := func(token, route string, body any) (int, []byte) {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+route, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", testUserID) // Auth must not trust spoofed identity.
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, e := srv.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, b
	}
	cohort := func(entries ...map[string]any) any { return map[string]any{"entries": entries} }
	email := "route-" + uuid.NewString() + "@example.test"
	// Own generated rows even if the test fails midway through a transaction.
	f.Cleanup(t, `DELETE FROM "user" WHERE email=$1`, email)
	f.Cleanup(t, `DELETE FROM member WHERE user_id IN (SELECT id FROM "user" WHERE email=$1)`, email)
	f.Cleanup(t, `DELETE FROM verification_code WHERE email=$1`, email)
	entry := map[string]any{"email": email, "name": "  First Display  ", "role": "member"}
	for _, tc := range []struct {
		name, token string
		want        int
	}{{"unauthenticated", "", 401}, {"invalid token", "mul_invalid", 401}} {
		t.Run(tc.name, func(t *testing.T) {
			code, b := request(tc.token, path, cohort(entry))
			if code != tc.want {
				t.Fatalf("status=%d body=%s", code, b)
			}
		})
	}
	for _, role := range []string{"member", "admin"} {
		user := f.User(t, role, uuid.NewString()+"@example.test")
		f.Member(t, testWorkspaceID, user, role)
		code, b := request(pat(user), path, cohort(entry))
		if code != 403 {
			t.Fatalf("%s status=%d body=%s", role, code, b)
		}
	}
	flags.Set(featureflags.BulkMemberProvisioning, featureflag.Rule{})
	if code, b := request(ownerToken, path, cohort(entry)); code != 404 {
		t.Fatalf("flag off: %d %s", code, b)
	}
	flags.Set(featureflags.BulkMemberProvisioning, featureflag.Rule{Default: true})
	h.SeatCapacity = seatcapacity.NewUnavailable(fmt.Errorf("fixture unavailable"))
	if code, b := request(ownerToken, path, cohort(entry)); code != 503 {
		t.Fatalf("capacity guard: %d %s", code, b)
	}
	h.SeatCapacity = nil
	f.Insert(t, "workspace_invitation", testutil.Cols{"workspace_id": testWorkspaceID, "inviter_id": testUserID, "invitee_email": email, "role": "admin"})
	for attempt, want := range []string{"created", "already_member"} {
		code, b := request(ownerToken, path, cohort(entry))
		if code != 200 {
			t.Fatalf("attempt %d: %d %s", attempt, code, b)
		}
		var out handler.ProvisionMembersResponse
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if out.Results[0].Status != want {
			t.Fatalf("result=%s", b)
		}
	}
	if code, b := request(ownerToken, path, cohort(entry, entry)); code != 200 {
		t.Fatalf("duplicate batch: %d %s", code, b)
	} else {
		var out handler.ProvisionMembersResponse
		if err := json.Unmarshal(b, &out); err != nil || out.Summary.AlreadyMember != 1 || out.Summary.Duplicate != 1 {
			t.Fatalf("duplicate outcomes: %s %v", b, err)
		}
	}
	var name, role string
	f.QueryRow(t, `SELECT u.name,m.role FROM "user" u JOIN member m ON m.user_id=u.id WHERE email=$1 AND workspace_id=$2`, email, testWorkspaceID).Scan(&name, &role)
	if name != "First Display" || role != "member" {
		t.Fatalf("name=%q role=%q", name, role)
	}
	if n := f.Count(t, `SELECT count(*) FROM workspace_invitation WHERE invitee_email=$1 AND status='pending'`, email); n != 0 {
		t.Fatalf("pending invitations=%d", n)
	}
	if n := f.Count(t, `SELECT count(*) FROM verification_code WHERE email=$1`, email); n != 0 {
		t.Fatal("provisioning created verification code")
	}
	if mailAttempts.Load() != 0 {
		t.Fatal("provisioning attempted outgoing email")
	}
	if code, _ := request("", "/auth/verify-code", map[string]any{"email": email, "code": "888888"}); code == 200 {
		t.Fatal("provisioned account logged in without a code")
	}
	t.Setenv("SMTP_HOST", "")
	h.EmailService = service.NewEmailService()
	if code, b := request("", "/auth/send-code", map[string]any{"email": email}); code != 200 {
		t.Fatalf("normal first login send-code: %d %s", code, b)
	}
	if code, b := request("", "/auth/verify-code", map[string]any{"email": email, "code": "000000"}); code == 200 {
		t.Fatalf("wrong code accepted: %s", b)
	}
	if code, b := request("", "/auth/verify-code", map[string]any{"email": email, "code": "888888"}); code != 200 {
		t.Fatalf("normal verification: %d %s", code, b)
	}
	// Invalid entries never grant owner or create a blocked global account.
	blocked := "blocked-" + uuid.NewString() + "@blocked.test"
	code, b := request(ownerToken, path, cohort(map[string]any{"email": "bad"}, map[string]any{"email": email, "role": "owner"}, map[string]any{"email": email, "role": "nonsense"}, map[string]any{"email": email, "name": " "}, map[string]any{"email": blocked}))
	var invalid handler.ProvisionMembersResponse
	_ = json.Unmarshal(b, &invalid)
	if code != 200 || invalid.Summary.Invalid != 4 || invalid.Summary.Failed != 1 {
		t.Fatalf("invalid cohort: %d %s", code, b)
	}
	if f.Count(t, `SELECT count(*) FROM "user" WHERE email=$1`, blocked) != 0 {
		t.Fatal("allowlist bypassed")
	}
	// Concurrent global creation and membership retry use actual transactions.
	concurrent := "concurrent-" + uuid.NewString() + "@example.test"
	f.Cleanup(t, `DELETE FROM "user" WHERE email=$1`, concurrent)
	f.Cleanup(t, `DELETE FROM member WHERE user_id IN (SELECT id FROM "user" WHERE email=$1)`, concurrent)
	const count = 8
	responses := make([]handler.ProvisionMembersResponse, count)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, b := request(ownerToken, path, cohort(map[string]any{"email": concurrent}))
			if c != 200 {
				t.Errorf("concurrent: %d %s", c, b)
				return
			}
			if e := json.Unmarshal(b, &responses[i]); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	created, existing := 0, 0
	for _, out := range responses {
		created += out.Summary.Created
		existing += out.Summary.AlreadyMember
		if out.Summary.Failed != 0 {
			t.Fatalf("concurrent failure: %+v", out)
		}
	}
	if created != 1 || existing != count-1 {
		t.Fatalf("created=%d already=%d", created, existing)
	}
	// Build and execute the actual command against this real route and database.
	binary := filepath.Join(t.TempDir(), "multica")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../multica")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build CLI: %v %s", e, b)
	}
	cliEmail := "cli-" + uuid.NewString() + "@example.test"
	f.Cleanup(t, `DELETE FROM "user" WHERE email=$1`, cliEmail)
	f.Cleanup(t, `DELETE FROM member WHERE user_id IN (SELECT id FROM "user" WHERE email=$1)`, cliEmail)
	home := t.TempDir()
	for i, ref := range []string{testWorkspaceID, integrationTestWorkspaceSlug, strings.ReplaceAll(testWorkspaceID, "-", "")[:8], ""} {
		args := []string{"workspace", "member", "provision", cliEmail}
		if ref != "" {
			args = append(args, ref)
		}
		requestedName, requestedRole := " CLI Name ", "admin"
		if i > 0 {
			requestedName, requestedRole = "Replacement", "member"
		}
		args = append(args, "--name", requestedName, "--role", requestedRole, "--output", "json", "--server-url", srv.URL)
		cmd := exec.CommandContext(t.Context(), binary, args...)
		defaultWorkspace := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		if ref == "" {
			defaultWorkspace = testWorkspaceID
		}
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "MULTICA_TOKEN=" + ownerToken, "MULTICA_WORKSPACE_ID=" + defaultWorkspace}
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("CLI %s: %v %s", ref, e, b)
		}
		var result map[string]any
		if e := json.Unmarshal(b, &result); e != nil {
			t.Fatalf("CLI JSON: %v %s", e, b)
		}
		if result["status"] != "created" && result["status"] != "already_member" {
			t.Fatalf("CLI outcome: %s", b)
		}
		if result["role"] != "admin" {
			t.Fatalf("CLI changed existing role: %s", b)
		}
		t.Logf("CLI ref=%s status=%v", ref, result["status"])
	}
	var updated time.Time
	f.QueryRow(t, `SELECT u.name,u.updated_at,m.role FROM "user" u JOIN member m ON m.user_id=u.id WHERE email=$1 AND workspace_id=$2`, cliEmail, testWorkspaceID).Scan(&name, &updated, &role)
	if name != "CLI Name" || role != "admin" {
		t.Fatalf("CLI database name=%q role=%q", name, role)
	}
	existingEmail := "existing-" + uuid.NewString() + "@outside.test"
	existingID := f.User(t, "Existing Profile", existingEmail)
	f.Cleanup(t, `DELETE FROM member WHERE user_id=$1`, existingID)
	cmd := exec.CommandContext(t.Context(), binary, "workspace", "member", "provision", existingEmail, testWorkspaceID, "--name", "Ignored", "--output", "json", "--server-url", srv.URL)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "MULTICA_TOKEN=" + ownerToken}
	b, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI existing user: %v %s", err, b)
	}
	var added handler.ProvisionMemberResult
	if err := json.Unmarshal(b, &added); err != nil || added.Status != "added" {
		t.Fatalf("CLI existing outcome: %s %v", b, err)
	}
	f.QueryRow(t, `SELECT name FROM "user" WHERE id=$1`, existingID).Scan(&name)
	if name != "Existing Profile" {
		t.Fatal("CLI overwrote existing profile")
	}
	if mailAttempts.Load() != 0 {
		t.Fatal("provisioning attempted email")
	}
	t.Log("real route/PAT/CLI/PostgreSQL verified; provisioning sent no email; normal first-login verification required")
}
