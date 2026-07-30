package lark

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectSyncDirectIssueTopicRoute(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()

	var directSchema bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'channel_issue_topic_binding'
			  AND column_name = 'installation_id'
		)`).Scan(&directSchema); err != nil {
		t.Fatalf("check direct topic schema: %v", err)
	}
	if !directSchema {
		t.Skip("direct Issue topic migration not applied")
	}

	const (
		workspaceID    = "d1ec7100-0000-4000-8000-000000000001"
		installationID = "d1ec7100-0000-4000-8000-000000000002"
		agentID        = "d1ec7100-0000-4000-8000-000000000003"
		userID         = "d1ec7100-0000-4000-8000-000000000004"
		issueID        = "d1ec7100-0000-4000-8000-000000000005"
		projectRouteID = "d1ec7100-0000-4000-8000-000000000006"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_notification_outbox WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_issue_topic_binding WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_project_binding WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	}
	clean()
	t.Cleanup(clean)

	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace (id, name, slug)
		VALUES ($1, 'direct topic test', 'direct-topic-route-test')`, workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_installation (
			id, workspace_id, agent_id, channel_type, config,
			installer_user_id, status
		) VALUES ($1, $2, $3, 'feishu', '{}', $4, 'active')`,
		installationID, workspaceID, agentID, userID); err != nil {
		t.Fatalf("insert installation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue (
			id, workspace_id, title, creator_type, creator_id, project_id
		) VALUES ($1, $2, 'Projectless direct topic', 'member', $3, NULL)`,
		issueID, workspaceID, userID); err != nil {
		t.Fatalf("insert Issue: %v", err)
	}

	store := newProjectSyncStore(pool)
	topic, err := store.createIssueTopic(
		ctx,
		pool,
		util.MustParseUUID(workspaceID),
		util.MustParseUUID(installationID),
		pgtype.UUID{},
		pgtype.UUID{},
		util.MustParseUUID(issueID),
		"oc_direct",
		"om_direct_root",
		"",
		"manual_topic_bind",
		util.MustParseUUID(userID),
	)
	if err != nil {
		t.Fatalf("create direct Issue topic: %v", err)
	}
	if topic.ProjectBindingID.Valid || topic.ProjectID.Valid {
		t.Fatalf(
			"direct topic unexpectedly carries Project route: project_binding=%v project=%v",
			topic.ProjectBindingID.Valid,
			topic.ProjectID.Valid,
		)
	}
	if topic.InstallationID != util.MustParseUUID(installationID) {
		t.Fatalf("installation mismatch: got %v", topic.InstallationID)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE issue SET status = 'in_progress'
		WHERE id = $1 AND workspace_id = $2`, issueID, workspaceID); err != nil {
		t.Fatalf("update Issue status: %v", err)
	}

	var (
		outboxProjectID        pgtype.UUID
		outboxProjectBindingID pgtype.UUID
		outboxTopicBindingID   pgtype.UUID
		eventType              string
	)
	if err := pool.QueryRow(ctx, `
		SELECT project_id, project_binding_id, issue_topic_binding_id, event_type
		FROM channel_notification_outbox
		WHERE workspace_id = $1 AND issue_id = $2`,
		workspaceID, issueID,
	).Scan(
		&outboxProjectID,
		&outboxProjectBindingID,
		&outboxTopicBindingID,
		&eventType,
	); err != nil {
		t.Fatalf("load direct notification: %v", err)
	}
	if outboxProjectID.Valid || outboxProjectBindingID.Valid {
		t.Fatalf(
			"direct notification unexpectedly carries Project route: project=%v binding=%v",
			outboxProjectID.Valid,
			outboxProjectBindingID.Valid,
		)
	}
	if outboxTopicBindingID != topic.ID || eventType != "issue_status_changed" {
		t.Fatalf(
			"direct notification route mismatch: topic=%v event=%q",
			outboxTopicBindingID,
			eventType,
		)
	}

	if err := store.finishProjectUnbind(
		ctx,
		pool,
		ChannelProjectBinding{
			ID:          util.MustParseUUID(projectRouteID),
			WorkspaceID: util.MustParseUUID(workspaceID),
		},
		"project_unbound",
	); err != nil {
		t.Fatalf("finish unrelated Project unbind: %v", err)
	}
	active, err := store.getIssueTopicByID(
		ctx,
		pool,
		util.MustParseUUID(workspaceID),
		topic.ID,
	)
	if err != nil {
		t.Fatalf("reload direct topic after Project unbind: %v", err)
	}
	if active.State != "active" {
		t.Fatalf("Project unbind changed direct topic state to %q", active.State)
	}

	service := &ProjectSyncService{store: store}
	if err := service.RevokeInstallationBindings(
		ctx,
		util.MustParseUUID(workspaceID),
		util.MustParseUUID(installationID),
	); err != nil {
		t.Fatalf("revoke installation routes: %v", err)
	}
	revoked, err := store.getIssueTopicByID(
		ctx,
		pool,
		util.MustParseUUID(workspaceID),
		topic.ID,
	)
	if err != nil {
		t.Fatalf("reload revoked direct topic: %v", err)
	}
	if revoked.State != "bot_revoked" {
		t.Fatalf("revoked direct topic state = %q, want bot_revoked", revoked.State)
	}

	var outboxStatus string
	if err := pool.QueryRow(ctx, `
		SELECT status
		FROM channel_notification_outbox
		WHERE workspace_id = $1 AND issue_id = $2`,
		workspaceID, issueID,
	).Scan(&outboxStatus); err != nil {
		t.Fatalf("load revoked direct notification: %v", err)
	}
	if outboxStatus != "dead" {
		t.Fatalf("revoked direct notification status = %q, want dead", outboxStatus)
	}
}

func TestBeginProjectBindingSwitchesBotWithoutTouchingDirectTopics(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()

	var directSchema bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'channel_issue_topic_binding'
			  AND column_name = 'installation_id'
		)`).Scan(&directSchema); err != nil {
		t.Fatalf("check direct topic schema: %v", err)
	}
	if !directSchema {
		t.Skip("direct Issue topic migration not applied")
	}

	const (
		workspaceID     = "d1ec7200-0000-4000-8000-000000000001"
		userID          = "d1ec7200-0000-4000-8000-000000000002"
		projectID       = "d1ec7200-0000-4000-8000-000000000003"
		installationAID = "d1ec7200-0000-4000-8000-000000000004"
		installationBID = "d1ec7200-0000-4000-8000-000000000005"
		agentAID        = "d1ec7200-0000-4000-8000-000000000006"
		agentBID        = "d1ec7200-0000-4000-8000-000000000007"
		directIssueID   = "d1ec7200-0000-4000-8000-000000000008"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_notification_outbox WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_issue_topic_binding WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_project_binding WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM project WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	}
	clean()
	t.Cleanup(clean)

	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed Project Bot switch: %v", err)
		}
	}
	seed(`
		INSERT INTO workspace (id, name, slug)
		VALUES ($1, 'project bot switch test', 'project-bot-switch-test')`,
		workspaceID,
	)
	seed(`
		INSERT INTO "user" (id, name, email)
		VALUES ($1, 'Project Admin', 'project-bot-switch@example.test')`,
		userID,
	)
	seed(`
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'admin')`,
		workspaceID,
		userID,
	)
	seed(`
		INSERT INTO project (id, workspace_id, title)
		VALUES ($1, $2, 'Project Bot Switch')`,
		projectID,
		workspaceID,
	)
	seed(`
		INSERT INTO channel_installation (
			id, workspace_id, agent_id, channel_type, config,
			installer_user_id, status
		) VALUES
			($1, $2, $3, 'feishu', '{"app_id":"switch-a"}', $4, 'active'),
			($5, $2, $6, 'feishu', '{"app_id":"switch-b"}', $4, 'active')`,
		installationAID,
		workspaceID,
		agentAID,
		userID,
		installationBID,
		agentBID,
	)

	store := newProjectSyncStore(pool)
	service := &ProjectSyncService{
		store:   store,
		queries: db.New(pool),
	}
	first, _, err := service.BeginProjectBinding(
		ctx,
		util.MustParseUUID(workspaceID),
		util.MustParseUUID(projectID),
		util.MustParseUUID(installationAID),
		util.MustParseUUID(userID),
	)
	if err != nil {
		t.Fatalf("begin first Project binding: %v", err)
	}

	direct, err := store.createIssueTopic(
		ctx,
		pool,
		util.MustParseUUID(workspaceID),
		util.MustParseUUID(installationAID),
		pgtype.UUID{},
		pgtype.UUID{},
		util.MustParseUUID(directIssueID),
		"oc_direct_switch",
		"om_direct_switch",
		"",
		"manual_topic_bind",
		util.MustParseUUID(userID),
	)
	if err != nil {
		t.Fatalf("create direct topic before Bot switch: %v", err)
	}

	second, _, err := service.BeginProjectBinding(
		ctx,
		util.MustParseUUID(workspaceID),
		util.MustParseUUID(projectID),
		util.MustParseUUID(installationBID),
		util.MustParseUUID(userID),
	)
	if err != nil {
		t.Fatalf("switch Project Bot: %v", err)
	}
	if second.InstallationID != util.MustParseUUID(installationBID) ||
		second.State != "pending_group" {
		t.Fatalf(
			"replacement binding = installation %v state %q",
			second.InstallationID,
			second.State,
		)
	}

	old, err := store.getProjectBindingByID(
		ctx,
		pool,
		util.MustParseUUID(workspaceID),
		first.ID,
	)
	if err != nil {
		t.Fatalf("load replaced Project binding: %v", err)
	}
	if old.State != "unbound" {
		t.Fatalf("replaced Project binding state = %q, want unbound", old.State)
	}

	stillDirect, err := store.getIssueTopicByID(
		ctx,
		pool,
		util.MustParseUUID(workspaceID),
		direct.ID,
	)
	if err != nil {
		t.Fatalf("load direct topic after Bot switch: %v", err)
	}
	if stillDirect.State != "active" {
		t.Fatalf("Bot switch changed direct topic state to %q", stillDirect.State)
	}

	same, confirmationCode, err := service.BeginProjectBinding(
		ctx,
		util.MustParseUUID(workspaceID),
		util.MustParseUUID(projectID),
		util.MustParseUUID(installationBID),
		util.MustParseUUID(userID),
	)
	if err != nil {
		t.Fatalf("select current Bot idempotently: %v", err)
	}
	if confirmationCode != "" {
		t.Fatalf("idempotent selection returned confirmation code %q", confirmationCode)
	}
	if same.ID != second.ID {
		t.Fatalf("idempotent selection binding = %v, want %v", same.ID, second.ID)
	}

	current, err := store.getCurrentProjectBinding(
		ctx,
		pool,
		util.MustParseUUID(workspaceID),
		util.MustParseUUID(projectID),
	)
	if err != nil {
		t.Fatalf("load current Project binding: %v", err)
	}
	if current.ID != second.ID || current.InstallationID != util.MustParseUUID(installationBID) {
		t.Fatalf(
			"current Project binding changed after idempotent selection: id=%v installation=%v",
			current.ID,
			current.InstallationID,
		)
	}
}
