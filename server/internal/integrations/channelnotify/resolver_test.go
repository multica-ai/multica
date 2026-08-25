package channelnotify

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type resolverQueryStub struct {
	row db.FindIssueChannelNotificationTargetRow
	err error
}

func (s resolverQueryStub) FindIssueChannelNotificationTarget(context.Context, db.FindIssueChannelNotificationTargetParams) (db.FindIssueChannelNotificationTargetRow, error) {
	return s.row, s.err
}

func testUUID(seed byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{seed}, Valid: true}
}

func TestResolverMapsIssueChannelNotificationTarget(t *testing.T) {
	row := db.FindIssueChannelNotificationTargetRow{
		InstallationID: testUUID(1),
		AgentID:        testUUID(2),
		ChannelType:    "feishu",
		ChannelUserID:  "ou_recipient",
		WorkspaceSlug:  "acme",
	}
	resolver := NewResolver(resolverQueryStub{row: row})

	target, ok, err := resolver.Resolve(context.Background(), Notification{
		WorkspaceID: testUUID(3),
		RecipientID: testUUID(4),
		IssueID:     testUUID(5),
	}, channel.TypeFeishu)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !ok {
		t.Fatal("Resolve returned no target")
	}
	if target.InstallationID != row.InstallationID || target.AgentID != row.AgentID ||
		target.ChannelType != channel.Type(row.ChannelType) || target.ChannelUserID != row.ChannelUserID ||
		target.WorkspaceSlug != row.WorkspaceSlug {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestResolverTreatsMissingTargetAsSilent(t *testing.T) {
	resolver := NewResolver(resolverQueryStub{err: pgx.ErrNoRows})

	target, ok, err := resolver.Resolve(context.Background(), Notification{
		WorkspaceID: testUUID(1),
		RecipientID: testUUID(2),
		IssueID:     testUUID(3),
	}, channel.TypeFeishu)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected no target, got %+v", target)
	}
}

func TestResolverPropagatesUnexpectedQueryError(t *testing.T) {
	want := errors.New("database unavailable")
	resolver := NewResolver(resolverQueryStub{err: want})

	_, ok, err := resolver.Resolve(context.Background(), Notification{
		WorkspaceID: testUUID(1),
		RecipientID: testUUID(2),
		IssueID:     testUUID(3),
	}, channel.TypeFeishu)
	if !errors.Is(err, want) {
		t.Fatalf("Resolve error = %v, want %v", err, want)
	}
	if ok {
		t.Fatal("unexpected target on query error")
	}
}
