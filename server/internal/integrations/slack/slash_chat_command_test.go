package slack

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeChatCommands answers Command with reply, handled and err, and records the
// commands it was asked. Presses are not used by these tests.
type fakeChatCommands struct {
	mu      sync.Mutex
	reply   channel.Message
	handled bool
	err     error
	got     []channel.Command
}

func (f *fakeChatCommands) Press(context.Context, channel.Press) (channel.PressResult, error) {
	return channel.PressResult{}, nil
}

func (f *fakeChatCommands) ActionKey(string) string { return "" }

func (f *fakeChatCommands) Command(_ context.Context, c channel.Command) (channel.Message, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, c)
	return f.reply, f.handled, f.err
}

func (f *fakeChatCommands) commands() []channel.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]channel.Command(nil), f.got...)
}

// chatCommandProcessor is a processor over the linked-member fakes, with the
// given chat command handler wired in.
func chatCommandProcessor(actions channel.ActionHandler, q *fakeSlashQueries) (*SlashCommandProcessor, *string, *int) {
	p, captured, count := newTestSlashProcessor(q, &fakeQuickCreate{}, &fakeBindingMinter{raw: "TOKEN123"})
	p.actions = actions
	return p, captured, count
}

func linkedSlashQueries() *fakeSlashQueries {
	return &fakeSlashQueries{
		inst:    activeSlashInstallation(),
		binding: db.ChannelUserBinding{MulticaUserID: slashTestUUID(9)},
	}
}

func chatSlashCmd(command string) slack.SlashCommand {
	cmd := issueSlashCmd()
	cmd.Command = command
	cmd.Text = ""
	return cmd
}

func TestSlashBriefAnswersALinkedMember(t *testing.T) {
	actions := &fakeChatCommands{
		handled: true,
		reply:   channel.Message{Text: "Ship the launch\n\nBrief: https://app.example/acme/company?brief=b1"},
	}
	p, captured, count := chatCommandProcessor(actions, linkedSlashQueries())

	p.Handle(context.Background(), chatSlashCmd("/brief"))

	if *count != 1 || *captured != "Ship the launch\n\nBrief: https://app.example/acme/company?brief=b1" {
		t.Fatalf("reply = %q (%d replies), want the brief text once", *captured, *count)
	}
	got := actions.commands()
	if len(got) != 1 {
		t.Fatalf("commands = %d, want 1", len(got))
	}
	want := channel.Command{
		Sender: channel.Sender{ChannelType: TypeSlack, InstallationID: slashTestUUID(1), ChannelUserID: "U1"},
		Name:   channel.CommandBrief,
	}
	if got[0] != want {
		t.Fatalf("command = %+v, want %+v", got[0], want)
	}
}

func TestSlashDecisionsNamesThePendingDecisions(t *testing.T) {
	actions := &fakeChatCommands{
		handled: true,
		reply:   channel.Message{Text: "1 decision needs you:\n- Ship <beta> & more https://app.example/acme/company?decision=p1"},
	}
	p, captured, _ := chatCommandProcessor(actions, linkedSlashQueries())

	p.Handle(context.Background(), chatSlashCmd("/decisions"))

	if !strings.Contains(*captured, "- Ship &lt;beta&gt; &amp; more https://app.example/acme/company?decision=p1") {
		t.Fatalf("reply = %q, want the title escaped and the link kept", *captured)
	}
	if got := actions.commands(); len(got) != 1 || got[0].Name != channel.CommandDecisions {
		t.Fatalf("commands = %+v, want one decisions command", got)
	}
}

func TestSlashChatCommandUnlinkedGetsTheBindingPrompt(t *testing.T) {
	q := &fakeSlashQueries{inst: activeSlashInstallation(), bindErr: pgx.ErrNoRows}
	actions := &fakeChatCommands{handled: true, reply: channel.Message{Text: "should not be sent"}}
	p, captured, _ := chatCommandProcessor(actions, q)

	p.Handle(context.Background(), chatSlashCmd("/brief"))

	if !strings.Contains(*captured, "link your account") || !strings.Contains(*captured, "TOKEN123") {
		t.Fatalf("reply = %q, want the binding prompt", *captured)
	}
	if n := len(actions.commands()); n != 0 {
		t.Fatalf("an unlinked sender reached the command handler %d times", n)
	}
}

func TestSlashChatCommandNonMemberIsDropped(t *testing.T) {
	q := linkedSlashQueries()
	q.memberErr = pgx.ErrNoRows
	actions := &fakeChatCommands{handled: true, reply: channel.Message{Text: "should not be sent"}}
	p, captured, _ := chatCommandProcessor(actions, q)

	p.Handle(context.Background(), chatSlashCmd("/decisions"))

	if *captured != slashNotMemberText {
		t.Fatalf("reply = %q, want the not-a-member text", *captured)
	}
	if n := len(actions.commands()); n != 0 {
		t.Fatalf("a non-member reached the command handler %d times", n)
	}
}

func TestSlashChatCommandOnRevokedInstallationIsDisabled(t *testing.T) {
	q := linkedSlashQueries()
	q.inst.Status = "revoked"
	actions := &fakeChatCommands{handled: true}
	p, captured, _ := chatCommandProcessor(actions, q)

	p.Handle(context.Background(), chatSlashCmd("/brief"))

	if *captured != slashDisabledText {
		t.Fatalf("reply = %q, want the disabled text", *captured)
	}
}

// When the handler does not answer, the sender gets the binding prompt, as the
// ordinary path would give them.
func TestSlashChatCommandUnhandledFallsBackToTheBindingPrompt(t *testing.T) {
	actions := &fakeChatCommands{handled: false}
	p, captured, _ := chatCommandProcessor(actions, linkedSlashQueries())

	p.Handle(context.Background(), chatSlashCmd("/brief"))

	if !strings.Contains(*captured, "link your account") {
		t.Fatalf("reply = %q, want the binding prompt", *captured)
	}
}

func TestSlashChatCommandFailureIsInternalError(t *testing.T) {
	actions := &fakeChatCommands{err: pgx.ErrTxClosed}
	p, captured, _ := chatCommandProcessor(actions, linkedSlashQueries())

	p.Handle(context.Background(), chatSlashCmd("/brief"))

	if *captured != slashInternalErrorText {
		t.Fatalf("reply = %q, want the internal error text", *captured)
	}
}

// Without a handler wired, /brief is not answered as a silent success: the
// member is told something went wrong, and nothing else is asked.
func TestSlashChatCommandWithoutHandlerIsInternalError(t *testing.T) {
	p, captured, _ := chatCommandProcessor(nil, linkedSlashQueries())

	p.Handle(context.Background(), chatSlashCmd("/brief"))

	if *captured != slashInternalErrorText {
		t.Fatalf("reply = %q, want the internal error text", *captured)
	}
}
