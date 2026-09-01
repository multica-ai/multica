//go:build mattermostdemo

// A scripted conversation against a live Mattermost, used to produce
// screenshots that look the way production looks. Separate build tag from the
// E2E suite so it never runs as part of verification — it asserts almost
// nothing, it just drives realistic traffic and leaves it on screen.
//
// What is real here:
//
//   - The adapter. Inbound normalization, addressing, threading, chunking and
//     the outbound sender are the production code paths, against a real server.
//   - The verdict replies. Binding prompt, /issue confirmation, /clear
//     confirmation and the unsupported-media notice are produced by the real
//     OutboundReplier from replier.go, so the wording on screen is the wording
//     a user gets.
//
// What is NOT real: there is no Multica agent, server, database or runtime in
// this process. Two things therefore stand in for it —
//
//   - the ANSWER text an agent would have written (production takes it from
//     the completion and passes it to this same sender verbatim), and
//   - the issue identifier in the /issue confirmation, which production reads
//     back from the database.
//
// Everything about how those reach Mattermost is production code; only the
// content is supplied here.
//
//	eval "$(./scripts/mattermost-e2e-up.sh)"
//	(cd server && go test -tags=mattermostdemo ./internal/integrations/mattermost/ -run Demo -v -count=1)

package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// demoMinter stands in for BindingTokenService so the real replier can render
// its real binding prompt without a database behind it.
type demoMinter struct{}

func (demoMinter) Mint(context.Context, pgtype.UUID, pgtype.UUID, string) (BindingToken, error) {
	return BindingToken{Raw: "demo-token-not-redeemable"}, nil
}

// agentAnswer is the stand-in for an agent completion. Production hands the
// completion straight to the same sender, so the delivery, Markdown rendering
// and threading below are real; only these words are invented.
func agentAnswer(topic string) string {
	switch topic {
	case "login":
		return "Three changes landed on the login flow this week:\n\n" +
			"1. **`?next=` is now preserved** through the OAuth round trip " +
			"([#7812](https://github.com/multica-ai/multica/pull/7812)).\n" +
			"2. Session cookies moved to `SameSite=Lax`, which is what broke the " +
			"Safari redirect earlier in the week.\n" +
			"3. The rate limiter on `/api/auth/login` dropped from 20/min to 10/min.\n\n" +
			"The Safari regression is the one worth a second look — I'd start at " +
			"`server/internal/handler/auth.go:214`."
	case "mobile":
		return "Checked — the mobile path takes the same handler, so it inherits " +
			"the `?next=` fix. The one difference is that the app opens the callback " +
			"in an in-app browser, so `SameSite=Lax` behaves differently there. " +
			"Worth testing on a real device before we call it done."
	default:
		return "Yes — I can see this channel and I'm connected to the workspace. " +
			"Ask me anything, or use `/issue <title>` to file something."
	}
}

func TestDemoConversation(t *testing.T) {
	env := requireE2E(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rest := newRESTClient(env.URL, env.BotToken, nil)
	send := newSender(rest, logger)

	cfg, err := json.Marshal(installConfig{
		AppID:                installationKey(env.URL, env.BotUserID),
		ServerURL:            env.URL,
		BotUserID:            env.BotUserID,
		BotUsername:          env.BotUsername,
		AccessTokenEncrypted: base64Std(env.BotToken),
	})
	if err != nil {
		t.Fatal(err)
	}

	// The real replier, wired exactly as router.go wires it.
	replier := NewOutboundReplier(OutboundReplierConfig{
		Binding: demoMinter{},
		AppURL:  "https://app.multica.ai",
		Logger:  logger,
	})
	inst := engine.ResolvedInstallation{Platform: db.ChannelInstallation{Config: cfg}}

	// The plan is keyed by the message text AS THE ADAPTER NORMALIZES IT
	// (CommandText: mention stripped, directive intact) and registered before
	// anything is posted. Registering after the post loses the race — the
	// websocket event arrives in single-digit milliseconds on localhost.
	type replyPlan struct {
		verdict *engine.Result
		answer  string
	}
	var mu sync.Mutex
	plan := map[string]replyPlan{}
	handled := map[string]bool{}

	built, err := newMattermostFactory(ChannelDeps{Logger: logger})(channel.Config{
		Type: TypeMattermost,
		Raw:  cfg,
		Handler: func(ctx context.Context, msg channel.InboundMessage) error {
			// One line per inbound message, so a demo run is also a readable
			// trace of what the adapter decided. This goes to the test log,
			// never to Mattermost.
			t.Logf("inbound id=%s addressed=%t chat=%s text=%q",
				msg.MessageID, msg.AddressedToBot, msg.Source.ChatType, msg.Text)

			mu.Lock()
			step := plan[msg.CommandText]
			mu.Unlock()

			defer func() {
				mu.Lock()
				handled[msg.MessageID] = true
				mu.Unlock()
			}()

			// Unaddressed chatter draws nothing — the same gate the engine
			// applies, and part of what the screenshots need to show.
			if !msg.AddressedToBot {
				return nil
			}
			if step.verdict != nil {
				replier.Reply(ctx, inst, msg, *step.verdict)
				return nil
			}
			answer := step.answer
			if answer == "" {
				answer = agentAnswer("")
			}
			_, _, replyThread := mattermostSessionRouting(msg)
			if _, err := send.Send(ctx, channel.OutboundMessage{
				ChatID:   msg.Source.ChatID,
				Text:     answer,
				ThreadID: replyThread,
			}); err != nil {
				t.Errorf("send reply: %v", err)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connErr := make(chan error, 1)
	go func() { connErr <- built.Connect(ctx) }()

	select {
	case err := <-connErr:
		t.Fatalf("Connect returned before any traffic: %v", err)
	case <-time.After(2 * time.Second):
	}

	waitHandled := func(id string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			ok := handled[id]
			mu.Unlock()
			if ok {
				time.Sleep(500 * time.Millisecond) // let the reply land
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		select {
		case err := <-connErr:
			t.Fatalf("message %s was never handled; connection ended: %v", id, err)
		default:
		}
		t.Fatalf("message %s was never handled", id)
	}

	// post registers what the bot should do BEFORE publishing, keyed by the
	// text the adapter will hand back, then waits for the reply to land.
	post := func(channelID, text, root string, verdict *engine.Result, answer string) Post {
		t.Helper()
		key := strings.TrimSpace(stripBotMentions(text, env.BotUsername))
		mu.Lock()
		plan[key] = replyPlan{verdict: verdict, answer: answer}
		mu.Unlock()
		p := postAs(t, env, env.HumanToken, Post{ChannelID: channelID, Message: text, RootID: root})
		waitHandled(p.ID)
		return p
	}

	// Readiness: the socket authenticates asynchronously.
	waitHandled(postAs(t, env, env.HumanToken, Post{
		ChannelID: env.ChannelID,
		Message:   fmt.Sprintf("@%s are you connected?", env.BotUsername),
	}).ID)

	// ---- direct message: first contact, then a real conversation -----------

	// The very first thing a new user sees: the real binding prompt.
	needsBinding := engine.Result{Outcome: engine.OutcomeNeedsBinding, Sender: env.HumanUserID}
	post(env.DMChannelID, "hi, are you the Multica bot?", "", &needsBinding, "")

	post(env.DMChannelID, "what changed in the login flow this week?", "", nil, agentAnswer("login"))

	freshPending := engine.Result{Outcome: engine.OutcomeFreshPending}
	post(env.DMChannelID, "/clear", "", &freshPending, "")

	// ---- public channel ----------------------------------------------------

	post(env.ChannelID, fmt.Sprintf("@%s what changed in the login flow this week?", env.BotUsername),
		"", nil, agentAnswer("login"))

	// Neither of these is addressed to the bot; both must draw silence.
	post(env.ChannelID, "standup is at 10 today, usual link", "", nil, "")
	post(env.ChannelID, "@channel deploy freeze starts Friday", "", nil, "")

	// A real /issue confirmation. The copy is production; the identifier and
	// title are supplied here because there is no Multica database in-process.
	issueCreated := engine.Result{
		Outcome:         engine.OutcomeIngested,
		IssueID:         pgtype.UUID{Valid: true},
		IssueIdentifier: "MUL-4871",
		IssueTitle:      "Login redirect drops the ?next= param",
	}
	post(env.ChannelID, fmt.Sprintf("@%s /issue Login redirect drops the ?next= param", env.BotUsername),
		"", &issueCreated, "")

	// A thread the bot rooted, continued WITHOUT a mention.
	root, err := rest.CreatePost(context.Background(), Post{
		ChannelID: env.ChannelID,
		Message: "I opened a thread for the mobile question so we can keep it " +
			"separate from the channel.",
	})
	if err != nil {
		t.Fatalf("seed bot-rooted thread: %v", err)
	}
	post(env.ChannelID, "and can you also check the mobile path?", root.ID, nil, agentAnswer("mobile"))

	// Chunking is deliberately NOT demonstrated here. It needs ~4000 characters
	// to trigger, which swamps the screenshot with filler and shows a reader
	// nothing about the product. TestE2ESendChunksLongReplyIntoOneThread
	// asserts it against the same live server instead.

	t.Log("demo conversation posted; screenshot the channel and the DM")
}
