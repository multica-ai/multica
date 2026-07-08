package slack

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeIdentityQueries implements identityQueries so the cross-installation
// account-link reuse path (MUL-3911) is exercised without a database.
type fakeIdentityQueries struct {
	binding     db.ChannelUserBinding
	bindErr     error
	reusable    db.ChannelUserBinding
	reusableErr error
	memberErr   error
	createErr   error

	findCalls   int
	findWith    db.FindReusableChannelUserBindingParams
	createCalls int
	createWith  db.CreateChannelUserBindingParams
}

func (f *fakeIdentityQueries) GetChannelUserBindingByUserID(_ context.Context, _ db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	return f.binding, f.bindErr
}

func (f *fakeIdentityQueries) FindReusableChannelUserBinding(_ context.Context, arg db.FindReusableChannelUserBindingParams) (db.ChannelUserBinding, error) {
	f.findCalls++
	f.findWith = arg
	return f.reusable, f.reusableErr
}

func (f *fakeIdentityQueries) GetMemberByUserAndWorkspace(_ context.Context, _ db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return db.Member{}, f.memberErr
}

func (f *fakeIdentityQueries) CreateChannelUserBinding(_ context.Context, arg db.CreateChannelUserBindingParams) (db.ChannelUserBinding, error) {
	f.createCalls++
	f.createWith = arg
	return db.ChannelUserBinding{}, f.createErr
}

// TestResolveSenderReuse covers the identity resolver's decision to reuse an
// existing account link across installations of the same Slack team + Multica
// workspace, instead of re-prompting the user for every new Slack app.
func TestResolveSenderReuse(t *testing.T) {
	const senderID = "U123"
	wsID := slashTestUUID(0x11)
	instB := slashTestUUID(0xBB) // the installation the message arrives on
	instA := slashTestUUID(0xAA) // the installation the user already linked
	userID := slashTestUUID(0x77)

	// inst builds the ResolvedInstallation the message routes to; teamID is what
	// its stored config carries (empty = a legacy install with no recorded team).
	inst := func(teamID string) engine.ResolvedInstallation {
		cfg, _ := json.Marshal(installConfig{AppID: "A_APPB", TeamID: teamID})
		return engine.ResolvedInstallation{
			ID:          instB,
			WorkspaceID: wsID,
			Platform:    db.ChannelInstallation{ID: instB, WorkspaceID: wsID, Config: cfg},
		}
	}
	msg := inbound(channel.ChatTypeP2P, "D1", "", "1.0")
	msg.Source.SenderID = senderID

	t.Run("direct binding resolves without a reuse lookup or write", func(t *testing.T) {
		f := &fakeIdentityQueries{binding: db.ChannelUserBinding{MulticaUserID: userID}}
		got, err := (&identityResolver{q: f}).ResolveSender(context.Background(), inst("T1"), msg)
		if err != nil {
			t.Fatalf("ResolveSender err = %v", err)
		}
		if got.UserID != userID {
			t.Errorf("UserID = %v, want %v", got.UserID, userID)
		}
		if f.findCalls != 0 || f.createCalls != 0 {
			t.Errorf("directly-bound sender must not trigger reuse (find=%d create=%d)", f.findCalls, f.createCalls)
		}
	})

	t.Run("unlinked sender reuses a same-team link and materializes it", func(t *testing.T) {
		f := &fakeIdentityQueries{
			bindErr:  pgx.ErrNoRows,
			reusable: db.ChannelUserBinding{MulticaUserID: userID, InstallationID: instA},
		}
		got, err := (&identityResolver{q: f}).ResolveSender(context.Background(), inst("T1"), msg)
		if err != nil {
			t.Fatalf("ResolveSender err = %v", err)
		}
		if got.UserID != userID {
			t.Errorf("UserID = %v, want reused %v", got.UserID, userID)
		}
		if f.findCalls != 1 {
			t.Fatalf("reuse lookup must run exactly once, ran %d", f.findCalls)
		}
		if f.findWith.TeamID != "T1" || f.findWith.ChannelUserID != senderID || f.findWith.ChannelType != string(TypeSlack) || f.findWith.WorkspaceID != wsID {
			t.Errorf("reuse lookup args = %+v", f.findWith)
		}
		if f.createCalls != 1 {
			t.Fatalf("reused link must be materialized on THIS installation, create ran %d", f.createCalls)
		}
		if f.createWith.InstallationID != instB || f.createWith.MulticaUserID != userID || f.createWith.ChannelUserID != senderID {
			t.Errorf("materialized binding args = %+v (want install=%v user=%v sender=%q)", f.createWith, instB, userID, senderID)
		}
	})

	t.Run("no direct binding and nothing to reuse prompts a link", func(t *testing.T) {
		f := &fakeIdentityQueries{bindErr: pgx.ErrNoRows, reusableErr: pgx.ErrNoRows}
		_, err := (&identityResolver{q: f}).ResolveSender(context.Background(), inst("T1"), msg)
		if !errors.Is(err, engine.ErrSenderUnbound) {
			t.Fatalf("err = %v, want ErrSenderUnbound", err)
		}
		if f.createCalls != 0 {
			t.Errorf("nothing to reuse must not write a binding")
		}
	})

	t.Run("reusable link whose user left the workspace prompts a fresh link", func(t *testing.T) {
		f := &fakeIdentityQueries{
			bindErr:   pgx.ErrNoRows,
			reusable:  db.ChannelUserBinding{MulticaUserID: userID, InstallationID: instA},
			memberErr: pgx.ErrNoRows,
		}
		_, err := (&identityResolver{q: f}).ResolveSender(context.Background(), inst("T1"), msg)
		if !errors.Is(err, engine.ErrSenderUnbound) {
			t.Fatalf("err = %v, want ErrSenderUnbound (fresh link, not not-member)", err)
		}
		if f.createCalls != 0 {
			t.Errorf("must not materialize a binding for a non-member")
		}
	})

	t.Run("legacy installation with no team never attempts reuse", func(t *testing.T) {
		f := &fakeIdentityQueries{bindErr: pgx.ErrNoRows}
		_, err := (&identityResolver{q: f}).ResolveSender(context.Background(), inst(""), msg)
		if !errors.Is(err, engine.ErrSenderUnbound) {
			t.Fatalf("err = %v, want ErrSenderUnbound", err)
		}
		if f.findCalls != 0 {
			t.Errorf("an install with no recorded team must not attempt cross-app reuse")
		}
	})

	t.Run("directly-bound non-member surfaces not-member", func(t *testing.T) {
		f := &fakeIdentityQueries{binding: db.ChannelUserBinding{MulticaUserID: userID}, memberErr: pgx.ErrNoRows}
		_, err := (&identityResolver{q: f}).ResolveSender(context.Background(), inst("T1"), msg)
		if !errors.Is(err, engine.ErrSenderNotMember) {
			t.Fatalf("err = %v, want ErrSenderNotMember", err)
		}
	})
}
