package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---- fakes ----

type fakeIssueQueries struct {
	inst      db.ChannelInstallation
	instErr   error
	binding   db.ChannelUserBinding
	bindErr   error
	memberErr error
	gotAppID  string
}

func (f *fakeIssueQueries) GetChannelInstallationByAppID(_ context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	f.gotAppID = arg.AppID
	return f.inst, f.instErr
}

func (f *fakeIssueQueries) GetChannelUserBindingByUserID(_ context.Context, _ db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	return f.binding, f.bindErr
}

func (f *fakeIssueQueries) GetMemberByUserAndWorkspace(_ context.Context, _ db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
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

// fakeIssueReplier captures the processor's user-facing output without hitting
// DingTalk. It satisfies the (package-private) issueCommandReplier interface.
type fakeIssueReplier struct {
	posts        []string
	bindingCalls int
	postErr      error
}

func (f *fakeIssueReplier) post(_ context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, text string) error {
	f.posts = append(f.posts, text)
	return f.postErr
}

func (f *fakeIssueReplier) sendBindingPrompt(_ context.Context, _ engine.ResolvedInstallation, _ channel.InboundMessage, _ engine.Result) error {
	f.bindingCalls++
	return nil
}

func issueTestUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

func newTestIssueProcessor(q issueCommandQueries, tasks quickCreateEnqueuer, replier issueCommandReplier) *IssueCommandProcessor {
	return &IssueCommandProcessor{q: q, tasks: tasks, replier: replier, logger: slog.Default()}
}

func activeIssueInstallation() db.ChannelInstallation {
	return db.ChannelInstallation{
		ID:              issueTestUUID(1),
		WorkspaceID:     issueTestUUID(2),
		AgentID:         issueTestUUID(3),
		InstallerUserID: issueTestUUID(4),
		Status:          "active",
	}
}

// issueMsg builds a normalized inbound message carrying the DingTalk envelope
// the processor decodes (stamped AppKey + sender staff id).
func issueMsg(text string) channel.InboundMessage {
	raw, _ := json.Marshal(dingtalkRawEvent{AppID: "A1"})
	return channel.InboundMessage{
		Text:           text,
		AddressedToBot: true,
		MessageID:      "M1",
		Source: channel.Source{
			ChannelType: TypeDingTalk,
			ChatID:      "C1",
			ChatType:    channel.ChatTypeP2P,
			SenderID:    "U1",
		},
		Raw: raw,
	}
}

// ---- tests ----

func TestIssueHandle_EnqueuesQuickCreateAndAcks(t *testing.T) {
	q := &fakeIssueQueries{
		inst:    activeIssueInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: issueTestUUID(9)},
	}
	tasks := &fakeQuickCreate{}
	replier := &fakeIssueReplier{}
	p := newTestIssueProcessor(q, tasks, replier)

	p.Handle(context.Background(), issueMsg("/issue Fix login"))

	if tasks.calls != 1 {
		t.Fatalf("expected 1 quick-create enqueue, got %d", tasks.calls)
	}
	if len(replier.posts) != 1 || replier.posts[0] != issueQueuedText {
		t.Fatalf("expected queued ack, got %v", replier.posts)
	}
	if q.gotAppID != "A1" {
		t.Errorf("installation lookup used app id %q, want A1", q.gotAppID)
	}
	if tasks.prompt != "Fix login" {
		t.Errorf("quick-create prompt = %q, want Fix login", tasks.prompt)
	}
	if tasks.workspaceID != issueTestUUID(2) {
		t.Errorf("quick-create workspace is not the installation workspace")
	}
	if tasks.agentID != issueTestUUID(3) {
		t.Errorf("quick-create not dispatched to the installation agent")
	}
	if tasks.requesterID != issueTestUUID(9) {
		t.Errorf("quick-create requester is not the bound member")
	}
	if tasks.squadID.Valid {
		t.Errorf("/issue quick-create must not carry a squad id")
	}
}

func TestIssueHandle_MultilinePromptPassedThrough(t *testing.T) {
	q := &fakeIssueQueries{
		inst:    activeIssueInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: issueTestUUID(9)},
	}
	tasks := &fakeQuickCreate{}
	p := newTestIssueProcessor(q, tasks, &fakeIssueReplier{})

	p.Handle(context.Background(), issueMsg("/issue Title\nline one\nline two"))

	// The whole natural-language text is the prompt — no title/body split; the
	// agent authors the well-formed issue from it.
	if tasks.prompt != "Title\nline one\nline two" {
		t.Errorf("prompt = %q, want the full text", tasks.prompt)
	}
}

func TestIssueHandle_EmptyPromptIsUsage(t *testing.T) {
	tasks := &fakeQuickCreate{}
	replier := &fakeIssueReplier{}
	p := newTestIssueProcessor(&fakeIssueQueries{inst: activeIssueInstallation()}, tasks, replier)

	p.Handle(context.Background(), issueMsg("/issue"))

	if tasks.calls != 0 {
		t.Fatalf("empty prompt must not enqueue a task")
	}
	if len(replier.posts) != 1 || replier.posts[0] != issueUsageText {
		t.Fatalf("expected usage reply, got %v", replier.posts)
	}
}

func TestIssueHandle_UnboundUserGetsBindingPrompt(t *testing.T) {
	q := &fakeIssueQueries{inst: activeIssueInstallation(), bindErr: pgx.ErrNoRows}
	tasks := &fakeQuickCreate{}
	replier := &fakeIssueReplier{}
	p := newTestIssueProcessor(q, tasks, replier)

	p.Handle(context.Background(), issueMsg("/issue Fix login"))

	if tasks.calls != 0 {
		t.Fatalf("unbound user must not enqueue a task")
	}
	if replier.bindingCalls != 1 {
		t.Fatalf("expected a binding prompt, got %d", replier.bindingCalls)
	}
	if len(replier.posts) != 0 {
		t.Fatalf("unbound user should get only the binding prompt, got posts %v", replier.posts)
	}
}

func TestIssueHandle_NonMemberDropped(t *testing.T) {
	q := &fakeIssueQueries{
		inst:      activeIssueInstallation(),
		binding:   db.ChannelUserBinding{MulticaUserID: issueTestUUID(9)},
		memberErr: pgx.ErrNoRows,
	}
	tasks := &fakeQuickCreate{}
	replier := &fakeIssueReplier{}
	p := newTestIssueProcessor(q, tasks, replier)

	p.Handle(context.Background(), issueMsg("/issue Fix login"))

	if tasks.calls != 0 {
		t.Fatalf("non-member must not enqueue a task")
	}
	if len(replier.posts) != 1 || replier.posts[0] != issueNotMemberText {
		t.Fatalf("expected not-member reply, got %v", replier.posts)
	}
}

func TestIssueHandle_InactiveInstallation(t *testing.T) {
	inst := activeIssueInstallation()
	inst.Status = "revoked"
	tasks := &fakeQuickCreate{}
	replier := &fakeIssueReplier{}
	p := newTestIssueProcessor(&fakeIssueQueries{inst: inst}, tasks, replier)

	p.Handle(context.Background(), issueMsg("/issue Fix login"))

	if tasks.calls != 0 {
		t.Fatalf("inactive install must not enqueue a task")
	}
	if len(replier.posts) != 1 || replier.posts[0] != issueDisabledText {
		t.Fatalf("expected disabled reply, got %v", replier.posts)
	}
}

func TestIssueHandle_UnroutableInstallationStaysSilent(t *testing.T) {
	tasks := &fakeQuickCreate{}
	replier := &fakeIssueReplier{}
	p := newTestIssueProcessor(&fakeIssueQueries{instErr: pgx.ErrNoRows}, tasks, replier)

	p.Handle(context.Background(), issueMsg("/issue Fix login"))

	if tasks.calls != 0 || replier.bindingCalls != 0 || len(replier.posts) != 0 {
		t.Fatalf("unroutable event must be silent: calls=%d binding=%d posts=%v",
			tasks.calls, replier.bindingCalls, replier.posts)
	}
}

func TestIssueHandle_EnqueueFailureIsInternalError(t *testing.T) {
	q := &fakeIssueQueries{
		inst:    activeIssueInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: issueTestUUID(9)},
	}
	tasks := &fakeQuickCreate{err: errors.New("agent has no runtime")}
	replier := &fakeIssueReplier{}
	p := newTestIssueProcessor(q, tasks, replier)

	p.Handle(context.Background(), issueMsg("/issue Fix login"))

	if tasks.calls != 1 {
		t.Fatalf("expected the enqueue to be attempted once, got %d", tasks.calls)
	}
	if len(replier.posts) != 1 || replier.posts[0] != issueInternalError {
		t.Fatalf("expected internal-error reply, got %v", replier.posts)
	}
}

func TestIssuePrompt(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"/issue Fix login", "Fix login"},
		{"/issue Title\nbody line", "Title\nbody line"},
		{"/issue", ""},
		{"/issue   ", ""},
		{"\n\n/issue spaced", "spaced"},
		{"not a command", ""},
		{"/issuetracker down", ""},
	}
	for _, c := range cases {
		if got := issuePrompt(c.text); got != c.want {
			t.Errorf("issuePrompt(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}
