package dingtalk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeInstallationQueries struct {
	groupParams db.ResolveDingTalkInstallationForInboundGroupParams
	groupRow    db.ResolveDingTalkInstallationForInboundGroupRow
}

type fakeSessionQueries struct {
	params     db.DeleteDingTalkStaleGroupChatBindingParams
	matches    []bool
	matchCalls int
}

func (f *fakeSessionQueries) DingTalkGroupRouteMatchesAgent(context.Context, db.DingTalkGroupRouteMatchesAgentParams) (bool, error) {
	if f.matchCalls < len(f.matches) {
		match := f.matches[f.matchCalls]
		f.matchCalls++
		return match, nil
	}
	f.matchCalls++
	return true, nil
}

func (f *fakeSessionQueries) DeleteDingTalkStaleGroupChatBinding(_ context.Context, params db.DeleteDingTalkStaleGroupChatBindingParams) (int64, error) {
	f.params = params
	return 0, nil
}

func (f *fakeInstallationQueries) GetChannelInstallationByAppID(context.Context, db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{}, pgx.ErrNoRows
}

func (f *fakeInstallationQueries) ResolveDingTalkInstallationForInboundGroup(_ context.Context, params db.ResolveDingTalkInstallationForInboundGroupParams) (db.ResolveDingTalkInstallationForInboundGroupRow, error) {
	f.groupParams = params
	return f.groupRow, nil
}

type captureChatSession struct {
	ensure      engine.EnsureSessionInput
	ensureCalls int
	append      engine.AppendInput
	media       engine.BindMediaInput
}

func (c *captureChatSession) EnsureSession(_ context.Context, in engine.EnsureSessionInput) (pgtype.UUID, error) {
	c.ensure = in
	c.ensureCalls++
	return pgtype.UUID{}, nil
}
func (c *captureChatSession) MarkPendingFresh(context.Context, pgtype.UUID) error { return nil }
func (c *captureChatSession) AppendUserMessage(_ context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	c.append = in
	return engine.AppendResult{}, nil
}
func (c *captureChatSession) BindMediaRefs(_ context.Context, in engine.BindMediaInput) error {
	c.media = in
	return nil
}

func TestNewDingTalkResolverSetUsesDatabaseBackedIssueOrigin(t *testing.T) {
	set := NewDingTalkResolverSet(nil, nil, nil, nil, nil)
	if set.OriginType != originDingTalkChat {
		t.Fatalf("OriginType = %q, want %q", set.OriginType, originDingTalkChat)
	}
}

func TestInstallationResolverRoutesGroupToGroupAgent(t *testing.T) {
	var installationID, workspaceID, defaultAgentID, routeAgentID, installerID pgtype.UUID
	installationID.Bytes[0], workspaceID.Bytes[0], defaultAgentID.Bytes[0], routeAgentID.Bytes[0], installerID.Bytes[0] = 1, 2, 3, 4, 5
	installationID.Valid, workspaceID.Valid, defaultAgentID.Valid, routeAgentID.Valid, installerID.Valid = true, true, true, true, true
	fake := &fakeInstallationQueries{groupRow: db.ResolveDingTalkInstallationForInboundGroupRow{
		ID: installationID, WorkspaceID: workspaceID, AgentID: defaultAgentID,
		InstallerUserID: installerID, Status: "active", RouteAgentID: routeAgentID,
	}}
	raw, _ := json.Marshal(dingtalkRawEvent{AppID: "app-key", ConversationTitle: "Platform team"})
	resolved, err := (&installationResolver{q: fake}).ResolveInstallation(context.Background(), channel.InboundMessage{
		Source: channel.Source{ChatID: "cid-platform", ChatType: channel.ChatTypeGroup},
		Raw:    raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != installationID || resolved.WorkspaceID != workspaceID || resolved.AgentID != routeAgentID || resolved.InstallerUserID != installerID || !resolved.Active {
		t.Fatalf("resolved group installation = %+v", resolved)
	}
	if fake.groupParams.AppID != "app-key" || fake.groupParams.ConversationID != "cid-platform" || fake.groupParams.ConversationTitle != "Platform team" {
		t.Fatalf("group lookup params = %+v", fake.groupParams)
	}
	platform, ok := resolved.Platform.(db.ChannelInstallation)
	if !ok || platform.AgentID != defaultAgentID {
		t.Fatalf("platform installation must retain default agent: %#v", resolved.Platform)
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
	key, cfg := dingtalkSessionRouting(msg, pgtype.UUID{})
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
	_, cfg := dingtalkSessionRouting(msg, pgtype.UUID{})
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
	}}, pgtype.UUID{})
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

func TestSessionBinder_ClearsStaleGroupBindingAndStampsAgent(t *testing.T) {
	var installationID, agentID pgtype.UUID
	installationID.Bytes[0], agentID.Bytes[0] = 8, 9
	installationID.Valid, agentID.Valid = true, true
	queries := &fakeSessionQueries{}
	capture := &captureChatSession{}
	binder := &sessionBinder{q: queries, session: capture}
	if _, err := binder.EnsureSession(context.Background(), engine.EnsureSessionParams{
		Installation: engine.ResolvedInstallation{ID: installationID, AgentID: agentID},
		Message: channel.InboundMessage{Source: channel.Source{
			ChatID: "cid-routed", ChatType: channel.ChatTypeGroup,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if queries.params.InstallationID != installationID || queries.params.ConversationID != "cid-routed" || queries.params.AgentID != agentID {
		t.Fatalf("stale binding guard params = %+v", queries.params)
	}
	var cfg dingtalkBindingConfig
	if err := json.Unmarshal(capture.ensure.BindingConfig, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.AgentID == "" {
		t.Fatalf("group binding config did not stamp routed agent: %+v", cfg)
	}
}

func TestSessionBinder_StopsWhenGroupRouteChanged(t *testing.T) {
	queries := &fakeSessionQueries{matches: []bool{false}}
	capture := &captureChatSession{}
	binder := &sessionBinder{q: queries, session: capture}
	_, err := binder.EnsureSession(context.Background(), engine.EnsureSessionParams{
		Installation: engine.ResolvedInstallation{ID: pgtype.UUID{Valid: true}, AgentID: pgtype.UUID{Valid: true}},
		Message: channel.InboundMessage{Source: channel.Source{
			ChatID: "cid-reassigned", ChatType: channel.ChatTypeGroup,
		}},
	})
	if err == nil || capture.ensureCalls != 0 {
		t.Fatalf("route change err=%v ensure calls=%d, want error before session creation", err, capture.ensureCalls)
	}
}
