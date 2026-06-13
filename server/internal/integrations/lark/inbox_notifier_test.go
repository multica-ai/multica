package lark

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeInboxNotifierQueries struct {
	rows         []db.ListActiveLarkUserBindingsByMemberRow
	err          error
	arg          db.ListActiveLarkUserBindingsByMemberParams
	issue        db.Issue
	issueErr     error
	issueArg     pgtype.UUID
	workspace    db.Workspace
	workspaceErr error
	workspaceArg pgtype.UUID
	pref         db.NotificationPreference
	prefErr      error
	prefArg      db.GetNotificationPreferenceParams
	claims       map[string]bool
	claimCalls   int
	claimArg     db.ClaimLarkInboxNotificationDeliveryParams
	deleteCalls  int
	deleteArg    db.DeleteLarkInboxNotificationDeliveryParams
	deleteCtxErr error
}

func (f *fakeInboxNotifierQueries) GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error) {
	f.issueArg = id
	if f.issueErr != nil {
		return db.Issue{}, f.issueErr
	}
	return f.issue, nil
}

func (f *fakeInboxNotifierQueries) GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error) {
	f.workspaceArg = id
	if f.workspaceErr != nil {
		return db.Workspace{}, f.workspaceErr
	}
	return f.workspace, nil
}

func (f *fakeInboxNotifierQueries) GetNotificationPreference(ctx context.Context, arg db.GetNotificationPreferenceParams) (db.NotificationPreference, error) {
	f.prefArg = arg
	if f.prefErr != nil {
		return db.NotificationPreference{}, f.prefErr
	}
	return f.pref, nil
}

func (f *fakeInboxNotifierQueries) ListActiveLarkUserBindingsByMember(ctx context.Context, arg db.ListActiveLarkUserBindingsByMemberParams) ([]db.ListActiveLarkUserBindingsByMemberRow, error) {
	f.arg = arg
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeInboxNotifierQueries) ClaimLarkInboxNotificationDelivery(ctx context.Context, arg db.ClaimLarkInboxNotificationDeliveryParams) (bool, error) {
	f.claimCalls++
	f.claimArg = arg
	if f.claims == nil {
		f.claims = map[string]bool{}
	}
	key := uuidString(arg.InboxItemID) + "|" + uuidString(arg.InstallationID) + "|" + arg.LarkOpenID
	if f.claims[key] {
		return false, nil
	}
	f.claims[key] = true
	return true, nil
}

func (f *fakeInboxNotifierQueries) DeleteLarkInboxNotificationDelivery(ctx context.Context, arg db.DeleteLarkInboxNotificationDeliveryParams) error {
	f.deleteCalls++
	f.deleteArg = arg
	f.deleteCtxErr = ctx.Err()
	if f.claims != nil {
		key := uuidString(arg.InboxItemID) + "|" + uuidString(arg.InstallationID) + "|" + arg.LarkOpenID
		delete(f.claims, key)
	}
	return nil
}

func TestInboxNotifierSendsDMViaActorAgentBot(t *testing.T) {
	workspaceID := mustUUID("11111111-1111-1111-1111-111111111111")
	userID := mustUUID("22222222-2222-2222-2222-222222222222")
	otherAgentID := mustUUID("33333333-3333-3333-3333-333333333333")
	actorAgentID := mustUUID("44444444-4444-4444-4444-444444444444")
	q := &fakeInboxNotifierQueries{
		rows: []db.ListActiveLarkUserBindingsByMemberRow{
			inboxBindingRow(workspaceID, userID, otherAgentID, "cli_other", "ou_other"),
			inboxBindingRow(workspaceID, userID, actorAgentID, "cli_actor", "ou_actor"),
		},
		prefErr: pgx.ErrNoRows,
	}
	api := &stubAPIClientWithRecorder{configured: true}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{})

	err := notifier.notify(context.Background(), map[string]any{
		"item": map[string]any{
			"id":             "55555555-5555-5555-5555-555555555555",
			"workspace_id":   uuidString(workspaceID),
			"recipient_type": "member",
			"recipient_id":   uuidString(userID),
			"type":           "quick_create_failed",
			"severity":       "action_required",
			"title":          "Quick create failed",
			"body":           "agent exited with code 1",
			"actor_type":     "agent",
			"actor_id":       uuidString(actorAgentID),
		},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if q.arg.WorkspaceID != workspaceID || q.arg.MulticaUserID != userID {
		t.Fatalf("binding lookup arg = %+v", q.arg)
	}
	if q.claimCalls != 1 || q.claimArg.LarkOpenID != "ou_actor" {
		t.Fatalf("unexpected delivery claim calls=%d arg=%+v", q.claimCalls, q.claimArg)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.directCardsOut) != 1 {
		t.Fatalf("expected one direct card send, got %d", len(api.directCardsOut))
	}
	got := api.directCardsOut[0]
	if got.OpenID != "ou_actor" {
		t.Fatalf("OpenID = %q, want actor bot binding recipient ou_actor", got.OpenID)
	}
	if got.InstallationID.AppID != "cli_actor" {
		t.Fatalf("AppID = %q, want cli_actor", got.InstallationID.AppID)
	}
	if got.InstallationID.Region != RegionLark {
		t.Fatalf("Region = %q, want %q", got.InstallationID.Region, RegionLark)
	}
	if !strings.Contains(got.CardJSON, `"title":{"content":"Quick create failed"`) ||
		!strings.Contains(got.CardJSON, `"tag":"lark_md"`) ||
		!strings.Contains(got.CardJSON, "Quick create failed") ||
		!strings.Contains(got.CardJSON, "agent exited with code 1") {
		t.Fatalf("unexpected notification card: %q", got.CardJSON)
	}
}

func TestInboxNotifierSkipsDuplicateDeliveryClaim(t *testing.T) {
	workspaceID := mustUUID("11111111-1111-1111-1111-111111111111")
	userID := mustUUID("22222222-2222-2222-2222-222222222222")
	actorAgentID := mustUUID("44444444-4444-4444-4444-444444444444")
	itemID := "55555555-5555-5555-5555-555555555555"
	q := &fakeInboxNotifierQueries{
		rows: []db.ListActiveLarkUserBindingsByMemberRow{
			inboxBindingRow(workspaceID, userID, actorAgentID, "cli_actor", "ou_actor"),
		},
		prefErr: pgx.ErrNoRows,
	}
	api := &stubAPIClientWithRecorder{configured: true}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{})
	payload := map[string]any{
		"item": map[string]any{
			"id":             itemID,
			"workspace_id":   uuidString(workspaceID),
			"recipient_type": "member",
			"recipient_id":   uuidString(userID),
			"type":           "quick_create_failed",
			"severity":       "action_required",
			"title":          "Quick create failed",
			"actor_type":     "agent",
			"actor_id":       uuidString(actorAgentID),
		},
	}

	if err := notifier.notify(context.Background(), payload); err != nil {
		t.Fatalf("first notify: %v", err)
	}
	if err := notifier.notify(context.Background(), payload); err != nil {
		t.Fatalf("duplicate notify: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.directCardsOut) != 1 {
		t.Fatalf("duplicate delivery claim should send one card, got %d", len(api.directCardsOut))
	}
	if q.claimCalls != 2 {
		t.Fatalf("expected both attempts to claim delivery, got %d", q.claimCalls)
	}
}

func TestInboxNotifierHonorsMutedEventPreference(t *testing.T) {
	workspaceID := mustUUID("11111111-1111-1111-1111-111111111111")
	userID := mustUUID("22222222-2222-2222-2222-222222222222")
	actorAgentID := mustUUID("44444444-4444-4444-4444-444444444444")
	q := &fakeInboxNotifierQueries{
		rows: []db.ListActiveLarkUserBindingsByMemberRow{
			inboxBindingRow(workspaceID, userID, actorAgentID, "cli_actor", "ou_actor"),
		},
		pref: db.NotificationPreference{
			WorkspaceID: workspaceID,
			UserID:      userID,
			Preferences: []byte(`{"agent_activity":"muted"}`),
		},
	}
	api := &stubAPIClientWithRecorder{configured: true}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{})

	err := notifier.notify(context.Background(), map[string]any{
		"item": map[string]any{
			"id":             "55555555-5555-5555-5555-555555555555",
			"workspace_id":   uuidString(workspaceID),
			"recipient_type": "member",
			"recipient_id":   uuidString(userID),
			"type":           "task_failed",
			"severity":       "action_required",
			"title":          "Task failed",
			"actor_type":     "agent",
			"actor_id":       uuidString(actorAgentID),
		},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if q.arg.WorkspaceID.Valid {
		t.Fatalf("muted notification should not query Lark bindings")
	}
	if q.claimCalls != 0 {
		t.Fatalf("muted notification should not claim delivery, got %d calls", q.claimCalls)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.directCardsOut) != 0 {
		t.Fatalf("muted notification should not send cards, got %d", len(api.directCardsOut))
	}
}

func TestInboxNotifierPreferenceLookupFailureFallsThrough(t *testing.T) {
	workspaceID := mustUUID("11111111-1111-1111-1111-111111111111")
	userID := mustUUID("22222222-2222-2222-2222-222222222222")
	actorAgentID := mustUUID("44444444-4444-4444-4444-444444444444")
	q := &fakeInboxNotifierQueries{
		rows: []db.ListActiveLarkUserBindingsByMemberRow{
			inboxBindingRow(workspaceID, userID, actorAgentID, "cli_actor", "ou_actor"),
		},
		prefErr: errors.New("preference db unavailable"),
	}
	api := &stubAPIClientWithRecorder{configured: true}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{})

	err := notifier.notify(context.Background(), map[string]any{
		"item": map[string]any{
			"id":             "55555555-5555-5555-5555-555555555555",
			"workspace_id":   uuidString(workspaceID),
			"recipient_type": "member",
			"recipient_id":   uuidString(userID),
			"type":           "task_failed",
			"severity":       "action_required",
			"title":          "Task failed",
			"actor_type":     "agent",
			"actor_id":       uuidString(actorAgentID),
		},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.directCardsOut) != 1 {
		t.Fatalf("preference lookup failure should not drop notification, got %d sends", len(api.directCardsOut))
	}
}

func TestInboxNotifierReleasesClaimWhenSendFails(t *testing.T) {
	workspaceID := mustUUID("11111111-1111-1111-1111-111111111111")
	userID := mustUUID("22222222-2222-2222-2222-222222222222")
	actorAgentID := mustUUID("44444444-4444-4444-4444-444444444444")
	q := &fakeInboxNotifierQueries{
		rows: []db.ListActiveLarkUserBindingsByMemberRow{
			inboxBindingRow(workspaceID, userID, actorAgentID, "cli_actor", "ou_actor"),
		},
		prefErr: pgx.ErrNoRows,
	}
	api := &stubAPIClientWithRecorder{configured: true, sendErr: errors.New("lark 5xx")}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{})
	payload := map[string]any{
		"item": map[string]any{
			"id":             "55555555-5555-5555-5555-555555555555",
			"workspace_id":   uuidString(workspaceID),
			"recipient_type": "member",
			"recipient_id":   uuidString(userID),
			"type":           "task_failed",
			"severity":       "action_required",
			"title":          "Task failed",
			"actor_type":     "agent",
			"actor_id":       uuidString(actorAgentID),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := notifier.notify(ctx, payload); err == nil {
		t.Fatal("expected send failure")
	}
	if q.deleteCalls != 1 {
		t.Fatalf("send failure should release delivery claim, got %d deletes", q.deleteCalls)
	}
	if q.deleteCtxErr != nil {
		t.Fatalf("release claim should use a fresh context, got ctx err %v", q.deleteCtxErr)
	}
	api.sendErr = nil
	if err := notifier.notify(context.Background(), payload); err != nil {
		t.Fatalf("retry notify: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.directCardsOut) != 1 {
		t.Fatalf("retry after released claim should send one card, got %d", len(api.directCardsOut))
	}
}

func TestInboxNotifierFallsBackToAssigneeAgentBot(t *testing.T) {
	workspaceID := mustUUID("11111111-1111-1111-1111-111111111111")
	userID := mustUUID("22222222-2222-2222-2222-222222222222")
	otherAgentID := mustUUID("33333333-3333-3333-3333-333333333333")
	assigneeAgentID := mustUUID("44444444-4444-4444-4444-444444444444")
	issueID := mustUUID("55555555-5555-5555-5555-555555555555")
	q := &fakeInboxNotifierQueries{
		rows: []db.ListActiveLarkUserBindingsByMemberRow{
			inboxBindingRow(workspaceID, userID, otherAgentID, "cli_other", "ou_other"),
			inboxBindingRow(workspaceID, userID, assigneeAgentID, "cli_assignee", "ou_assignee"),
		},
		prefErr: pgx.ErrNoRows,
		issue: db.Issue{
			ID:           issueID,
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
			AssigneeID:   assigneeAgentID,
		},
	}
	api := &stubAPIClientWithRecorder{configured: true}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{})

	err := notifier.notify(context.Background(), map[string]any{
		"item": map[string]any{
			"id":             "66666666-6666-6666-6666-666666666666",
			"workspace_id":   uuidString(workspaceID),
			"recipient_type": "member",
			"recipient_id":   uuidString(userID),
			"type":           "status_changed",
			"severity":       "info",
			"issue_id":       uuidString(issueID),
			"title":          "Synced status changed",
			"actor_type":     "system",
		},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if q.issueArg != issueID {
		t.Fatalf("GetIssue arg = %v, want %v", q.issueArg, issueID)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.directCardsOut) != 1 {
		t.Fatalf("expected one direct card send, got %d", len(api.directCardsOut))
	}
	got := api.directCardsOut[0]
	if got.OpenID != "ou_assignee" {
		t.Fatalf("OpenID = %q, want assignee bot binding recipient ou_assignee", got.OpenID)
	}
	if got.InstallationID.AppID != "cli_assignee" {
		t.Fatalf("AppID = %q, want cli_assignee", got.InstallationID.AppID)
	}
}

func TestInboxNotifierSkipsWhenNoAgentBotMatches(t *testing.T) {
	workspaceID := mustUUID("11111111-1111-1111-1111-111111111111")
	userID := mustUUID("22222222-2222-2222-2222-222222222222")
	otherAgentID := mustUUID("33333333-3333-3333-3333-333333333333")
	actorAgentID := mustUUID("44444444-4444-4444-4444-444444444444")
	q := &fakeInboxNotifierQueries{
		rows: []db.ListActiveLarkUserBindingsByMemberRow{
			inboxBindingRow(workspaceID, userID, otherAgentID, "cli_other", "ou_other"),
		},
		prefErr: pgx.ErrNoRows,
	}
	api := &stubAPIClientWithRecorder{configured: true}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{})

	err := notifier.notify(context.Background(), map[string]any{
		"item": map[string]any{
			"id":             "55555555-5555-5555-5555-555555555555",
			"workspace_id":   uuidString(workspaceID),
			"recipient_type": "member",
			"recipient_id":   uuidString(userID),
			"type":           "quick_create_failed",
			"severity":       "action_required",
			"title":          "Quick create failed",
			"actor_type":     "agent",
			"actor_id":       uuidString(actorAgentID),
		},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.directCardsOut) != 0 {
		t.Fatalf("expected no direct card send for unmatched agent bot, got %d", len(api.directCardsOut))
	}
}

func TestInboxNotifierSkipsNonMemberRecipients(t *testing.T) {
	q := &fakeInboxNotifierQueries{}
	api := &stubAPIClientWithRecorder{configured: true}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{})

	err := notifier.notify(context.Background(), map[string]any{
		"item": map[string]any{
			"workspace_id":   "11111111-1111-1111-1111-111111111111",
			"recipient_type": "agent",
			"recipient_id":   "22222222-2222-2222-2222-222222222222",
			"title":          "Ignored",
		},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if q.arg.WorkspaceID.Valid {
		t.Fatalf("non-member recipient should not query bindings")
	}
}

func TestInboxNotifierSendsNewCommentAsMarkdownCard(t *testing.T) {
	workspaceID := mustUUID("11111111-1111-1111-1111-111111111111")
	userID := mustUUID("22222222-2222-2222-2222-222222222222")
	actorAgentID := mustUUID("44444444-4444-4444-4444-444444444444")
	q := &fakeInboxNotifierQueries{
		rows: []db.ListActiveLarkUserBindingsByMemberRow{
			inboxBindingRow(workspaceID, userID, actorAgentID, "cli_actor", "ou_actor"),
		},
		workspace: db.Workspace{ID: workspaceID, Slug: "tide-server", IssuePrefix: "TID"},
		prefErr:   pgx.ErrNoRows,
	}
	api := &stubAPIClientWithRecorder{configured: true}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{
		PublicURL: "https://multica.test",
	})

	err := notifier.notify(context.Background(), map[string]any{
		"item": map[string]any{
			"id":             "55555555-5555-5555-5555-555555555555",
			"workspace_id":   uuidString(workspaceID),
			"recipient_type": "member",
			"recipient_id":   uuidString(userID),
			"type":           "new_comment",
			"severity":       "info",
			"issue_id":       "66666666-6666-6666-6666-666666666666",
			"title":          "Issue updated",
			"body":           "**Result**\n- Cleaned up\n- Kept `tide`",
			"actor_type":     "agent",
			"actor_id":       uuidString(actorAgentID),
		},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.directCardsOut) != 1 {
		t.Fatalf("expected one direct card send for new_comment, got %d", len(api.directCardsOut))
	}
	card := api.directCardsOut[0].CardJSON
	for _, want := range []string{`"tag":"lark_md"`, "**Agent commented**", "**Result**", "View in Multica", "https://multica.test/tide-server/issues/66666666-6666-6666-6666-666666666666"} {
		if !strings.Contains(card, want) {
			t.Fatalf("new_comment card missing %q: %s", want, card)
		}
	}
}

func TestInboxNotifierRejectsMissingInboxItemID(t *testing.T) {
	workspaceID := mustUUID("11111111-1111-1111-1111-111111111111")
	userID := mustUUID("22222222-2222-2222-2222-222222222222")
	q := &fakeInboxNotifierQueries{}
	api := &stubAPIClientWithRecorder{configured: true}
	notifier := NewInboxNotifier(q, stubCredentialsResolver{secret: "secret"}, api, InboxNotifierConfig{})

	err := notifier.notify(context.Background(), map[string]any{
		"item": map[string]any{
			"workspace_id":   uuidString(workspaceID),
			"recipient_type": "member",
			"recipient_id":   uuidString(userID),
			"title":          "Missing id",
		},
	})
	if err == nil {
		t.Fatal("expected missing inbox item id error")
	}
	if q.arg.WorkspaceID.Valid {
		t.Fatalf("missing inbox item id should not query bindings")
	}
}

func inboxBindingRow(workspaceID, userID, agentID pgtype.UUID, appID, openID string) db.ListActiveLarkUserBindingsByMemberRow {
	return db.ListActiveLarkUserBindingsByMemberRow{
		LarkUserBinding: db.LarkUserBinding{
			WorkspaceID:    workspaceID,
			MulticaUserID:  userID,
			InstallationID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			LarkOpenID:     openID,
		},
		LarkInstallation: db.LarkInstallation{
			ID:          mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			WorkspaceID: workspaceID,
			AgentID:     agentID,
			AppID:       appID,
			Status:      string(InstallationActive),
			Region:      string(RegionLark),
		},
	}
}
