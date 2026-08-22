package channelnotify

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type resolverDBFixture struct {
	t           *testing.T
	ctx         context.Context
	tx          pgx.Tx
	resolver    *Resolver
	workspaceID pgtype.UUID
	recipientID pgtype.UUID
	issueID     pgtype.UUID
	installerID pgtype.UUID
}

func resolverIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("resolver integration tests require Postgres: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("resolver integration tests require Postgres: %v", err)
	}
	var present bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.channel_installation') IS NOT NULL").Scan(&present); err != nil || !present {
		pool.Close()
		t.Skip("resolver integration tests require a migrated database")
	}
	t.Cleanup(pool.Close)
	return pool
}

func newResolverDBFixture(t *testing.T) *resolverDBFixture {
	t.Helper()
	ctx := context.Background()
	tx, err := resolverIntegrationPool(t).Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	f := &resolverDBFixture{t: t, ctx: ctx, tx: tx}
	f.workspaceID = f.newUUID()
	f.recipientID = f.newUUID()
	f.installerID = f.recipientID
	f.issueID = f.newUUID()

	if _, err := tx.Exec(ctx,
		`INSERT INTO "user" (id, name, email) VALUES ($1, 'Recipient', $2)`,
		f.recipientID, fmt.Sprintf("resolver-%s@example.test", uuid.NewString())); err != nil {
		t.Fatalf("seed recipient: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO workspace (id, name, slug) VALUES ($1, 'Resolver Test', $2)`,
		f.workspaceID, "resolver-"+uuid.NewString()); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		f.workspaceID, f.recipientID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO issue (id, workspace_id, title, creator_type, creator_id)
VALUES ($1, $2, 'Resolve a notification target', 'member', $3)
`, f.issueID, f.workspaceID, f.recipientID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	f.resolver = NewResolver(db.New(tx))
	return f
}

func (f *resolverDBFixture) newUUID() pgtype.UUID {
	f.t.Helper()
	return util.MustParseUUID(uuid.NewString())
}

func (f *resolverDBFixture) addAgent(name string) pgtype.UUID {
	f.t.Helper()
	id := f.newUUID()
	if _, err := f.tx.Exec(f.ctx, `
INSERT INTO agent (id, workspace_id, name, runtime_mode)
VALUES ($1, $2, $3, 'local')
`, id, f.workspaceID, name); err != nil {
		f.t.Fatalf("insert agent %q: %v", name, err)
	}
	return id
}

func (f *resolverDBFixture) assign(agentID pgtype.UUID) {
	f.t.Helper()
	if _, err := f.tx.Exec(f.ctx, `
UPDATE issue SET assignee_type = 'agent', assignee_id = $1 WHERE id = $2
`, agentID, f.issueID); err != nil {
		f.t.Fatalf("assign agent: %v", err)
	}
}

func (f *resolverDBFixture) subscribe(agentID pgtype.UUID, createdAt time.Time) {
	f.t.Helper()
	if _, err := f.tx.Exec(f.ctx, `
INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason, created_at)
VALUES ($1, 'agent', $2, 'commenter', $3)
`, f.issueID, agentID, createdAt); err != nil {
		f.t.Fatalf("subscribe agent: %v", err)
	}
}

func (f *resolverDBFixture) install(agentID pgtype.UUID, channelType channel.Type, status string) pgtype.UUID {
	f.t.Helper()
	id := f.newUUID()
	if _, err := f.tx.Exec(f.ctx, `
INSERT INTO channel_installation (
    id, workspace_id, agent_id, channel_type, status, installer_user_id
) VALUES ($1, $2, $3, $4, $5, $6)
`, id, f.workspaceID, agentID, string(channelType), status, f.installerID); err != nil {
		f.t.Fatalf("install %s agent: %v", channelType, err)
	}
	return id
}

func (f *resolverDBFixture) bind(installationID pgtype.UUID, channelType channel.Type, externalID string) {
	f.t.Helper()
	if _, err := f.tx.Exec(f.ctx, `
INSERT INTO channel_user_binding (
    workspace_id, multica_user_id, installation_id, channel_type, channel_user_id
) VALUES ($1, $2, $3, $4, $5)
`, f.workspaceID, f.recipientID, installationID, string(channelType), externalID); err != nil {
		f.t.Fatalf("bind recipient to %s installation: %v", channelType, err)
	}
}

func (f *resolverDBFixture) resolve(channelType channel.Type) (Target, bool, error) {
	return f.resolver.Resolve(f.ctx, Notification{
		WorkspaceID: f.workspaceID,
		RecipientID: f.recipientID,
		IssueID:     f.issueID,
	}, channelType)
}

func TestResolverPrefersCurrentAssigneeAgent(t *testing.T) {
	f := newResolverDBFixture(t)
	assignee := f.addAgent("Assignee")
	subscriber := f.addAgent("Subscriber")
	f.assign(assignee)
	f.subscribe(subscriber, time.Now().Add(-time.Hour))
	assigneeInstallation := f.install(assignee, channel.TypeFeishu, "active")
	f.bind(assigneeInstallation, channel.TypeFeishu, "ou_assignee")
	subscriberInstallation := f.install(subscriber, channel.TypeFeishu, "active")
	f.bind(subscriberInstallation, channel.TypeFeishu, "ou_subscriber")

	target, ok, err := f.resolve(channel.TypeFeishu)
	if err != nil || !ok {
		t.Fatalf("resolve target: ok=%v err=%v", ok, err)
	}
	if target.AgentID != assignee || target.InstallationID != assigneeInstallation {
		t.Fatalf("target = %+v, want assignee %v", target, assignee)
	}
}

func TestResolverFallsBackWhenAssigneeHasNoEligibleInstallationOrBinding(t *testing.T) {
	f := newResolverDBFixture(t)
	assignee := f.addAgent("Unbound Assignee")
	subscriber := f.addAgent("Bound Subscriber")
	f.assign(assignee)
	f.install(assignee, channel.TypeFeishu, "active")
	f.subscribe(subscriber, time.Now())
	subscriberInstallation := f.install(subscriber, channel.TypeFeishu, "active")
	f.bind(subscriberInstallation, channel.TypeFeishu, "ou_fallback")

	target, ok, err := f.resolve(channel.TypeFeishu)
	if err != nil || !ok {
		t.Fatalf("resolve fallback: ok=%v err=%v", ok, err)
	}
	if target.AgentID != subscriber {
		t.Fatalf("target agent = %v, want subscriber %v", target.AgentID, subscriber)
	}
}

func TestResolverFallsBackToFirstActiveAgentSubscriber(t *testing.T) {
	f := newResolverDBFixture(t)
	first := f.addAgent("First Subscriber")
	second := f.addAgent("Second Subscriber")
	f.subscribe(second, time.Now())
	f.subscribe(first, time.Now().Add(-time.Minute))
	firstInstallation := f.install(first, channel.TypeFeishu, "active")
	f.bind(firstInstallation, channel.TypeFeishu, "ou_first")
	secondInstallation := f.install(second, channel.TypeFeishu, "active")
	f.bind(secondInstallation, channel.TypeFeishu, "ou_second")

	target, ok, err := f.resolve(channel.TypeFeishu)
	if err != nil || !ok {
		t.Fatalf("resolve first subscriber: ok=%v err=%v", ok, err)
	}
	if target.AgentID != first {
		t.Fatalf("target agent = %v, want first subscriber %v", target.AgentID, first)
	}
}

func TestResolverRequiresBindingOnSelectedInstallation(t *testing.T) {
	f := newResolverDBFixture(t)
	participant := f.addAgent("Participant")
	nonParticipant := f.addAgent("Non Participant")
	f.assign(participant)
	f.install(participant, channel.TypeFeishu, "active")
	wrongInstallation := f.install(nonParticipant, channel.TypeFeishu, "active")
	f.bind(wrongInstallation, channel.TypeFeishu, "ou_wrong_bot")

	target, ok, err := f.resolve(channel.TypeFeishu)
	if err != nil {
		t.Fatalf("resolve wrong-installation case: %v", err)
	}
	if ok {
		t.Fatalf("unexpected target from non-participating installation: %+v", target)
	}
}

func TestResolverSkipsRevokedInstallationAndRemovedParticipant(t *testing.T) {
	f := newResolverDBFixture(t)
	revoked := f.addAgent("Revoked")
	archived := f.addAgent("Archived")
	f.assign(revoked)
	revokedInstallation := f.install(revoked, channel.TypeFeishu, "revoked")
	f.bind(revokedInstallation, channel.TypeFeishu, "ou_revoked")
	f.subscribe(archived, time.Now())
	archivedInstallation := f.install(archived, channel.TypeFeishu, "active")
	f.bind(archivedInstallation, channel.TypeFeishu, "ou_archived")
	if _, err := f.tx.Exec(f.ctx, "UPDATE agent SET archived_at = now() WHERE id = $1", archived); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	if target, ok, err := f.resolve(channel.TypeFeishu); err != nil || ok {
		t.Fatalf("revoked/archived candidates returned target=%+v ok=%v err=%v", target, ok, err)
	}

	if _, err := f.tx.Exec(f.ctx, "DELETE FROM member WHERE workspace_id = $1 AND user_id = $2", f.workspaceID, f.recipientID); err != nil {
		t.Fatalf("remove recipient member: %v", err)
	}
	if target, ok, err := f.resolve(channel.TypeFeishu); err != nil || ok {
		t.Fatalf("removed member returned target=%+v ok=%v err=%v", target, ok, err)
	}
}

func TestResolverSelectsIndependentlyPerChannelType(t *testing.T) {
	f := newResolverDBFixture(t)
	assignee := f.addAgent("Assignee")
	subscriber := f.addAgent("Subscriber")
	f.assign(assignee)
	f.subscribe(subscriber, time.Now())
	assigneeSlack := f.install(assignee, channel.Type("slack"), "active")
	f.bind(assigneeSlack, channel.Type("slack"), "U_assignee")
	subscriberFeishu := f.install(subscriber, channel.TypeFeishu, "active")
	f.bind(subscriberFeishu, channel.TypeFeishu, "ou_subscriber")

	feishuTarget, ok, err := f.resolve(channel.TypeFeishu)
	if err != nil || !ok || feishuTarget.AgentID != subscriber {
		t.Fatalf("feishu target=%+v ok=%v err=%v", feishuTarget, ok, err)
	}
	slackTarget, ok, err := f.resolve(channel.Type("slack"))
	if err != nil || !ok || slackTarget.AgentID != assignee {
		t.Fatalf("slack target=%+v ok=%v err=%v", slackTarget, ok, err)
	}
}
