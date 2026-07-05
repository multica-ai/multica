package dingtalk

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeAutoBindQueries struct {
	usersByEmail map[string]db.User
	memberOf     map[[16]byte]bool // user id bytes -> is member
	created      []db.CreateChannelUserBindingParams
	createErr    error
}

func (f *fakeAutoBindQueries) GetUserByEmail(_ context.Context, email string) (db.User, error) {
	u, ok := f.usersByEmail[email]
	if !ok {
		return db.User{}, pgx.ErrNoRows
	}
	return u, nil
}

func (f *fakeAutoBindQueries) GetMemberByUserAndWorkspace(_ context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	if f.memberOf[arg.UserID.Bytes] {
		return db.Member{}, nil
	}
	return db.Member{}, pgx.ErrNoRows
}

func (f *fakeAutoBindQueries) CreateChannelUserBinding(_ context.Context, arg db.CreateChannelUserBindingParams) (db.ChannelUserBinding, error) {
	if f.createErr != nil {
		return db.ChannelUserBinding{}, f.createErr
	}
	f.created = append(f.created, arg)
	return db.ChannelUserBinding{MulticaUserID: arg.MulticaUserID}, nil
}

type fakeUnionIDLookup struct {
	unionID string
	email   string
	err     error
	calls   int
}

func (f *fakeUnionIDLookup) LookupUserUnionID(_ context.Context, _ channelCredentials, _ string) (string, string, error) {
	f.calls++
	return f.unionID, f.email, f.err
}

func autoBindInbound(t *testing.T, staffID string) channel.InboundMessage {
	t.Helper()
	msg, ok := inboundFromBotCallback(botCallbackData{
		ConversationID:   "cid_1",
		MsgID:            "m_1",
		SenderStaffID:    staffID,
		ConversationType: "1",
		Msgtype:          "text",
	}, "client_a")
	if !ok {
		t.Fatal("inboundFromBotCallback rejected the fixture")
	}
	return msg
}

func autoBindInstallation(t *testing.T) engine.ResolvedInstallation {
	t.Helper()
	instID := typingTestUUID(9)
	return engine.ResolvedInstallation{
		ID:          instID,
		WorkspaceID: typingTestUUID(8),
		Active:      true,
		Platform:    testInstallationRow(t, instID, "client_a"),
	}
}

func TestAutoBinderBindsDirectoryMatch(t *testing.T) {
	user := db.User{ID: typingTestUUID(3), Email: "union_1@dingtalk.com"}
	q := &fakeAutoBindQueries{
		usersByEmail: map[string]db.User{"union_1@dingtalk.com": user},
		memberOf:     map[[16]byte]bool{user.ID.Bytes: true},
	}
	lookup := &fakeUnionIDLookup{unionID: "union_1"}
	binder := NewAutoBinder(q, lookup, plaintextDecrypter, nil)

	identity, err := binder.Resolve(context.Background(), autoBindInstallation(t), autoBindInbound(t, "staff_1"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.UserID != user.ID {
		t.Fatalf("bound wrong user: %v", identity.UserID)
	}
	if len(q.created) != 1 {
		t.Fatalf("expected 1 binding row, got %d", len(q.created))
	}
	created := q.created[0]
	if created.ChannelUserID != "staff_1" || created.ChannelType != string(TypeDingtalk) {
		t.Fatalf("unexpected binding params: %+v", created)
	}
}

func TestAutoBinderFallsBackToContactEmail(t *testing.T) {
	user := db.User{ID: typingTestUUID(4), Email: "dev@example.com"}
	q := &fakeAutoBindQueries{
		usersByEmail: map[string]db.User{"dev@example.com": user},
		memberOf:     map[[16]byte]bool{user.ID.Bytes: true},
	}
	lookup := &fakeUnionIDLookup{unionID: "union_nomatch", email: "Dev@Example.com"}
	binder := NewAutoBinder(q, lookup, plaintextDecrypter, nil)

	identity, err := binder.Resolve(context.Background(), autoBindInstallation(t), autoBindInbound(t, "staff_1"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if identity.UserID != user.ID {
		t.Fatalf("bound wrong user: %v", identity.UserID)
	}
}

func TestAutoBinderNonMemberIsNotBound(t *testing.T) {
	user := db.User{ID: typingTestUUID(5), Email: "union_1@dingtalk.com"}
	q := &fakeAutoBindQueries{
		usersByEmail: map[string]db.User{"union_1@dingtalk.com": user},
		memberOf:     map[[16]byte]bool{},
	}
	binder := NewAutoBinder(q, &fakeUnionIDLookup{unionID: "union_1"}, plaintextDecrypter, nil)

	_, err := binder.Resolve(context.Background(), autoBindInstallation(t), autoBindInbound(t, "staff_1"))
	if !errors.Is(err, engine.ErrSenderNotMember) {
		t.Fatalf("expected ErrSenderNotMember, got %v", err)
	}
	if len(q.created) != 0 {
		t.Fatalf("non-member must not be bound")
	}
}

func TestAutoBinderUnknownUserFallsBackToPrompt(t *testing.T) {
	binder := NewAutoBinder(&fakeAutoBindQueries{usersByEmail: map[string]db.User{}}, &fakeUnionIDLookup{unionID: "union_x"}, plaintextDecrypter, nil)

	_, err := binder.Resolve(context.Background(), autoBindInstallation(t), autoBindInbound(t, "staff_1"))
	if !errors.Is(err, engine.ErrSenderUnbound) {
		t.Fatalf("expected ErrSenderUnbound, got %v", err)
	}
}

func TestAutoBinderDirectoryFailureFallsBackToPrompt(t *testing.T) {
	binder := NewAutoBinder(&fakeAutoBindQueries{}, &fakeUnionIDLookup{err: errors.New("no permission")}, plaintextDecrypter, nil)

	_, err := binder.Resolve(context.Background(), autoBindInstallation(t), autoBindInbound(t, "staff_1"))
	if !errors.Is(err, engine.ErrSenderUnbound) {
		t.Fatalf("expected ErrSenderUnbound, got %v", err)
	}
}

func TestAutoBinderSkipsSenderWithoutStaffID(t *testing.T) {
	lookup := &fakeUnionIDLookup{unionID: "union_1"}
	binder := NewAutoBinder(&fakeAutoBindQueries{}, lookup, plaintextDecrypter, nil)

	// An out-of-org sender only carries the encrypted senderId.
	msg, ok := inboundFromBotCallback(botCallbackData{
		ConversationID:   "cid_1",
		MsgID:            "m_1",
		SenderID:         "$:LWCP_v1:$abc",
		ConversationType: "1",
		Msgtype:          "text",
	}, "client_a")
	if !ok {
		t.Fatal("inboundFromBotCallback rejected the fixture")
	}

	_, err := binder.Resolve(context.Background(), autoBindInstallation(t), msg)
	if !errors.Is(err, engine.ErrSenderUnbound) {
		t.Fatalf("expected ErrSenderUnbound, got %v", err)
	}
	if lookup.calls != 0 {
		t.Fatalf("directory must not be queried without a staff id")
	}
}

func TestAutoBinderConflictingBindingFallsBackToPrompt(t *testing.T) {
	user := db.User{ID: typingTestUUID(6), Email: "union_1@dingtalk.com"}
	q := &fakeAutoBindQueries{
		usersByEmail: map[string]db.User{"union_1@dingtalk.com": user},
		memberOf:     map[[16]byte]bool{user.ID.Bytes: true},
		createErr:    pgx.ErrNoRows, // ON CONFLICT gating: bound to a different user
	}
	binder := NewAutoBinder(q, &fakeUnionIDLookup{unionID: "union_1"}, plaintextDecrypter, nil)

	_, err := binder.Resolve(context.Background(), autoBindInstallation(t), autoBindInbound(t, "staff_1"))
	if !errors.Is(err, engine.ErrSenderUnbound) {
		t.Fatalf("expected ErrSenderUnbound, got %v", err)
	}
}

// fakeIdentityQueries drives identityResolver directly.
type fakeIdentityQueries struct {
	binding    *db.ChannelUserBinding
	memberOK   bool
	bindingErr error
}

func (f *fakeIdentityQueries) GetChannelUserBindingByUserID(_ context.Context, _ db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	if f.binding != nil {
		return *f.binding, nil
	}
	if f.bindingErr != nil {
		return db.ChannelUserBinding{}, f.bindingErr
	}
	return db.ChannelUserBinding{}, pgx.ErrNoRows
}

func (f *fakeIdentityQueries) GetMemberByUserAndWorkspace(_ context.Context, _ db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	if f.memberOK {
		return db.Member{}, nil
	}
	return db.Member{}, pgx.ErrNoRows
}

func TestIdentityResolverUsesAutoBinderWhenUnbound(t *testing.T) {
	user := db.User{ID: typingTestUUID(7), Email: "union_1@dingtalk.com"}
	binder := NewAutoBinder(&fakeAutoBindQueries{
		usersByEmail: map[string]db.User{"union_1@dingtalk.com": user},
		memberOf:     map[[16]byte]bool{user.ID.Bytes: true},
	}, &fakeUnionIDLookup{unionID: "union_1"}, plaintextDecrypter, nil)

	r := &identityResolver{q: &fakeIdentityQueries{}, auto: binder}
	identity, err := r.ResolveSender(context.Background(), autoBindInstallation(t), autoBindInbound(t, "staff_1"))
	if err != nil {
		t.Fatalf("ResolveSender: %v", err)
	}
	if identity.UserID != user.ID {
		t.Fatalf("resolved wrong user: %v", identity.UserID)
	}
}

func TestIdentityResolverWithoutAutoBinderStaysUnbound(t *testing.T) {
	r := &identityResolver{q: &fakeIdentityQueries{}}
	_, err := r.ResolveSender(context.Background(), autoBindInstallation(t), autoBindInbound(t, "staff_1"))
	if !errors.Is(err, engine.ErrSenderUnbound) {
		t.Fatalf("expected ErrSenderUnbound, got %v", err)
	}
}
