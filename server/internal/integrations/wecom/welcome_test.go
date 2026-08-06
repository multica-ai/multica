package wecom

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestMigration275AllowsWecomChatOrigin(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "migrations")
	raw, err := os.ReadFile(filepath.Join(root, "275_issue_origin_wecom_chat.up.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "'wecom_chat'") {
		t.Fatalf("migration must allow wecom_chat origin, got:\n%s", body)
	}
	for _, origin := range []string{"autopilot", "quick_create", "lark_chat", "slack_chat", "agent_create"} {
		if !strings.Contains(body, "'"+origin+"'") {
			t.Fatalf("migration must retain %q", origin)
		}
	}
}

func TestRenderWelcomeBound(t *testing.T) {
	got, err := RenderWelcome(WelcomeInput{Locale: "en", Bound: true})
	if err != nil {
		t.Fatalf("RenderWelcome: %v", err)
	}
	if strings.Contains(got, "http") || strings.Contains(got, "token") {
		t.Fatalf("bound welcome must not include bind URL: %q", got)
	}
	if !strings.Contains(got, "Welcome") {
		t.Fatalf("bound welcome text unexpected: %q", got)
	}
}

func TestRenderWelcomeUnboundIncludesBindURL(t *testing.T) {
	got, err := RenderWelcome(WelcomeInput{
		Locale:          "en",
		AppURL:          "https://app.example.com",
		Bound:           false,
		BindingTokenRaw: "tok123",
	})
	if err != nil {
		t.Fatalf("RenderWelcome: %v", err)
	}
	want := "https://app.example.com/wecom/bind?token=tok123"
	if !strings.Contains(got, want) {
		t.Fatalf("welcome = %q, want substring %q", got, want)
	}
}

func TestBuildWelcomeMarkdownBound(t *testing.T) {
	instID := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	q := &fakeWelcomeQueries{
		binding: db.ChannelUserBinding{MulticaUserID: util.MustParseUUID("22222222-2222-2222-2222-222222222222")},
	}

	got, err := buildWelcomeMarkdown(context.Background(), welcomeBuildInput{
		Locale:         "en",
		InstallationID: util.UUIDToString(instID),
		WecomUserID:    "u1",
		Queries:        q,
	})
	if err != nil {
		t.Fatalf("buildWelcomeMarkdown: %v", err)
	}
	if strings.Contains(got, "/wecom/bind") {
		t.Fatalf("bound welcome must not include bind URL: %q", got)
	}
}

func TestBuildWelcomeMarkdownUnboundMintsToken(t *testing.T) {
	instID := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	wsID := util.MustParseUUID("33333333-3333-3333-3333-333333333333")
	q := &fakeWelcomeQueries{
		installation: db.ChannelInstallation{ID: instID, WorkspaceID: wsID},
		bindingErr:   pgx.ErrNoRows,
	}
	minter := &fakeWelcomeBinder{raw: "minted-token"}

	got, err := buildWelcomeMarkdown(context.Background(), welcomeBuildInput{
		Locale:         "en",
		AppURL:         "https://app.example.com",
		InstallationID: util.UUIDToString(instID),
		WecomUserID:    "u1",
		Queries:        q,
		Binding:        minter,
	})
	if err != nil {
		t.Fatalf("buildWelcomeMarkdown: %v", err)
	}
	if !strings.Contains(got, "token=minted-token") {
		t.Fatalf("unbound welcome = %q, want minted bind URL", got)
	}
	if minter.mintCalls != 1 {
		t.Fatalf("mint calls = %d, want 1", minter.mintCalls)
	}
}

func TestOnWelcomeIgnoresNonSingleChat(t *testing.T) {
	var skipped atomic.Int32
	ch := &wecomChannel{
		installationID: "inst-1",
		logger:         slog.Default(),
		metrics: &recordingMetrics{
			onWelcomeSkipped: func() { skipped.Add(1) },
		},
	}
	body, _ := json.Marshal(EventCallbackBody{
		MsgID:    "m1",
		ChatType: ChatTypeGroup,
		From:     &MsgFrom{UserID: "u1"},
		Event:    EventBody{EventType: EventTypeEnterChat},
	})
	replied := false
	ch.onWelcome(context.Background(), WelcomeContext{
		Frame: Frame{Body: body},
		Reply: func(context.Context, any) error {
			replied = true
			return nil
		},
	})
	if replied {
		t.Fatal("non-single enter_chat must not reply")
	}
	if skipped.Load() != 1 {
		t.Fatalf("skipped metric = %d, want 1", skipped.Load())
	}
}

func TestConn_WelcomeHighPriorityOverNormalSend(t *testing.T) {
	clientReady := make(chan struct{})
	welcomeSeen := make(chan Frame, 1)

	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 0, "")

		// Wait until the client has enqueued a normal send so welcome must
		// preempt the backed-up writePump queue.
		select {
		case <-clientReady:
		case <-time.After(3 * time.Second):
			t.Error("client never queued normal send")
			return
		}

		body, _ := json.Marshal(EventCallbackBody{
			MsgID:    "m-1",
			ChatType: ChatTypeSingle,
			From:     &MsgFrom{UserID: "u1"},
			Event:    EventBody{EventType: EventTypeEnterChat},
		})
		writeFrame(t, c, Frame{
			Cmd:     CmdEventCallback,
			Headers: FrameHeaders{ReqID: "WELCOME-HI"},
			Body:    body,
		})

		firstCmd := ""
		for i := 0; i < 3; i++ {
			f, ok := readFrame(t, c)
			if !ok {
				return
			}
			if firstCmd == "" {
				firstCmd = f.Cmd
			}
			switch f.Cmd {
			case CmdRespondWelcome:
				welcomeSeen <- f
				writeResponse(t, c, f.Headers.ReqID, 0, "")
			case CmdSendMsg:
				writeResponse(t, c, f.Headers.ReqID, 0, "")
			case CmdPing:
				writeResponse(t, c, f.Headers.ReqID, 0, "")
			}
		}
		if firstCmd != CmdRespondWelcome {
			t.Errorf("first write cmd = %q, want %q", firstCmd, CmdRespondWelcome)
		}
	})

	conn, err := NewConn(ConnConfig{
		DialURL:      fs.url,
		BotID:        "bot",
		Secret:       "sec",
		PingInterval: time.Hour,
		OnWelcome: func(ctx context.Context, wc WelcomeContext) {
			_ = wc.Reply(ctx, WelcomeMsgBody{
				MsgType:  "markdown",
				Markdown: &MarkdownBody{Content: "hi"},
			})
		},
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = conn.Run(ctx) }()
	waitConnected(t, conn)

	go func() {
		_, _ = conn.SendRequest(context.Background(), CmdSendMsg, SendMsgBody{
			ChatID:   "u1",
			ChatType: 1,
			MsgType:  "markdown",
			Markdown: &MarkdownBody{Content: "backlog"},
		})
	}()
	time.Sleep(30 * time.Millisecond)
	close(clientReady)

	select {
	case f := <-welcomeSeen:
		if f.Cmd != CmdRespondWelcome {
			t.Fatalf("welcome cmd = %q, want %q", f.Cmd, CmdRespondWelcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("welcome frame not observed before timeout")
	}
	cancel()
}

func TestConn_EnterChatWelcomeDoesNotBlockReadPump(t *testing.T) {
	responseAfterWelcome := make(chan struct{})

	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 0, "")

		body, _ := json.Marshal(EventCallbackBody{
			MsgID:    "m-1",
			ChatType: ChatTypeSingle,
			From:     &MsgFrom{UserID: "u1"},
			Event:    EventBody{EventType: EventTypeEnterChat},
		})
		writeFrame(t, c, Frame{
			Cmd:     CmdEventCallback,
			Headers: FrameHeaders{ReqID: "EVENT-READPUMP"},
			Body:    body,
		})

		for {
			f, ok := readFrame(t, c)
			if !ok {
				return
			}
			if f.Cmd == CmdRespondWelcome {
				time.Sleep(150 * time.Millisecond)
				writeResponse(t, c, f.Headers.ReqID, 0, "")
				close(responseAfterWelcome)
				continue
			}
			if f.Cmd == CmdPing {
				writeResponse(t, c, f.Headers.ReqID, 0, "")
			}
		}
	})

	conn, err := NewConn(ConnConfig{
		DialURL:         fs.url,
		BotID:           "bot",
		Secret:          "sec",
		PingInterval:    50 * time.Millisecond,
		WelcomeDeadline: 4 * time.Second,
		OnWelcome: func(ctx context.Context, wc WelcomeContext) {
			_ = wc.Reply(ctx, WelcomeMsgBody{
				MsgType:  "markdown",
				Markdown: &MarkdownBody{Content: "welcome"},
			})
		},
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = conn.Run(ctx) }()
	waitConnected(t, conn)

	select {
	case <-responseAfterWelcome:
	case <-time.After(3 * time.Second):
		t.Fatal("welcome response not completed within deadline")
	}

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer pingCancel()
	if _, err := conn.SendRequest(pingCtx, CmdPing, nil); err != nil {
		t.Fatalf("readPump appears blocked by welcome path: %v", err)
	}
	cancel()
}

type fakeWelcomeQueries struct {
	installation db.ChannelInstallation
	binding      db.ChannelUserBinding
	bindingErr   error
}

func (f *fakeWelcomeQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.installation, nil
}

func (f *fakeWelcomeQueries) GetChannelUserBindingByUserID(context.Context, db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	if f.bindingErr != nil {
		return db.ChannelUserBinding{}, f.bindingErr
	}
	return f.binding, nil
}

type fakeWelcomeBinder struct {
	raw       string
	mintCalls int
}

func (f *fakeWelcomeBinder) Mint(context.Context, pgtype.UUID, pgtype.UUID, string) (BindingToken, error) {
	f.mintCalls++
	return BindingToken{Raw: f.raw}, nil
}
