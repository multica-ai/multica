//go:build mattermostdemo

// A scripted conversation against a live Mattermost, used to produce
// screenshots of the adapter working. Separate build tag from the E2E suite so
// it never runs as part of verification — it asserts almost nothing, it just
// drives realistic traffic and leaves it on screen.
//
// The bot's replies deliberately echo the fields the adapter ACTUALLY parsed
// out of each message rather than a canned "here's your answer". A staged reply
// would prove the bot can post; echoing the parse proves the bot understood,
// and every value on screen is one the production code produced.
//
// There is no Multica agent in the loop here — no server, no database, no
// runtime — so nothing below should be read as an agent answering. It is the
// adapter's inbound normalization and outbound sender, against a real server.
//
//	eval "$(./scripts/mattermost-e2e-up.sh)"
//	(cd server && go test -tags=mattermostdemo ./internal/integrations/mattermost/ -run Demo -v -count=1)

package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// describe renders the adapter's verdict on one inbound message as a compact
// Mattermost table. Every row is read straight off the normalized envelope.
func describe(msg channel.InboundMessage) string {
	var raw mattermostRawEvent
	_ = json.Unmarshal(msg.Raw, &raw)

	thread := msg.Source.ThreadID
	if thread == "" {
		thread = "_(top level)_"
	}
	bindingKey, _, replyThread := mattermostSessionRouting(msg)
	if replyThread == "" {
		replyThread = "_(none)_"
	}

	return fmt.Sprintf(
		"**Adapter parsed this message as:**\n\n"+
			"| field | value |\n|---|---|\n"+
			"| addressed to bot | `%t` |\n"+
			"| chat type | `%s` |\n"+
			"| message type | `%s` |\n"+
			"| text seen by agent | `%s` |\n"+
			"| command text | `%s` |\n"+
			"| fresh session | `%t` |\n"+
			"| thread | %s |\n"+
			"| session key | `%s` |\n"+
			"| reply thread | %s |\n"+
			"| sender | `%s` (%s) |\n"+
			"| routing key | `%s` |\n",
		msg.AddressedToBot, msg.Source.ChatType, msg.Type,
		msg.Text, msg.CommandText, msg.ForceFresh,
		thread, bindingKey, replyThread,
		raw.SenderName, msg.Source.SenderID, raw.AppID)
}

func TestDemoConversation(t *testing.T) {
	env := requireE2E(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rest := newRESTClient(env.URL, env.BotToken, nil)
	send := newSender(rest, logger)

	var mu sync.Mutex
	handled := map[string]bool{}

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

	built, err := newMattermostFactory(ChannelDeps{Logger: logger})(channel.Config{
		Type: TypeMattermost,
		Raw:  cfg,
		Handler: func(ctx context.Context, msg channel.InboundMessage) error {
			// One line per inbound message, so a demo run is also a readable
			// trace of what the adapter decided.
			t.Logf("inbound id=%s addressed=%t chat=%s text=%q", msg.MessageID, msg.AddressedToBot, msg.Source.ChatType, msg.Text)
			// Only answer when the adapter says the message was addressed to
			// the bot — the same gate the engine applies. Unaddressed chatter
			// staying silent is part of what the screenshots need to show.
			if !msg.AddressedToBot {
				mu.Lock()
				handled[msg.MessageID] = true
				mu.Unlock()
				return nil
			}
			_, _, replyThread := mattermostSessionRouting(msg)
			if _, err := send.Send(ctx, channel.OutboundMessage{
				ChatID:   msg.Source.ChatID,
				Text:     describe(msg),
				ThreadID: replyThread,
			}); err != nil {
				t.Errorf("send reply: %v", err)
			}
			mu.Lock()
			handled[msg.MessageID] = true
			mu.Unlock()
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

	// Surface a connection failure immediately. Swallowing it turns every
	// later step into an unexplained "never handled" timeout.
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
				time.Sleep(400 * time.Millisecond) // let the reply land
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		select {
		case err := <-connErr:
			t.Fatalf("message %s was never handled; connection had ended: %v", id, err)
		default:
		}
		t.Fatalf("message %s was never handled", id)
	}

	// Readiness: the socket authenticates asynchronously.
	probe := postAs(t, env, env.HumanToken, Post{
		ChannelID: env.ChannelID,
		Message:   fmt.Sprintf("@%s ready?", env.BotUsername),
	})
	waitHandled(probe.ID)

	say := func(channelID, text string, root string) Post {
		t.Helper()
		p := postAs(t, env, env.HumanToken, Post{ChannelID: channelID, Message: text, RootID: root})
		waitHandled(p.ID)
		return p
	}

	// 1. A plain mention in a public channel.
	say(env.ChannelID, fmt.Sprintf("@%s what changed in the login flow this week?", env.BotUsername), "")

	// 2. Unaddressed chatter — must draw no reply at all.
	say(env.ChannelID, "standup is at 10 today, usual link", "")

	// 3. A broadcast mention — must also draw no reply.
	say(env.ChannelID, "@channel deploy freeze starts Friday", "")

	// 4. An /issue command addressed to the bot.
	say(env.ChannelID, fmt.Sprintf("@%s /issue Login redirect drops the ?next= param", env.BotUsername), "")

	// 5. A thread the bot rooted, continued WITHOUT a mention.
	root, err := rest.CreatePost(context.Background(), Post{
		ChannelID: env.ChannelID,
		Message:   "I opened a thread here so the next message needs no mention.",
	})
	if err != nil {
		t.Fatalf("seed bot-rooted thread: %v", err)
	}
	say(env.ChannelID, "and can you also check the mobile path?", root.ID)

	// 6. A direct message — no mention needed.
	say(env.DMChannelID, "hey, no mention needed in here", "")

	// 7. /clear in the DM, to show the control command being recognised.
	say(env.DMChannelID, "/clear", "")

	// 8. A reply longer than one Mattermost post, to show chunking.
	long := "Here is a deliberately long reply to show chunking.\n\n" +
		strings.TrimRight(strings.Repeat("The adapter splits on a newline boundary when it can. ", 90), " ")
	if _, err := send.Send(context.Background(), channel.OutboundMessage{
		ChatID: env.ChannelID,
		Text:   long,
	}); err != nil {
		t.Fatalf("send long reply: %v", err)
	}

	t.Log("demo conversation posted; screenshot the channel and the DM")
}
