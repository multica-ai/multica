package slack

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSlashHelpRepliesWithoutAgentOrSessionWork(t *testing.T) {
	for _, channelID := range []string{"D1", "C1"} {
		t.Run(channelID, func(t *testing.T) {
			q := &fakeSlashQueries{inst: activeSlashInstallation(), binding: db.ChannelUserBinding{MulticaUserID: slashTestUUID(9)}}
			tasks := &fakeQuickCreate{}
			control := &fakeSlashControlStarter{}
			p, captured, count := newTestSlashProcessor(q, tasks, nil)
			p.control = control
			cmd := issueSlashCmd()
			cmd.Command, cmd.Text, cmd.ChannelID = "/help", "", channelID
			p.HandleEnvelope(context.Background(), cmd, "help-envelope")
			if *count != 1 || *captured != engine.SlackHelpText {
				t.Fatalf("help replies = %d, text = %q", *count, *captured)
			}
			for _, command := range []string{"/help", "/new", "/clear", "/issue"} {
				if !strings.Contains(*captured, command) {
					t.Errorf("help does not list %s", command)
				}
			}
			if tasks.calls != 0 || control.newCalls != 0 || control.clearCalls != 0 {
				t.Fatal("help scheduled a task or changed a Chat")
			}
		})
	}
}

func TestSlashHelpRespectsInstallationAndMembership(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*fakeSlashQueries)
		want  string
	}{
		{name: "missing installation", setup: func(q *fakeSlashQueries) { q.instErr = pgx.ErrNoRows }, want: slashDisabledText},
		{name: "revoked", setup: func(q *fakeSlashQueries) { q.inst.Status = "revoked" }, want: slashDisabledText},
		{name: "unbound", setup: func(q *fakeSlashQueries) { q.bindErr = pgx.ErrNoRows }, want: slashLinkAccountFallback},
		{name: "nonmember", setup: func(q *fakeSlashQueries) { q.memberErr = pgx.ErrNoRows }, want: slashNotMemberText},
		{name: "lookup failure", setup: func(q *fakeSlashQueries) { q.instErr = errors.New("unavailable") }, want: slashInternalErrorText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeSlashQueries{inst: activeSlashInstallation()}
			tc.setup(q)
			tasks := &fakeQuickCreate{}
			p, captured, count := newTestSlashProcessor(q, tasks, nil)
			cmd := issueSlashCmd()
			cmd.Command, cmd.Text = "/help", ""
			p.Handle(context.Background(), cmd)
			if *count != 1 || *captured != tc.want {
				t.Fatalf("replies = %d, text = %q, want %q", *count, *captured, tc.want)
			}
			if tasks.calls != 0 {
				t.Fatal("help enqueued a quick-create task")
			}
		})
	}
}

func TestSlashHelpWithoutResponseURLDoesNotPost(t *testing.T) {
	p, _, count := newTestSlashProcessor(&fakeSlashQueries{inst: activeSlashInstallation()}, &fakeQuickCreate{}, nil)
	p.Handle(context.Background(), slack.SlashCommand{Command: "/help", APIAppID: "A1", TeamID: "T1"})
	if *count != 0 {
		t.Fatal("posted help without a response URL")
	}
}
