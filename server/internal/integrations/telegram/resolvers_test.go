package telegram

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

type fakeIdentityQ struct {
	binding db.ChannelUserBinding
	bindErr error
	memErr  error
}

func (f fakeIdentityQ) GetChannelUserBindingByUserID(ctx context.Context, a db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	return f.binding, f.bindErr
}
func (f fakeIdentityQ) GetMemberByUserAndWorkspace(ctx context.Context, a db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return db.Member{}, f.memErr
}

func inst() engine.ResolvedInstallation {
	return engine.ResolvedInstallation{ID: pgtype.UUID{Valid: true}, WorkspaceID: pgtype.UUID{Valid: true}}
}
func msgFrom(sender string) channel.InboundMessage {
	raw, _ := json.Marshal(telegramRawEvent{BotID: "123", EventType: "message"})
	return channel.InboundMessage{Source: channel.Source{ChannelType: TypeTelegram, SenderID: sender}, Raw: raw}
}

func TestIdentityResolver_Unbound(t *testing.T) {
	r := &identityResolver{q: fakeIdentityQ{bindErr: pgx.ErrNoRows}}
	_, err := r.ResolveSender(context.Background(), inst(), msgFrom("900"))
	if !errors.Is(err, engine.ErrSenderUnbound) {
		t.Fatalf("want ErrSenderUnbound, got %v", err)
	}
}

func TestIdentityResolver_BoundNonMember(t *testing.T) {
	r := &identityResolver{q: fakeIdentityQ{binding: db.ChannelUserBinding{MulticaUserID: pgtype.UUID{Valid: true}}, memErr: pgx.ErrNoRows}}
	_, err := r.ResolveSender(context.Background(), inst(), msgFrom("900"))
	if !errors.Is(err, engine.ErrSenderNotMember) {
		t.Fatalf("want ErrSenderNotMember, got %v", err)
	}
}

func TestIdentityResolver_BoundMember(t *testing.T) {
	uid := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	r := &identityResolver{q: fakeIdentityQ{binding: db.ChannelUserBinding{MulticaUserID: uid}}}
	got, err := r.ResolveSender(context.Background(), inst(), msgFrom("900"))
	if err != nil || got.UserID != uid {
		t.Fatalf("got (%v, %v)", got, err)
	}
}
