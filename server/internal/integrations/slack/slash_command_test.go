package slack

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---- fakes ----

type fakeSlashQueries struct {
	inst      db.ChannelInstallation
	instErr   error
	binding   db.ChannelUserBinding
	bindErr   error
	memberErr error
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

// fakeQuickCreate records the last EnqueueQuickCreateTask call so tests can
// assert the prompt is passed through verbatim and attributed correctly.
type fakeQuickCreate struct {
	task  db.AgentTaskQueue
	err   error
	calls int

	workspaceID pgtype.UUID
	requesterID pgtype.UUID
	agentID     pgtype.UUID
	squadID     pgtype.UUID
	prompt      string
}

func (f *fakeQuickCreate) EnqueueQuickCreateTask(_ context.Context, workspaceID, requesterID, agentID, squadID pgtype.UUID, prompt string, _, _ pgtype.UUID, _ []pgtype.UUID) (db.AgentTaskQueue, error) {
	f.calls++
	f.workspaceID = workspaceID
	f.requesterID = requesterID
	f.agentID = agentID
	f.squadID = squadID
	f.prompt = prompt
	return f.task, f.err
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
func newTestSlashProcessor(q slashQueries, tasks quickCreateEnqueuer, binding bindingMinter) (*SlashCommandProcessor, *string, *int) {
	captured := new(string)
	count := new(int)
	p := &SlashCommandProcessor{
		q:           q,
		tasks:       tasks,
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

func TestSlashHandle_EnqueuesQuickCreateAndAcks(t *testing.T) {
	q := &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: slashTestUUID(9)},
	}
	tasks := &fakeQuickCreate{}
	p, captured, count := newTestSlashProcessor(q, tasks, &fakeBindingMinter{})

	p.Handle(context.Background(), issueSlashCmd())

	if tasks.calls != 1 {
		t.Fatalf("expected 1 quick-create enqueue, got %d", tasks.calls)
	}
	if *count != 1 {
		t.Fatalf("expected 1 ephemeral reply, got %d", *count)
	}
	if *captured != slashQueuedText {
		t.Fatalf("expected queued ack, got %q", *captured)
	}
	if q.gotAppID != "A1" {
		t.Errorf("installation lookup used app id %q, want A1", q.gotAppID)
	}
	if tasks.prompt != "Fix login" {
		t.Errorf("quick-create prompt = %q, want Fix login", tasks.prompt)
	}
	if tasks.workspaceID != slashTestUUID(2) {
		t.Errorf("quick-create workspace is not the installation workspace")
	}
	if tasks.agentID != slashTestUUID(3) {
		t.Errorf("quick-create not dispatched to the installation agent")
	}
	if tasks.requesterID != slashTestUUID(9) {
		t.Errorf("quick-create requester is not the bound member")
	}
	if tasks.squadID.Valid {
		t.Errorf("slash-command quick-create must not carry a squad id")
	}
}

func TestSlashHandle_MultilinePromptPassedThrough(t *testing.T) {
	q := &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: slashTestUUID(9)},
	}
	tasks := &fakeQuickCreate{}
	p, _, _ := newTestSlashProcessor(q, tasks, &fakeBindingMinter{})

	cmd := issueSlashCmd()
	cmd.Text = "  Title\nline one\nline two  "
	p.Handle(context.Background(), cmd)

	// The whole (trimmed) natural-language text is the prompt — no title/body
	// split; the agent authors the well-formed issue from it.
	if tasks.prompt != "Title\nline one\nline two" {
		t.Errorf("prompt = %q, want the full trimmed text", tasks.prompt)
	}
}

func TestSlashHandle_EmptyPromptIsUsage(t *testing.T) {
	tasks := &fakeQuickCreate{}
	p, captured, count := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, tasks, &fakeBindingMinter{})

	cmd := issueSlashCmd()
	cmd.Text = "   "
	p.Handle(context.Background(), cmd)

	if tasks.calls != 0 {
		t.Fatalf("empty prompt must not enqueue a task")
	}
	if *count != 1 || *captured != slashUsageText {
		t.Fatalf("expected usage reply, got %q", *captured)
	}
}

func TestSlashHandle_UnboundUserGetsLink(t *testing.T) {
	q := &fakeSlashQueries{inst: activeSlashInstallation(), bindErr: pgx.ErrNoRows}
	tasks := &fakeQuickCreate{}
	bind := &fakeBindingMinter{raw: "TOKEN123"}
	p, captured, _ := newTestSlashProcessor(q, tasks, bind)

	p.Handle(context.Background(), issueSlashCmd())

	if tasks.calls != 0 {
		t.Fatalf("unbound user must not enqueue a task")
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
	tasks := &fakeQuickCreate{}
	p, captured, _ := newTestSlashProcessor(q, tasks, &fakeBindingMinter{})

	p.Handle(context.Background(), issueSlashCmd())

	if tasks.calls != 0 {
		t.Fatalf("non-member must not enqueue a task")
	}
	if *captured != slashNotMemberText {
		t.Fatalf("expected not-member reply, got %q", *captured)
	}
}

func TestSlashHandle_InactiveInstallation(t *testing.T) {
	inst := activeSlashInstallation()
	inst.Status = "revoked"
	tasks := &fakeQuickCreate{}
	p, captured, _ := newTestSlashProcessor(&fakeSlashQueries{inst: inst}, tasks, &fakeBindingMinter{})

	p.Handle(context.Background(), issueSlashCmd())

	if tasks.calls != 0 || *captured != slashDisabledText {
		t.Fatalf("inactive install: calls=%d reply=%q", tasks.calls, *captured)
	}
}

func TestSlashHandle_TeamMismatchTreatedAsDisconnected(t *testing.T) {
	tasks := &fakeQuickCreate{}
	p, captured, _ := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, tasks, &fakeBindingMinter{})

	cmd := issueSlashCmd()
	cmd.TeamID = "T2" // config team is T1
	p.Handle(context.Background(), cmd)

	if tasks.calls != 0 || *captured != slashDisabledText {
		t.Fatalf("team mismatch: calls=%d reply=%q", tasks.calls, *captured)
	}
}

func TestSlashHandle_EnqueueFailureIsInternalError(t *testing.T) {
	q := &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: slashTestUUID(9)},
	}
	tasks := &fakeQuickCreate{err: errors.New("agent has no runtime")}
	p, captured, _ := newTestSlashProcessor(q, tasks, &fakeBindingMinter{})

	p.Handle(context.Background(), issueSlashCmd())

	if tasks.calls != 1 {
		t.Fatalf("expected the enqueue to be attempted once, got %d", tasks.calls)
	}
	if *captured != slashInternalErrorText {
		t.Fatalf("expected internal-error reply, got %q", *captured)
	}
}

func TestSlashHandle_IgnoresOtherCommands(t *testing.T) {
	tasks := &fakeQuickCreate{}
	p, _, count := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, tasks, &fakeBindingMinter{})

	cmd := issueSlashCmd()
	cmd.Command = "/other"
	p.Handle(context.Background(), cmd)

	if tasks.calls != 0 || *count != 0 {
		t.Fatalf("non-/issue command must be ignored: calls=%d replies=%d", tasks.calls, *count)
	}
}
