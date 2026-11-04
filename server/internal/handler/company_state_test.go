package handler

import (
	"context"
	csvpkg "encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// companyWorkspace is a workspace of its own with an owner, an admin and a plain
// member. Its governance rows are removed on cleanup, since the log has no
// foreign key that would remove them with the workspace.
type companyWorkspace struct {
	ID     string
	Owner  string
	Admin  string
	Member string
}

func newCompanyWorkspace(t *testing.T) companyWorkspace {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fx := companyWorkspace{}
	fx.ID = dbfx.Workspace(t, "Company "+suffix, "company-"+suffix)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM governance_event WHERE workspace_id = $1`, fx.ID)
	})
	fx.Owner = dbfx.User(t, "Company Owner "+suffix, "company-owner-"+suffix+"@example.com")
	dbfx.Member(t, fx.ID, fx.Owner, "owner")
	fx.Admin = dbfx.User(t, "Company Admin "+suffix, "company-admin-"+suffix+"@example.com")
	dbfx.Member(t, fx.ID, fx.Admin, "admin")
	fx.Member = dbfx.User(t, "Company Member "+suffix, "company-member-"+suffix+"@example.com")
	dbfx.Member(t, fx.ID, fx.Member, "member")
	return fx
}

// companyReq is a request as userID in workspace wsID.
func companyReq(userID, wsID, method, path string, body any) *http.Request {
	req := newRequestAs(userID, method, path, body)
	req.Header.Set("X-Workspace-ID", wsID)
	return req
}

func TestCompanyState_ReadsAreOpenToMembersAndDefaultToRunning(t *testing.T) {
	fx := newCompanyWorkspace(t)
	var out CompanyStateResponse
	testutil.Call(t, testHandler.GetCompanyState, companyReq(fx.Member, fx.ID, http.MethodGet, "/api/office/company-state", nil)).
		Want(http.StatusOK).JSON(&out)
	if out.State != "running" || out.Setting != "running" {
		t.Fatalf("a new company is %+v, want running", out)
	}
	if out.QuietHours.Enabled || out.QuietHours.Start != "22:00" || out.QuietHours.End != "07:00" || out.QuietHours.Timezone != "UTC" {
		t.Fatalf("default quiet hours = %+v, want off, 22:00 to 07:00 UTC", out.QuietHours)
	}
	if out.UpdatedAt.IsZero() {
		t.Fatal("updated_at is missing")
	}
}

func TestCompanyState_OnlyOwnersAndAdminsChangeIt(t *testing.T) {
	fx := newCompanyWorkspace(t)
	put := func(userID string, body any) *testutil.Response {
		return testutil.Call(t, testHandler.SetCompanyState,
			companyReq(userID, fx.ID, http.MethodPut, "/api/office/company-state", body))
	}

	put(fx.Member, map[string]any{"state": "paused"}).Want(http.StatusForbidden)

	var out CompanyStateResponse
	put(fx.Admin, map[string]any{"state": "paused"}).Want(http.StatusOK).JSON(&out)
	if out.State != "paused" || out.Setting != "paused" {
		t.Fatalf("an admin's pause returned %+v", out)
	}
	put(fx.Owner, map[string]any{"state": "running"}).Want(http.StatusOK).JSON(&out)
	if out.State != "running" {
		t.Fatalf("an owner's resume returned %+v", out)
	}
}

func TestCompanyState_ValidatesTheStateAndStopRunning(t *testing.T) {
	fx := newCompanyWorkspace(t)
	put := func(body any) *testutil.Response {
		return testutil.Call(t, testHandler.SetCompanyState,
			companyReq(fx.Owner, fx.ID, http.MethodPut, "/api/office/company-state", body))
	}
	put(map[string]any{"state": "asleep"}).Want(http.StatusBadRequest)
	put(map[string]any{"state": "running", "stop_running": true}).Want(http.StatusBadRequest)
	put(map[string]any{"state": "quiet"}).Want(http.StatusOK)
}

func TestCompanyState_PauseAndStopPausesTheCompany(t *testing.T) {
	fx := newCompanyWorkspace(t)
	var out CompanyStateResponse
	testutil.Call(t, testHandler.SetCompanyState,
		companyReq(fx.Owner, fx.ID, http.MethodPut, "/api/office/company-state", map[string]any{"state": "paused", "stop_running": true})).
		Want(http.StatusOK).JSON(&out)
	if out.State != "paused" {
		t.Fatalf("pause and stop returned %+v, want paused", out)
	}
	var stored string
	dbfx.QueryRow(t, `SELECT company_state FROM workspace WHERE id = $1`, fx.ID).Scan(&stored)
	if stored != "paused" {
		t.Fatalf("stored state = %q, want paused", stored)
	}
}

func TestQuietHours_SavedByOwnersAndAdminsAndValidated(t *testing.T) {
	fx := newCompanyWorkspace(t)
	quiet := map[string]any{"enabled": true, "start": "22:00", "end": "07:00", "timezone": "Europe/Paris"}
	put := func(userID string, body any) *testutil.Response {
		return testutil.Call(t, testHandler.SetQuietHours,
			companyReq(userID, fx.ID, http.MethodPut, "/api/office/quiet-hours", body))
	}

	put(fx.Member, quiet).Want(http.StatusForbidden)

	var out CompanyStateResponse
	put(fx.Owner, quiet).Want(http.StatusOK).JSON(&out)
	if !out.QuietHours.Enabled || out.QuietHours.Start != "22:00" || out.QuietHours.End != "07:00" || out.QuietHours.Timezone != "Europe/Paris" {
		t.Fatalf("saved quiet hours = %+v", out.QuietHours)
	}
	put(fx.Owner, map[string]any{"enabled": true, "start": "22:00", "end": "07:00", "timezone": "Mars/Olympus"}).Want(http.StatusBadRequest)
	put(fx.Owner, map[string]any{"enabled": true, "start": "25:00", "end": "07:00", "timezone": "UTC"}).Want(http.StatusBadRequest)
	put(fx.Owner, map[string]any{"enabled": true, "start": "07:00", "end": "07:00", "timezone": "UTC"}).Want(http.StatusBadRequest)
}

func TestGovernance_ListFiltersPagesAndExportsCSV(t *testing.T) {
	fx := newCompanyWorkspace(t)
	testutil.Call(t, testHandler.SetCompanyState,
		companyReq(fx.Owner, fx.ID, http.MethodPut, "/api/office/company-state", map[string]any{"state": "paused"})).Want(http.StatusOK)
	testutil.Call(t, testHandler.SetCompanyState,
		companyReq(fx.Owner, fx.ID, http.MethodPut, "/api/office/company-state", map[string]any{"state": "running"})).Want(http.StatusOK)

	list := func(userID, query string) *testutil.Response {
		return testutil.Call(t, testHandler.ListGovernanceEvents,
			companyReq(userID, fx.ID, http.MethodGet, "/api/governance/events"+query, nil))
	}

	// Members read the log; the two control changes are the only entries.
	var page ListGovernanceEventsResponse
	list(fx.Member, "").Want(http.StatusOK).JSON(&page)
	if len(page.Events) != 2 || page.NextCursor != "" {
		t.Fatalf("log has %d events, cursor %q; want 2 and no cursor", len(page.Events), page.NextCursor)
	}
	if page.Events[0].Kind != "company_resumed" || page.Events[1].Kind != "company_paused" {
		t.Fatalf("log is %q then %q, want newest first: company_resumed then company_paused", page.Events[0].Kind, page.Events[1].Kind)
	}
	if page.Events[0].ActorID == nil || *page.Events[0].ActorID != fx.Owner || page.Events[0].ActorType != "member" {
		t.Fatalf("the resume's actor is %q %v, want the owner", page.Events[0].ActorType, page.Events[0].ActorID)
	}

	var paused ListGovernanceEventsResponse
	list(fx.Member, "?kind=company_paused").Want(http.StatusOK).JSON(&paused)
	if len(paused.Events) != 1 {
		t.Fatalf("kind filter returned %d events, want 1", len(paused.Events))
	}
	var none ListGovernanceEventsResponse
	list(fx.Member, "?actor_type=agent").Want(http.StatusOK).JSON(&none)
	if len(none.Events) != 0 {
		t.Fatalf("actor filter for agents returned %d events, want 0", len(none.Events))
	}
	list(fx.Member, "?subject_id="+fx.ID).Want(http.StatusOK).JSON(&none)
	if len(none.Events) != 2 {
		t.Fatalf("subject filter returned %d events, want 2", len(none.Events))
	}

	// Paging: one event per page, then the second page ends the listing.
	var first, second ListGovernanceEventsResponse
	list(fx.Member, "?limit=1").Want(http.StatusOK).JSON(&first)
	if len(first.Events) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %d events, cursor %q", len(first.Events), first.NextCursor)
	}
	list(fx.Member, "?limit=1&cursor="+first.NextCursor).Want(http.StatusOK).JSON(&second)
	if len(second.Events) != 1 || second.Events[0].ID == first.Events[0].ID || second.NextCursor != "" {
		t.Fatalf("second page = %+v, cursor %q", second.Events, second.NextCursor)
	}

	list(fx.Member, "?kind=made_up").Want(http.StatusBadRequest)
	list(fx.Member, "?actor_type=robot").Want(http.StatusBadRequest)
	list(fx.Member, "?subject_id=not-a-uuid").Want(http.StatusBadRequest)
	list(fx.Member, "?limit=0").Want(http.StatusBadRequest)
	list(fx.Member, "?limit=201").Want(http.StatusBadRequest)
	list(fx.Member, "?cursor=garbage!").Want(http.StatusBadRequest)

	csvOut := testutil.Call(t, testHandler.ExportGovernanceEvents,
		companyReq(fx.Member, fx.ID, http.MethodGet, "/api/governance/events.csv", nil)).Want(http.StatusOK)
	if ct := csvOut.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content type = %q, want text/csv", ct)
	}
	if cd := csvOut.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("content disposition = %q, want an attachment", cd)
	}
	lines := strings.Split(strings.TrimSpace(csvOut.Text()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "id,created_at,kind,actor_type,actor_id") {
		t.Fatalf("CSV = %q, want a header and the two events", csvOut.Text())
	}
	if !strings.Contains(lines[1], "company_resumed") || !strings.Contains(lines[2], "company_paused") {
		t.Fatalf("CSV rows out of order: %q", csvOut.Text())
	}
	rows, err := csvpkg.NewReader(strings.NewReader(csvOut.Text())).ReadAll()
	if err != nil {
		t.Fatalf("CSV does not parse: %v", err)
	}
	var details map[string]string
	if err := json.Unmarshal([]byte(rows[2][8]), &details); err != nil || details["from"] != "running" || details["to"] != "paused" {
		t.Fatalf("pause row details = %q (%v), want the from and to states", rows[2][8], err)
	}
}
