package sharecrm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type captureChatSession struct {
	appendIn     engine.AppendInput
	startIn      engine.StartSessionInput
	mediaIn      engine.BindMediaInput
	freshSession pgtype.UUID
	freshMessage string
}

func (f *captureChatSession) EnsureSession(context.Context, engine.EnsureSessionInput) (pgtype.UUID, error) {
	return pgtype.UUID{}, nil
}

func (f *captureChatSession) StartSession(_ context.Context, in engine.StartSessionInput) (engine.StartSessionResult, error) {
	f.startIn = in
	return engine.StartSessionResult{}, nil
}

func (f *captureChatSession) MarkPendingFresh(_ context.Context, sessionID pgtype.UUID, messageID string) error {
	f.freshSession = sessionID
	f.freshMessage = messageID
	return nil
}

func (f *captureChatSession) AppendUserMessage(_ context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	f.appendIn = in
	return engine.AppendResult{}, nil
}

func (f *captureChatSession) BindMediaRefs(_ context.Context, in engine.BindMediaInput) error {
	f.mediaIn = in
	return nil
}

type fakeIdentityQueries struct {
	binding   db.ChannelUserBinding
	bindErr   error
	agent     db.Agent
	agentErr  error
	user      db.User
	userErr   error
	memberErr error
}

func (f *fakeIdentityQueries) GetChannelUserBindingByUserID(context.Context, db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	return f.binding, f.bindErr
}

func (f *fakeIdentityQueries) GetAgent(context.Context, pgtype.UUID) (db.Agent, error) {
	return f.agent, f.agentErr
}

func (f *fakeIdentityQueries) GetUserByEmail(context.Context, string) (db.User, error) {
	return f.user, f.userErr
}

func (f *fakeIdentityQueries) GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return db.Member{}, f.memberErr
}

func TestShareCRMIdentityResolver_UsesConfiguredProxyUser(t *testing.T) {
	proxyID := pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	inst := engine.ResolvedInstallation{
		ID:          pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		AgentID:     pgtype.UUID{Bytes: [16]byte{3}, Valid: true},
	}
	agentEnv, _ := json.Marshal(map[string]string{shareCRMProxyEmailEnvKey: " proxy@example.com "})
	for _, chatType := range []channel.ChatType{channel.ChatTypeP2P, channel.ChatTypeGroup} {
		t.Run(string(chatType), func(t *testing.T) {
			f := &fakeIdentityQueries{
				bindErr: pgx.ErrNoRows,
				agent:   db.Agent{CustomEnv: agentEnv},
				user:    db.User{ID: proxyID},
			}
			msg := channel.InboundMessage{Source: channel.Source{SenderID: "external", ChatType: chatType}}

			got, err := (&identityResolver{q: f}).ResolveSender(context.Background(), inst, msg)
			if err != nil {
				t.Fatalf("ResolveSender: %v", err)
			}
			if got.UserID != proxyID {
				t.Fatalf("UserID = %v, want %v", got.UserID, proxyID)
			}
		})
	}
}

func TestShareCRMIdentityResolver_ProxyFallbackRequiresValidMember(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		userErr   error
		memberErr error
	}{
		{name: "missing email"},
		{name: "missing user", env: map[string]string{shareCRMProxyEmailEnvKey: "proxy@example.com"}, userErr: pgx.ErrNoRows},
		{name: "not member", env: map[string]string{shareCRMProxyEmailEnvKey: "proxy@example.com"}, memberErr: pgx.ErrNoRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := json.Marshal(tc.env)
			f := &fakeIdentityQueries{bindErr: pgx.ErrNoRows, agent: db.Agent{CustomEnv: env}, user: db.User{ID: pgtype.UUID{Valid: true}}, userErr: tc.userErr, memberErr: tc.memberErr}
			_, err := (&identityResolver{q: f}).ResolveSender(context.Background(), engine.ResolvedInstallation{AgentID: pgtype.UUID{Valid: true}}, channel.InboundMessage{})
			if !errors.Is(err, engine.ErrSenderUnbound) {
				t.Fatalf("err = %v, want ErrSenderUnbound", err)
			}
		})
	}
}

func TestExternalSessionIDFromBindingConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config []byte
		want   string
	}{
		{"value", []byte(`{"chat_id":"chat","session_id":" session-1 "}`), "session-1"},
		{"missing", []byte(`{"chat_id":"chat"}`), ""},
		{"malformed", []byte(`{"session_id":`), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExternalSessionIDFromBindingConfig(tc.config); got != tc.want {
				t.Fatalf("ExternalSessionIDFromBindingConfig() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShareCRMSessionBinder_AppendPreservesFreshContextIntent(t *testing.T) {
	session := &captureChatSession{}
	binder := &sessionBinder{session: session}

	if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		Message: channel.InboundMessage{
			MessageID:  "m-fresh",
			Text:       "summarize this",
			ForceFresh: true,
			Source: channel.Source{
				ChatID:   "0:fs:session123:",
				ChatType: channel.ChatTypeP2P,
			},
		},
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if !session.appendIn.ForceFresh {
		t.Fatal("AppendUserMessage lost ForceFresh; /clear <message> would remain in the previous context generation")
	}
	if session.appendIn.MessageID != "m-fresh" {
		t.Fatalf("MessageID = %q, want m-fresh", session.appendIn.MessageID)
	}
}

func TestShareCRMSessionBinder_MarkPendingFreshForwardsMessageID(t *testing.T) {
	session := &captureChatSession{}
	binder := &sessionBinder{session: session}
	sessionID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	if err := binder.MarkPendingFresh(context.Background(), sessionID, "bare-new"); err != nil {
		t.Fatalf("MarkPendingFresh: %v", err)
	}
	if session.freshMessage != "bare-new" {
		t.Fatalf("messageID = %q, want bare-new", session.freshMessage)
	}
	if session.freshSession != sessionID {
		t.Fatalf("sessionID = %v, want %v", session.freshSession, sessionID)
	}
}

func TestShareCRMSessionBinder_StartSessionForwardsRouting(t *testing.T) {
	session := &captureChatSession{}
	binder := &sessionBinder{session: session}
	creator := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	initiator := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}

	if _, err := binder.StartSession(context.Background(), engine.StartSessionParams{
		Installation: engine.ResolvedInstallation{ID: pgtype.UUID{Bytes: [16]byte{4}, Valid: true}},
		Creator:      creator,
		Sender:       initiator,
		Message: channel.InboundMessage{
			MessageID: "m-new",
			Text:      "hello after /new",
			Raw:       []byte(`{"session_id":" external-session-1 "}`),
			Source: channel.Source{
				ChatID:   "0:fs:session-new:",
				ChatType: channel.ChatTypeP2P,
			},
		},
		PersistMessage: true,
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if session.startIn.BindingKey != "0:fs:session-new:" {
		t.Fatalf("BindingKey = %q, want chat id", session.startIn.BindingKey)
	}
	var bindingConfig sharecrmBindingConfig
	if err := json.Unmarshal(session.startIn.BindingConfig, &bindingConfig); err != nil {
		t.Fatalf("BindingConfig: %v", err)
	}
	if bindingConfig.SessionID != "external-session-1" {
		t.Fatalf("binding session_id = %q, want external-session-1", bindingConfig.SessionID)
	}
	if session.startIn.Sender != creator {
		t.Fatalf("embedded Sender = %v, want creator", session.startIn.Sender)
	}
	if session.startIn.Initiator != initiator {
		t.Fatalf("Initiator = %v, want authenticated sender", session.startIn.Initiator)
	}
	if session.startIn.Body != "hello after /new" || session.startIn.MessageID != "m-new" {
		t.Fatalf("first-turn body/id = %q/%q", session.startIn.Body, session.startIn.MessageID)
	}
	if !session.startIn.PersistMessage {
		t.Fatal("PersistMessage dropped; /new <message> would create an empty chat")
	}
}

func TestShareCRMSessionBinder_MapsMediaBodyAndIssueTarget(t *testing.T) {
	var message, sessionID, workspace, sender, issue pgtype.UUID
	message.Bytes[0], sessionID.Bytes[0], workspace.Bytes[0], sender.Bytes[0], issue.Bytes[0] = 1, 2, 3, 4, 5
	message.Valid, sessionID.Valid, workspace.Valid, sender.Valid, issue.Valid = true, true, true, true, true
	ref := channel.MediaRef{Type: channel.MsgTypeImage, InlinePlaceholder: "[Image]", InlineIndex: 0}
	base := pgtype.Text{String: "[Image]\nfix login", Valid: true}
	session := &captureChatSession{}
	binder := &sessionBinder{session: session}
	if _, err := binder.BindMedia(context.Background(), engine.BindMediaParams{
		MessageID: message, SessionID: sessionID, WorkspaceID: workspace, Sender: sender,
		IssueID: issue, IssueDescriptionBase: base, IssueCommandText: "/issue fix login",
		Body: "[Image]\nfix login", MediaRefs: []channel.MediaRef{ref},
	}); err != nil {
		t.Fatal(err)
	}
	got := session.mediaIn
	if got.MessageID != message || got.SessionID != sessionID || got.WorkspaceID != workspace || got.Sender != sender || got.IssueID != issue || got.IssueDescriptionBase != base || got.IssueCommandText != "/issue fix login" || got.Body != "[Image]\nfix login" || len(got.MediaRefs) != 1 || got.MediaRefs[0] != ref {
		t.Fatalf("mapped media input = %+v", got)
	}
}
