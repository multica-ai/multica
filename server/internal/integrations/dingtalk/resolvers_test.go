package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeGroupPresenceQueries struct {
	identityCalls  int
	identityParams db.UpsertDingTalkBotIdentityParams
	identityErr    error
	calls          int
	params         db.RecordDingTalkGroupPresenceParams
	err            error
	activityCalls  int
	activityParams db.RecordDingTalkGroupActivityParams
	activityErr    error
}

func (f *fakeGroupPresenceQueries) UpsertDingTalkBotIdentity(_ context.Context, params db.UpsertDingTalkBotIdentityParams) (pgtype.UUID, error) {
	f.identityCalls++
	f.identityParams = params
	return params.InstallationID, f.identityErr
}

func (f *fakeGroupPresenceQueries) RecordDingTalkGroupPresence(_ context.Context, params db.RecordDingTalkGroupPresenceParams) (pgtype.UUID, error) {
	f.calls++
	f.params = params
	return params.InstallationID, f.err
}

func (f *fakeGroupPresenceQueries) RecordDingTalkGroupActivity(_ context.Context, params db.RecordDingTalkGroupActivityParams) (pgtype.UUID, error) {
	f.activityCalls++
	f.activityParams = params
	return params.InstallationID, f.activityErr
}

type captureChatSession struct {
	ensure      engine.EnsureSessionInput
	ensureCalls int
	ensureErr   error
	append      engine.AppendInput
	appendErr   error
	media       engine.BindMediaInput
}

func (c *captureChatSession) EnsureSession(_ context.Context, in engine.EnsureSessionInput) (pgtype.UUID, error) {
	c.ensure = in
	c.ensureCalls++
	return pgtype.UUID{}, c.ensureErr
}
func (c *captureChatSession) MarkPendingFresh(context.Context, pgtype.UUID, string) error { return nil }
func (c *captureChatSession) AppendUserMessage(_ context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	c.append = in
	return engine.AppendResult{}, c.appendErr
}

func TestSessionBinder_RecordsActivityOnlyAfterSuccessfulGroupAppend(t *testing.T) {
	installationID := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	queries := &fakeGroupPresenceQueries{activityErr: errors.New("activity unavailable")}
	capture := &captureChatSession{}
	binder := &sessionBinder{
		session:       capture,
		groupPresence: &groupPresenceObserver{q: queries},
	}
	message := channel.InboundMessage{Source: channel.Source{
		ChatID:   "cid-platform",
		ChatType: channel.ChatTypeGroup,
	}}
	// Activity metadata is best effort after the durable append, so its own
	// failure does not turn the accepted message into an error.
	if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		InstallationID: installationID,
		Message:        message,
	}); err != nil {
		t.Fatalf("activity failure blocked append: %v", err)
	}
	if queries.activityCalls != 1 || queries.activityParams.InstallationID != installationID ||
		queries.activityParams.ConversationID != "cid-platform" {
		t.Fatalf("activity call = %d params %+v", queries.activityCalls, queries.activityParams)
	}

	appendErr := errors.New("append unavailable")
	queries.activityCalls = 0
	capture.appendErr = appendErr
	if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		InstallationID: installationID,
		Message:        message,
	}); !errors.Is(err, appendErr) {
		t.Fatalf("append error = %v, want %v", err, appendErr)
	}
	if queries.activityCalls != 0 {
		t.Fatalf("failed append recorded activity %d times", queries.activityCalls)
	}
}
func (c *captureChatSession) BindMediaRefs(_ context.Context, in engine.BindMediaInput) error {
	c.media = in
	return nil
}

func TestNewDingTalkResolverSetUsesDatabaseBackedIssueOrigin(t *testing.T) {
	set := NewDingTalkResolverSet(nil, nil, nil, nil, nil, nil)
	if set.OriginType != originDingTalkChat {
		t.Fatalf("OriginType = %q, want %q", set.OriginType, originDingTalkChat)
	}
}

func TestSessionBinder_MapsCommandTextAndMediaDeadline(t *testing.T) {
	var session, sender, inst, claim pgtype.UUID
	session.Bytes[0], sender.Bytes[0], inst.Bytes[0], claim.Bytes[0] = 2, 3, 4, 5
	session.Valid, sender.Valid, inst.Valid, claim.Valid = true, true, true, true
	capture := &captureChatSession{}
	binder := &sessionBinder{session: capture}
	_, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		SessionID: session, Sender: sender, InstallationID: inst, ClaimToken: claim,
		MediaPendingSeconds: 45,
		Message: channel.InboundMessage{
			MessageID: "m1", Text: "[Image]\n/issue fix login", CommandText: "/issue fix login", ForceFresh: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	in := capture.append
	if in.Body != "[Image]\n/issue fix login" || in.CommandText != "/issue fix login" {
		t.Fatalf("body/command = %q/%q", in.Body, in.CommandText)
	}
	if in.MediaPendingSeconds != 45 || !in.ForceFresh || in.SessionID != session || in.Sender != sender || in.InstallationID != inst || in.ClaimToken != claim {
		t.Fatalf("mapped append input = %+v", in)
	}
}

func TestSessionBinder_GroupUsesInstallationDefaultAgent(t *testing.T) {
	installationID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	defaultAgentID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	installerID := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	capture := &captureChatSession{}
	binder := &sessionBinder{session: capture}

	if _, err := binder.EnsureSession(context.Background(), engine.EnsureSessionParams{
		Installation: engine.ResolvedInstallation{
			ID:              installationID,
			WorkspaceID:     workspaceID,
			AgentID:         defaultAgentID,
			InstallerUserID: installerID,
		},
		Sender: installerID,
		Message: channel.InboundMessage{Source: channel.Source{
			ChatID:   "group-1",
			ChatType: channel.ChatTypeGroup,
		}},
	}); err != nil {
		t.Fatalf("ensure DingTalk group session: %v", err)
	}

	if capture.ensure.AgentID != defaultAgentID {
		t.Fatalf("group session agent = %v, want installation default %v", capture.ensure.AgentID, defaultAgentID)
	}
	if capture.ensure.InstallationID != installationID || capture.ensure.WorkspaceID != workspaceID {
		t.Fatalf("group session scope = %+v", capture.ensure)
	}
}

func TestGroupPresenceObserverRecordsExactBotWithoutChangingRouting(t *testing.T) {
	installationID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	agentID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	platform := botIdentityInstallation(t, "app-key", "robot-release")
	platform.ID = installationID

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		_, _ = w.Write([]byte(`{"chatbotInstanceVOList":[{"robotCode":"robot-other","name":"Other Bot"},{"robotCode":"robot-release","name":"Release Bot"}]}`))
	}))
	defer srv.Close()

	queries := &fakeGroupPresenceQueries{}
	raw, _ := json.Marshal(dingtalkRawEvent{ConversationTitle: "Platform team"})
	err := (&groupPresenceObserver{
		q:        queries,
		botNames: NewBotNameResolver(NewClient(nil, srv.URL), nil),
	}).Observe(context.Background(), engine.ResolvedInstallation{
		ID: installationID, WorkspaceID: workspaceID, AgentID: agentID, Platform: platform,
	}, channel.InboundMessage{
		Source: channel.Source{ChatID: "cid-platform", ChatType: channel.ChatTypeGroup},
		Raw:    raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if queries.calls != 1 || queries.params.InstallationID != installationID ||
		queries.params.WorkspaceID != workspaceID || queries.params.ConversationID != "cid-platform" ||
		queries.params.ConversationTitle != "Platform team" || queries.identityCalls != 1 ||
		queries.identityParams.BotName != "Release Bot" || queries.identityParams.BotIdentityIssue != "" {
		t.Fatalf("recorded identity/group presence = identity calls %d params %+v, group calls %d params %+v",
			queries.identityCalls, queries.identityParams, queries.calls, queries.params)
	}
}

func TestGroupPresenceObserverSkipsDirectMessagesAndRejectsMalformedGroupRaw(t *testing.T) {
	queries := &fakeGroupPresenceQueries{}
	observer := &groupPresenceObserver{q: queries}
	if err := observer.Observe(context.Background(), engine.ResolvedInstallation{}, channel.InboundMessage{
		Source: channel.Source{ChatType: channel.ChatTypeP2P}, Raw: []byte("{"),
	}); err != nil || queries.calls != 0 {
		t.Fatalf("direct observation = error %v calls %d, want no-op", err, queries.calls)
	}
	if err := observer.Observe(context.Background(), engine.ResolvedInstallation{}, channel.InboundMessage{
		Source: channel.Source{ChatType: channel.ChatTypeGroup}, Raw: []byte("{"),
	}); err == nil || queries.calls != 0 {
		t.Fatalf("malformed group observation = error %v calls %d", err, queries.calls)
	}
}

func TestGroupPresenceObserverPersistsPermissionIssueAndPropagatesDatabaseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"Forbidden.AccessDenied.AccessTokenPermissionDenied","message":"missing qyapi_chat_manage"}`))
	}))
	defer srv.Close()

	databaseErr := errors.New("database unavailable")
	queries := &fakeGroupPresenceQueries{err: databaseErr}
	platform := botIdentityInstallation(t, "app-key", "robot-release")
	raw, _ := json.Marshal(dingtalkRawEvent{ConversationTitle: "Platform"})
	err := (&groupPresenceObserver{
		q:        queries,
		botNames: NewBotNameResolver(NewClient(nil, srv.URL), nil),
	}).Observe(context.Background(), engine.ResolvedInstallation{
		Platform: platform,
	}, channel.InboundMessage{
		Source: channel.Source{ChatID: "cid-platform", ChatType: channel.ChatTypeGroup},
		Raw:    raw,
	})
	if !errors.Is(err, databaseErr) {
		t.Fatalf("observer error = %v, want database error", err)
	}
	if queries.identityParams.BotName != "" || queries.identityParams.BotIdentityIssue != botIdentityIssueMissingChatManage {
		t.Fatalf("permission fallback params = %+v", queries.identityParams)
	}
}

func TestSessionBinder_GroupPresenceFailureDoesNotBlockMessages(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	queries := &fakeGroupPresenceQueries{err: databaseErr}
	capture := &captureChatSession{}
	raw, _ := json.Marshal(dingtalkRawEvent{ConversationTitle: "Platform"})
	binder := &sessionBinder{
		session: capture,
		groupPresence: &groupPresenceObserver{
			q: queries,
		},
	}

	if _, err := binder.EnsureSession(context.Background(), engine.EnsureSessionParams{
		Installation: engine.ResolvedInstallation{
			ID:          pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
			WorkspaceID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
			AgentID:     pgtype.UUID{Bytes: [16]byte{3}, Valid: true},
		},
		Message: channel.InboundMessage{
			Source: channel.Source{ChatID: "cid-platform", ChatType: channel.ChatTypeGroup},
			Raw:    raw,
		},
	}); err != nil {
		t.Fatalf("group metadata failure blocked session: %v", err)
	}
	if capture.ensureCalls != 1 || queries.calls != 1 {
		t.Fatalf("session/group calls = %d/%d, want 1/1", capture.ensureCalls, queries.calls)
	}
}

func TestSessionBinder_SessionFailureDoesNotWriteGroupPresence(t *testing.T) {
	sessionErr := errors.New("session unavailable")
	queries := &fakeGroupPresenceQueries{}
	capture := &captureChatSession{ensureErr: sessionErr}
	raw, _ := json.Marshal(dingtalkRawEvent{ConversationTitle: "Platform"})
	binder := &sessionBinder{
		session:       capture,
		groupPresence: &groupPresenceObserver{q: queries},
	}

	_, err := binder.EnsureSession(context.Background(), engine.EnsureSessionParams{
		Message: channel.InboundMessage{
			Source: channel.Source{ChatID: "cid-platform", ChatType: channel.ChatTypeGroup},
			Raw:    raw,
		},
	})
	if !errors.Is(err, sessionErr) || queries.calls != 0 {
		t.Fatalf("session error/group calls = %v/%d, want original error and no metadata write", err, queries.calls)
	}
}

func TestSessionBinder_MapsMediaBodyAndIssueTarget(t *testing.T) {
	var message, session, workspace, sender, issue pgtype.UUID
	message.Bytes[0], session.Bytes[0], workspace.Bytes[0], sender.Bytes[0], issue.Bytes[0] = 1, 2, 3, 4, 5
	message.Valid, session.Valid, workspace.Valid, sender.Valid, issue.Valid = true, true, true, true, true
	ref := channel.MediaRef{Type: channel.MsgTypeImage, InlinePlaceholder: "[Image]", InlineIndex: 0}
	base := pgtype.Text{String: "[Image]\nfix login", Valid: true}
	capture := &captureChatSession{}
	binder := &sessionBinder{session: capture}
	if err := binder.BindMedia(context.Background(), engine.BindMediaParams{
		MessageID: message, SessionID: session, WorkspaceID: workspace, Sender: sender,
		IssueID: issue, IssueDescriptionBase: base, IssueCommandText: "/issue fix login", Body: "[Image]\nfix login", MediaRefs: []channel.MediaRef{ref},
	}); err != nil {
		t.Fatal(err)
	}
	got := capture.media
	if got.MessageID != message || got.SessionID != session || got.WorkspaceID != workspace || got.Sender != sender || got.IssueID != issue || got.IssueDescriptionBase != base || got.IssueCommandText != "/issue fix login" || got.Body != "[Image]\nfix login" || len(got.MediaRefs) != 1 || got.MediaRefs[0] != ref {
		t.Fatalf("mapped media input = %+v", got)
	}
}

func TestDingTalkSessionRouting_P2PCarriesStaffID(t *testing.T) {
	msg := channel.InboundMessage{Source: channel.Source{
		ChatID:   "cid-1",
		ChatType: channel.ChatTypeP2P,
		SenderID: "staff-7",
	}}
	key, cfg := dingtalkSessionRouting(msg)
	if key != "cid-1" {
		t.Errorf("binding key = %q, want conversation id", key)
	}
	var dc dingtalkBindingConfig
	if err := json.Unmarshal(cfg, &dc); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if dc.ConversationType != convTypeP2P || dc.ConversationID != "cid-1" || dc.StaffID != "staff-7" {
		t.Errorf("p2p config = %+v", dc)
	}
}

func TestDingTalkSessionRouting_GroupOmitsStaffID(t *testing.T) {
	msg := channel.InboundMessage{Source: channel.Source{
		ChatID:   "cid-2",
		ChatType: channel.ChatTypeGroup,
		SenderID: "staff-7",
	}}
	_, cfg := dingtalkSessionRouting(msg)
	var dc dingtalkBindingConfig
	_ = json.Unmarshal(cfg, &dc)
	if dc.ConversationType != convTypeGroup || dc.StaffID != "" {
		t.Errorf("group config must omit staff id: %+v", dc)
	}
}

func TestOutboundTarget_RoundTripsBindingConfig(t *testing.T) {
	_, cfg := dingtalkSessionRouting(channel.InboundMessage{Source: channel.Source{
		ChatID:   "cid-3",
		ChatType: channel.ChatTypeP2P,
		SenderID: "staff-3",
	}})
	target := outboundTarget(db.ChannelChatSessionBinding{ChannelChatID: "cid-3", Config: cfg})
	if target.ConversationType != convTypeP2P || target.StaffID != "staff-3" || target.ConversationID != "cid-3" {
		t.Errorf("round-tripped target = %+v", target)
	}
}

func TestOutboundTarget_FallsBackToChatID(t *testing.T) {
	target := outboundTarget(db.ChannelChatSessionBinding{ChannelChatID: "cid-4"})
	if target.ConversationType != convTypeGroup || target.ConversationID != "cid-4" {
		t.Errorf("missing config must fall back to a group send on chat id: %+v", target)
	}
}
