package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeInstallationQueries struct {
	params db.GetChannelInstallationByAppIDParams
	row    db.ChannelInstallation
	err    error
	calls  int
}

type fakeValidatedInboundQueries struct {
	params db.DiscoverDingTalkGroupRouteParams
	row    db.DiscoverDingTalkGroupRouteRow
	err    error
	calls  int
}

type fakeSessionQueries struct {
	params       db.DeleteDingTalkStaleGroupChatBindingParams
	matches      []bool
	matchErrs    []error
	matchCalls   int
	deleteCounts []int64
	deleteErrs   []error
	deleteCalls  int
}

func (f *fakeSessionQueries) DingTalkGroupRouteMatchesAgent(context.Context, db.DingTalkGroupRouteMatchesAgentParams) (bool, error) {
	call := f.matchCalls
	f.matchCalls++
	var err error
	if call < len(f.matchErrs) {
		err = f.matchErrs[call]
	}
	match := true
	if call < len(f.matches) {
		match = f.matches[call]
	}
	return match, err
}

func (f *fakeSessionQueries) DeleteDingTalkStaleGroupChatBinding(_ context.Context, params db.DeleteDingTalkStaleGroupChatBindingParams) (int64, error) {
	f.params = params
	call := f.deleteCalls
	f.deleteCalls++
	var count int64
	if call < len(f.deleteCounts) {
		count = f.deleteCounts[call]
	}
	var err error
	if call < len(f.deleteErrs) {
		err = f.deleteErrs[call]
	}
	return count, err
}

func (f *fakeInstallationQueries) GetChannelInstallationByAppID(_ context.Context, params db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	f.params = params
	f.calls++
	return f.row, f.err
}

func (f *fakeValidatedInboundQueries) DiscoverDingTalkGroupRoute(_ context.Context, params db.DiscoverDingTalkGroupRouteParams) (db.DiscoverDingTalkGroupRouteRow, error) {
	f.params = params
	f.calls++
	return f.row, f.err
}

type captureChatSession struct {
	ensure      engine.EnsureSessionInput
	ensureCalls int
	ensureErr   error
	append      engine.AppendInput
	appendCalls int
	appended    int
	runGuard    bool
	media       engine.BindMediaInput
}

func (c *captureChatSession) EnsureSession(_ context.Context, in engine.EnsureSessionInput) (pgtype.UUID, error) {
	c.ensure = in
	c.ensureCalls++
	return pgtype.UUID{}, c.ensureErr
}
func (c *captureChatSession) MarkPendingFresh(context.Context, pgtype.UUID) error { return nil }
func (c *captureChatSession) AppendUserMessage(ctx context.Context, in engine.AppendInput) (engine.AppendResult, error) {
	c.append = in
	c.appendCalls++
	if c.runGuard && in.BeforeWrite != nil {
		if err := in.BeforeWrite(ctx, nil); err != nil {
			return engine.AppendResult{}, err
		}
	}
	c.appended++
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

func TestInstallationResolverGroupLookupIsReadOnly(t *testing.T) {
	var installationID, workspaceID, defaultAgentID, installerID pgtype.UUID
	installationID.Bytes[0], workspaceID.Bytes[0], defaultAgentID.Bytes[0], installerID.Bytes[0] = 1, 2, 3, 5
	installationID.Valid, workspaceID.Valid, defaultAgentID.Valid, installerID.Valid = true, true, true, true
	fake := &fakeInstallationQueries{row: db.ChannelInstallation{
		ID: installationID, WorkspaceID: workspaceID, AgentID: defaultAgentID,
		InstallerUserID: installerID, Status: "active",
	}}
	raw, _ := json.Marshal(dingtalkRawEvent{AppID: "app-key", ConversationTitle: "Platform team"})
	resolved, err := (&installationResolver{q: fake}).ResolveInstallation(context.Background(), channel.InboundMessage{
		Source: channel.Source{ChatID: "cid-platform", ChatType: channel.ChatTypeGroup},
		Raw:    raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != installationID || resolved.WorkspaceID != workspaceID || resolved.AgentID != defaultAgentID || resolved.InstallerUserID != installerID || !resolved.Active {
		t.Fatalf("resolved group installation = %+v", resolved)
	}
	if fake.params.ChannelType != string(TypeDingTalk) || fake.params.AppID != "app-key" {
		t.Fatalf("installation lookup params = %+v", fake.params)
	}
	platform, ok := resolved.Platform.(db.ChannelInstallation)
	if !ok || platform.AgentID != defaultAgentID {
		t.Fatalf("platform installation must retain default agent: %#v", resolved.Platform)
	}
}

func TestDingTalkResolversRejectMalformedCallbackRaw(t *testing.T) {
	t.Run("installation", func(t *testing.T) {
		fake := &fakeInstallationQueries{}
		_, err := (&installationResolver{q: fake}).ResolveInstallation(context.Background(), channel.InboundMessage{Raw: []byte("{")})
		if err == nil || fake.calls != 0 {
			t.Fatalf("malformed installation callback error=%v query calls=%d, want decode error before query", err, fake.calls)
		}
	})

	t.Run("validated group route", func(t *testing.T) {
		fake := &fakeValidatedInboundQueries{}
		_, err := (&validatedInboundResolver{q: fake}).ResolveValidatedInbound(
			context.Background(), engine.ResolvedInstallation{}, engine.ResolvedIdentity{},
			channel.InboundMessage{Source: channel.Source{ChatType: channel.ChatTypeGroup}, Raw: []byte("{")},
		)
		if err == nil || fake.calls != 0 {
			t.Fatalf("malformed discovery callback error=%v query calls=%d, want decode error before query", err, fake.calls)
		}
	})
}

func TestValidatedInboundResolverReturnsDiscoveryQueryErrorWithoutChangingRoute(t *testing.T) {
	queryErr := errors.New("discover route database unavailable")
	fake := &fakeValidatedInboundQueries{err: queryErr}
	inst := engine.ResolvedInstallation{AgentID: pgtype.UUID{Bytes: [16]byte{3}, Valid: true}, RouteRevision: 4}
	raw, _ := json.Marshal(dingtalkRawEvent{ConversationTitle: "Platform team"})

	resolved, err := (&validatedInboundResolver{q: fake}).ResolveValidatedInbound(
		context.Background(), inst, engine.ResolvedIdentity{},
		channel.InboundMessage{Source: channel.Source{ChatID: "cid-platform", ChatType: channel.ChatTypeGroup}, Raw: raw},
	)
	if !errors.Is(err, queryErr) {
		t.Fatalf("discovery query error = %v, want %v", err, queryErr)
	}
	if resolved.AgentID != inst.AgentID || resolved.RouteRevision != inst.RouteRevision || fake.calls != 1 {
		t.Fatalf("failed discovery changed route: resolved=%+v calls=%d", resolved, fake.calls)
	}
}

func TestDingTalkP2PSkipsGroupDiscoveryAndUsesDefaultAgentSession(t *testing.T) {
	var installationID, workspaceID, defaultAgentID, senderID pgtype.UUID
	installationID.Bytes[0], workspaceID.Bytes[0], defaultAgentID.Bytes[0], senderID.Bytes[0] = 1, 2, 3, 4
	installationID.Valid, workspaceID.Valid, defaultAgentID.Valid, senderID.Valid = true, true, true, true
	inst := engine.ResolvedInstallation{ID: installationID, WorkspaceID: workspaceID, AgentID: defaultAgentID}
	msg := channel.InboundMessage{Source: channel.Source{
		ChatID: "cid-direct", ChatType: channel.ChatTypeP2P, SenderID: "staff-7",
	}}
	discovery := &fakeValidatedInboundQueries{err: errors.New("must not query group routes for p2p")}

	resolved, err := (&validatedInboundResolver{q: discovery}).ResolveValidatedInbound(
		context.Background(), inst, engine.ResolvedIdentity{}, msg,
	)
	if err != nil || resolved.AgentID != defaultAgentID || discovery.calls != 0 {
		t.Fatalf("p2p validated resolution = %+v err=%v discovery calls=%d", resolved, err, discovery.calls)
	}

	queries := &fakeSessionQueries{matchErrs: []error{errors.New("must not query group route session fence for p2p")}}
	capture := &captureChatSession{}
	appendFenceCalls := 0
	binder := &sessionBinder{
		q: queries, session: capture,
		lockRouteForAppend: func(context.Context, pgx.Tx, db.LockDingTalkGroupRouteForAppendParams) error {
			appendFenceCalls++
			return errors.New("must not lock group route for p2p")
		},
	}
	if _, err := binder.EnsureSession(context.Background(), engine.EnsureSessionParams{
		Installation: inst,
		Sender:       senderID,
		Message:      msg,
	}); err != nil {
		t.Fatalf("p2p EnsureSession: %v", err)
	}
	if queries.matchCalls != 0 || queries.deleteCalls != 0 || capture.ensureCalls != 1 {
		t.Fatalf("p2p route calls = match %d delete %d ensure %d, want 0/0/1", queries.matchCalls, queries.deleteCalls, capture.ensureCalls)
	}
	if capture.ensure.AgentID != defaultAgentID || capture.ensure.BindingKey != "cid-direct" || capture.ensure.ChatType != channel.ChatTypeP2P {
		t.Fatalf("p2p session input = %+v, want default agent and direct-message binding", capture.ensure)
	}
	if _, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		SessionID:      pgtype.UUID{Bytes: [16]byte{5}, Valid: true},
		InstallationID: installationID,
		AgentID:        defaultAgentID,
		Message:        msg,
	}); err != nil {
		t.Fatalf("p2p AppendMessage: %v", err)
	}
	hasBeforeWrite := capture.append.BeforeWrite != nil
	if appendFenceCalls != 0 || capture.appended != 1 || hasBeforeWrite {
		t.Fatalf("p2p append = fence calls %d durable appends %d has beforeWrite %t, want 0/1/false", appendFenceCalls, capture.appended, hasBeforeWrite)
	}
}

func TestValidatedInboundResolverDiscoversGroupAndFinalizesAgent(t *testing.T) {
	var installationID, workspaceID, defaultAgentID, routeAgentID pgtype.UUID
	installationID.Bytes[0], workspaceID.Bytes[0], defaultAgentID.Bytes[0], routeAgentID.Bytes[0] = 1, 2, 3, 4
	installationID.Valid, workspaceID.Valid, defaultAgentID.Valid, routeAgentID.Valid = true, true, true, true
	fake := &fakeValidatedInboundQueries{row: db.DiscoverDingTalkGroupRouteRow{
		AgentID: routeAgentID, Revision: 7, AgentActive: true,
	}}
	raw, _ := json.Marshal(dingtalkRawEvent{ConversationTitle: "Platform team"})
	resolved, err := (&validatedInboundResolver{q: fake}).ResolveValidatedInbound(
		context.Background(),
		engine.ResolvedInstallation{ID: installationID, WorkspaceID: workspaceID, AgentID: defaultAgentID},
		engine.ResolvedIdentity{},
		channel.InboundMessage{
			Source: channel.Source{ChatID: "cid-platform", ChatType: channel.ChatTypeGroup},
			Raw:    raw,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AgentID != routeAgentID || resolved.RouteRevision != 7 {
		t.Fatalf("resolved route = agent %v revision %d, want agent %v revision 7", resolved.AgentID, resolved.RouteRevision, routeAgentID)
	}
	if fake.params.InstallationID != installationID || fake.params.WorkspaceID != workspaceID || fake.params.ConversationID != "cid-platform" || fake.params.ConversationTitle != "Platform team" {
		t.Fatalf("discovery params = %+v", fake.params)
	}
}

func TestValidatedInboundResolverPreservesArchivedRoute(t *testing.T) {
	var routeAgentID pgtype.UUID
	routeAgentID.Bytes[0], routeAgentID.Valid = 4, true
	fake := &fakeValidatedInboundQueries{row: db.DiscoverDingTalkGroupRouteRow{AgentID: routeAgentID}}
	raw, _ := json.Marshal(dingtalkRawEvent{})
	resolved, err := (&validatedInboundResolver{q: fake}).ResolveValidatedInbound(
		context.Background(), engine.ResolvedInstallation{}, engine.ResolvedIdentity{},
		channel.InboundMessage{Source: channel.Source{ChatType: channel.ChatTypeGroup}, Raw: raw},
	)
	if err != engine.ErrTargetAgentArchived {
		t.Fatalf("archived route error = %v, want %v", err, engine.ErrTargetAgentArchived)
	}
	if resolved.AgentID != routeAgentID {
		t.Fatalf("archived route agent = %v, want preserved %v", resolved.AgentID, routeAgentID)
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

func TestSessionBinder_GroupRouteFailureMatrix(t *testing.T) {
	queryErr := errors.New("route query failed")
	cleanupErr := errors.New("stale binding cleanup failed")
	ensureErr := errors.New("ensure session failed")
	for _, tc := range []struct {
		name         string
		queries      *fakeSessionQueries
		ensureErr    error
		wantErr      error
		wantContains string
		wantEnsures  int
	}{
		{name: "initial route query error", queries: &fakeSessionQueries{matchErrs: []error{queryErr}}, wantErr: queryErr, wantContains: "verify dingtalk group route"},
		{name: "initial stale binding cleanup error", queries: &fakeSessionQueries{matches: []bool{true}, deleteErrs: []error{cleanupErr}}, wantErr: cleanupErr, wantContains: "retire stale dingtalk group session"},
		{name: "underlying ensure session error", queries: &fakeSessionQueries{matches: []bool{true}}, ensureErr: ensureErr, wantErr: ensureErr, wantEnsures: 1},
		{name: "recheck route query error", queries: &fakeSessionQueries{matches: []bool{true, true}, matchErrs: []error{nil, queryErr}}, wantErr: queryErr, wantContains: "recheck dingtalk group route", wantEnsures: 1},
		{name: "recheck stale binding cleanup error", queries: &fakeSessionQueries{matches: []bool{true, true}, deleteErrs: []error{nil, cleanupErr}}, wantErr: cleanupErr, wantContains: "recheck dingtalk group session", wantEnsures: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := &captureChatSession{ensureErr: tc.ensureErr}
			_, err := (&sessionBinder{q: tc.queries, session: capture}).EnsureSession(context.Background(), dingtalkGroupEnsureParams())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("EnsureSession error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantContains != "" && !strings.Contains(err.Error(), tc.wantContains) {
				t.Fatalf("EnsureSession error = %q, want context %q", err, tc.wantContains)
			}
			if capture.ensureCalls != tc.wantEnsures {
				t.Fatalf("underlying ensure calls = %d, want %d", capture.ensureCalls, tc.wantEnsures)
			}
			if capture.appendCalls != 0 {
				t.Fatalf("failed session resolution appended %d messages, want 0", capture.appendCalls)
			}
		})
	}
}

func TestSessionBinder_RouteChangesAfterSessionCreation(t *testing.T) {
	queries := &fakeSessionQueries{matches: []bool{true, false}}
	capture := &captureChatSession{}
	_, err := (&sessionBinder{q: queries, session: capture}).EnsureSession(context.Background(), dingtalkGroupEnsureParams())
	if !errors.Is(err, engine.ErrRouteChanged) {
		t.Fatalf("post-create route conflict = %v, want route changed", err)
	}
	if capture.ensureCalls != 1 {
		t.Fatalf("underlying ensure calls = %d, want 1 before recheck conflict", capture.ensureCalls)
	}
	if capture.appendCalls != 0 {
		t.Fatalf("post-create route conflict appended %d messages, want 0", capture.appendCalls)
	}
}

func TestSessionBinder_RetryExhaustionReturnsRouteChanged(t *testing.T) {
	queries := &fakeSessionQueries{
		matches:      []bool{true, true, true, true, true, true},
		deleteCounts: []int64{0, 1, 0, 1, 0, 1},
	}
	capture := &captureChatSession{}
	_, err := (&sessionBinder{q: queries, session: capture}).EnsureSession(context.Background(), dingtalkGroupEnsureParams())
	if !errors.Is(err, engine.ErrRouteChanged) {
		t.Fatalf("retry exhaustion error = %v, want route changed", err)
	}
	if capture.ensureCalls != 3 || queries.matchCalls != 6 || queries.deleteCalls != 6 {
		t.Fatalf("retry attempts = ensure %d match %d cleanup %d, want 3/6/6", capture.ensureCalls, queries.matchCalls, queries.deleteCalls)
	}
	if capture.appendCalls != 0 {
		t.Fatalf("retry exhaustion appended %d messages, want 0", capture.appendCalls)
	}
}

func dingtalkGroupEnsureParams() engine.EnsureSessionParams {
	return engine.EnsureSessionParams{
		Installation: engine.ResolvedInstallation{
			ID:      pgtype.UUID{Bytes: [16]byte{8}, Valid: true},
			AgentID: pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
		},
		Message: channel.InboundMessage{Source: channel.Source{
			ChatID: "cid-routed", ChatType: channel.ChatTypeGroup,
		}},
	}
}

func TestSessionBinder_AppendFenceMapsStaleRevisionToRouteChanged(t *testing.T) {
	var installationID, agentID pgtype.UUID
	installationID.Bytes[0], agentID.Bytes[0] = 8, 9
	installationID.Valid, agentID.Valid = true, true
	capture := &captureChatSession{runGuard: true}
	var got db.LockDingTalkGroupRouteForAppendParams
	binder := &sessionBinder{
		session: capture,
		lockRouteForAppend: func(_ context.Context, _ pgx.Tx, params db.LockDingTalkGroupRouteForAppendParams) error {
			got = params
			return pgx.ErrNoRows
		},
	}
	_, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		InstallationID: installationID,
		AgentID:        agentID,
		RouteRevision:  11,
		Message: channel.InboundMessage{Source: channel.Source{
			ChatID: "cid-fenced", ChatType: channel.ChatTypeGroup,
		}},
	})
	if !errors.Is(err, engine.ErrRouteChanged) {
		t.Fatalf("stale append fence error = %v, want route changed", err)
	}
	if got.InstallationID != installationID || got.AgentID != agentID || got.ConversationID != "cid-fenced" || got.RouteRevision != 11 {
		t.Fatalf("append fence params = %+v", got)
	}
}

func TestSessionBinder_AppendFenceReturnsInfrastructureErrorWithoutAppend(t *testing.T) {
	lockErr := errors.New("route lock database unavailable")
	capture := &captureChatSession{runGuard: true}
	binder := &sessionBinder{
		session: capture,
		lockRouteForAppend: func(context.Context, pgx.Tx, db.LockDingTalkGroupRouteForAppendParams) error {
			return lockErr
		},
	}

	_, err := binder.AppendMessage(context.Background(), engine.AppendParams{
		InstallationID: pgtype.UUID{Bytes: [16]byte{8}, Valid: true},
		AgentID:        pgtype.UUID{Bytes: [16]byte{9}, Valid: true},
		RouteRevision:  11,
		Message: channel.InboundMessage{Source: channel.Source{
			ChatID: "cid-fenced", ChatType: channel.ChatTypeGroup,
		}},
	})
	if !errors.Is(err, lockErr) || !strings.Contains(err.Error(), "lock dingtalk group route for append") {
		t.Fatalf("append fence error = %v, want wrapped infrastructure error", err)
	}
	if capture.appendCalls != 1 || capture.appended != 0 {
		t.Fatalf("append calls=%d durable appends=%d, want 1/0", capture.appendCalls, capture.appended)
	}
}
