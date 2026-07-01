package slack

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---- fakes ----

type fakeSlashQueries struct {
	inst      db.ChannelInstallation
	instErr   error
	binding   db.ChannelUserBinding
	bindErr   error
	memberErr error
	ws        db.Workspace
	wsErr     error
	gotAppID  string
}

func (f *fakeSlashQueries) GetChannelInstallationByAppID(_ context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	f.gotAppID = arg.AppID
	return f.inst, f.instErr
}

func (f *fakeSlashQueries) GetChannelUserBindingByUserID(_ context.Context, _ db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	return f.binding, f.bindErr
}

func (f *fakeSlashQueries) GetMemberByUserAndWorkspace(_ context.Context, _ db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return db.Member{}, f.memberErr
}

func (f *fakeSlashQueries) GetWorkspace(_ context.Context, _ pgtype.UUID) (db.Workspace, error) {
	return f.ws, f.wsErr
}

type fakeIssueCreator struct {
	result service.IssueCreateResult
	err    error
	calls  int
	params service.IssueCreateParams
}

func (f *fakeIssueCreator) Create(_ context.Context, p service.IssueCreateParams, _ service.IssueCreateOpts) (service.IssueCreateResult, error) {
	f.calls++
	f.params = p
	return f.result, f.err
}

func slashTestUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

// newTestSlashProcessor builds a processor over fakes and returns it plus a
// pointer to the last ephemeral reply text and the reply count.
func newTestSlashProcessor(q slashQueries, issues engine.IssueCreator, binding bindingMinter) (*SlashCommandProcessor, *string, *int) {
	captured := new(string)
	count := new(int)
	p := &SlashCommandProcessor{
		q:           q,
		issues:      issues,
		binding:     binding,
		appURL:      "https://app.example",
		bindingPath: "/slack/bind",
		logger:      slog.Default(),
	}
	p.respond = func(_ context.Context, _ string, text string) error {
		*count++
		*captured = text
		return nil
	}
	return p, captured, count
}

func activeSlashInstallation() db.ChannelInstallation {
	return db.ChannelInstallation{
		ID:              slashTestUUID(1),
		WorkspaceID:     slashTestUUID(2),
		AgentID:         slashTestUUID(3),
		InstallerUserID: slashTestUUID(4),
		Status:          "active",
		Config:          []byte(`{"app_id":"A1","team_id":"T1"}`),
	}
}

func issueSlashCmd() slack.SlashCommand {
	return slack.SlashCommand{
		Command:     "/issue",
		Text:        "Fix login",
		APIAppID:    "A1",
		TeamID:      "T1",
		UserID:      "U1",
		ChannelID:   "C1",
		ResponseURL: "https://hooks.slack.test/response",
	}
}

// ---- tests ----

func TestSlashHandle_CreatesIssueAndConfirms(t *testing.T) {
	q := &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: slashTestUUID(9)},
		ws:      db.Workspace{IssuePrefix: "MUL"},
	}
	issues := &fakeIssueCreator{result: service.IssueCreateResult{Issue: db.Issue{Number: 7, Title: "Fix login"}}}
	p, captured, count := newTestSlashProcessor(q, issues, &fakeBindingMinter{})

	p.Handle(context.Background(), issueSlashCmd())

	if issues.calls != 1 {
		t.Fatalf("expected 1 issue create, got %d", issues.calls)
	}
	if *count != 1 {
		t.Fatalf("expected 1 ephemeral reply, got %d", *count)
	}
	if !strings.Contains(*captured, "MUL-7") || !strings.Contains(*captured, "Fix login") {
		t.Fatalf("confirmation missing identifier/title: %q", *captured)
	}
	if q.gotAppID != "A1" {
		t.Errorf("installation lookup used app id %q, want A1", q.gotAppID)
	}
	if issues.params.Title != "Fix login" {
		t.Errorf("issue title = %q, want Fix login", issues.params.Title)
	}
	if issues.params.AssigneeID != slashTestUUID(3) {
		t.Errorf("issue not assigned to the installation agent")
	}
	if issues.params.CreatorID != slashTestUUID(9) {
		t.Errorf("issue creator is not the bound member")
	}
	if issues.params.OriginID.Valid {
		t.Errorf("slash-command issue must have no origin session id")
	}
}

func TestSlashHandle_TitleAndDescription(t *testing.T) {
	q := &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: slashTestUUID(9)},
		ws:      db.Workspace{IssuePrefix: "MUL"},
	}
	issues := &fakeIssueCreator{result: service.IssueCreateResult{Issue: db.Issue{Number: 1, Title: "Title"}}}
	p, _, _ := newTestSlashProcessor(q, issues, &fakeBindingMinter{})

	cmd := issueSlashCmd()
	cmd.Text = "Title\nline one\nline two"
	p.Handle(context.Background(), cmd)

	if issues.params.Title != "Title" {
		t.Errorf("title = %q, want Title", issues.params.Title)
	}
	if got := issues.params.Description.String; got != "line one\nline two" {
		t.Errorf("description = %q, want two body lines", got)
	}
}

func TestSlashHandle_EmptyTitleIsUsage(t *testing.T) {
	issues := &fakeIssueCreator{}
	p, captured, count := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, issues, &fakeBindingMinter{})

	cmd := issueSlashCmd()
	cmd.Text = "   "
	p.Handle(context.Background(), cmd)

	if issues.calls != 0 {
		t.Fatalf("empty title must not create an issue")
	}
	if *count != 1 || *captured != slashUsageText {
		t.Fatalf("expected usage reply, got %q", *captured)
	}
}

func TestSlashHandle_UnboundUserGetsLink(t *testing.T) {
	q := &fakeSlashQueries{inst: activeSlashInstallation(), bindErr: pgx.ErrNoRows}
	issues := &fakeIssueCreator{}
	bind := &fakeBindingMinter{raw: "TOKEN123"}
	p, captured, _ := newTestSlashProcessor(q, issues, bind)

	p.Handle(context.Background(), issueSlashCmd())

	if issues.calls != 0 {
		t.Fatalf("unbound user must not create an issue")
	}
	if bind.calls != 1 {
		t.Fatalf("expected a binding token to be minted, got %d", bind.calls)
	}
	if !strings.Contains(*captured, "link your account") || !strings.Contains(*captured, "TOKEN123") {
		t.Fatalf("reply missing bind link: %q", *captured)
	}
}

func TestSlashHandle_NonMemberDropped(t *testing.T) {
	q := &fakeSlashQueries{
		inst:      activeSlashInstallation(),
		binding:   db.ChannelUserBinding{MulticaUserID: slashTestUUID(9)},
		memberErr: pgx.ErrNoRows,
	}
	issues := &fakeIssueCreator{}
	p, captured, _ := newTestSlashProcessor(q, issues, &fakeBindingMinter{})

	p.Handle(context.Background(), issueSlashCmd())

	if issues.calls != 0 {
		t.Fatalf("non-member must not create an issue")
	}
	if *captured != slashNotMemberText {
		t.Fatalf("expected not-member reply, got %q", *captured)
	}
}

func TestSlashHandle_InactiveInstallation(t *testing.T) {
	inst := activeSlashInstallation()
	inst.Status = "revoked"
	issues := &fakeIssueCreator{}
	p, captured, _ := newTestSlashProcessor(&fakeSlashQueries{inst: inst}, issues, &fakeBindingMinter{})

	p.Handle(context.Background(), issueSlashCmd())

	if issues.calls != 0 || *captured != slashDisabledText {
		t.Fatalf("inactive install: calls=%d reply=%q", issues.calls, *captured)
	}
}

func TestSlashHandle_TeamMismatchTreatedAsDisconnected(t *testing.T) {
	issues := &fakeIssueCreator{}
	p, captured, _ := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, issues, &fakeBindingMinter{})

	cmd := issueSlashCmd()
	cmd.TeamID = "T2" // config team is T1
	p.Handle(context.Background(), cmd)

	if issues.calls != 0 || *captured != slashDisabledText {
		t.Fatalf("team mismatch: calls=%d reply=%q", issues.calls, *captured)
	}
}

func TestSlashHandle_IgnoresOtherCommands(t *testing.T) {
	issues := &fakeIssueCreator{}
	p, _, count := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, issues, &fakeBindingMinter{})

	cmd := issueSlashCmd()
	cmd.Command = "/other"
	p.Handle(context.Background(), cmd)

	if issues.calls != 0 || *count != 0 {
		t.Fatalf("non-/issue command must be ignored: calls=%d replies=%d", issues.calls, *count)
	}
}

func TestSplitIssueText(t *testing.T) {
	cases := []struct {
		in, title, desc string
	}{
		{"Fix login", "Fix login", ""},
		{"  Fix login  ", "Fix login", ""},
		{"Title\nbody one\nbody two", "Title", "body one\nbody two"},
		{"", "", ""},
		{"   \n  ", "", ""},
		{"\n\nTitle\nbody", "Title", "body"},
	}
	for _, c := range cases {
		gotTitle, gotDesc := splitIssueText(c.in)
		if gotTitle != c.title || gotDesc != c.desc {
			t.Errorf("splitIssueText(%q) = (%q,%q), want (%q,%q)", c.in, gotTitle, gotDesc, c.title, c.desc)
		}
	}
}
