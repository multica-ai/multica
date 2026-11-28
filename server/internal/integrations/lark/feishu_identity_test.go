package lark

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeIdentityStore struct {
	binding    *UserBinding
	members    map[pgtype.UUID]bool
	bindingErr error
}

func (f *fakeIdentityStore) GetLarkUserBindingByOpenID(context.Context, GetUserBindingByOpenIDParams) (UserBinding, error) {
	if f.bindingErr != nil {
		return UserBinding{}, f.bindingErr
	}
	if f.binding == nil {
		return UserBinding{}, pgx.ErrNoRows
	}
	return *f.binding, nil
}

func (f *fakeIdentityStore) IsWorkspaceMember(_ context.Context, _, userID pgtype.UUID) (bool, error) {
	return f.members[userID], nil
}

func TestFeishuIdentityResolver_GroupSenderPolicy(t *testing.T) {
	installer := binderUUID(0x10)
	sender := binderUUID(0x20)
	bound := &UserBinding{MulticaUserID: sender}
	on := func(context.Context) bool { return true }
	off := func(context.Context) bool { return false }

	cases := []struct {
		name     string
		binding  *UserBinding
		members  map[pgtype.UUID]bool
		policy   GroupSenderPolicy
		chatType channel.ChatType
		flag     func(context.Context) bool
		wantUser pgtype.UUID
		wantErr  error
	}{
		{name: "bound member is served as themselves even when open", binding: bound, members: map[pgtype.UUID]bool{sender: true, installer: true}, policy: GroupSendersAnyone, chatType: channel.ChatTypeGroup, flag: on, wantUser: sender},
		{name: "unbound group sender runs as installer", members: map[pgtype.UUID]bool{installer: true}, policy: GroupSendersAnyone, chatType: channel.ChatTypeGroup, flag: on, wantUser: installer},
		{name: "bound non-member group sender runs as installer", binding: bound, members: map[pgtype.UUID]bool{installer: true}, policy: GroupSendersAnyone, chatType: channel.ChatTypeGroup, flag: on, wantUser: installer},
		{name: "members only keeps the binding prompt", members: map[pgtype.UUID]bool{installer: true}, policy: GroupSendersMembersOnly, chatType: channel.ChatTypeGroup, flag: on, wantErr: engine.ErrSenderUnbound},
		{name: "flag off ignores the stored policy", members: map[pgtype.UUID]bool{installer: true}, policy: GroupSendersAnyone, chatType: channel.ChatTypeGroup, flag: off, wantErr: engine.ErrSenderUnbound},
		{name: "nil flag ignores the stored policy", binding: bound, members: map[pgtype.UUID]bool{installer: true}, policy: GroupSendersAnyone, chatType: channel.ChatTypeGroup, wantErr: engine.ErrSenderNotMember},
		{name: "p2p is never opened", members: map[pgtype.UUID]bool{installer: true}, policy: GroupSendersAnyone, chatType: channel.ChatTypeP2P, flag: on, wantErr: engine.ErrSenderUnbound},
		{name: "installer who left cannot vouch", binding: bound, members: map[pgtype.UUID]bool{}, policy: GroupSendersAnyone, chatType: channel.ChatTypeGroup, flag: on, wantErr: engine.ErrSenderNotMember},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &feishuIdentityResolver{
				store:           &fakeIdentityStore{binding: tc.binding, members: tc.members},
				openGroupAccess: tc.flag,
			}
			inst := engine.ResolvedInstallation{
				ID: binderUUID(1), WorkspaceID: binderUUID(2), InstallerUserID: installer,
				Platform: Installation{GroupSenderPolicy: tc.policy},
			}
			msg := channel.InboundMessage{Source: channel.Source{SenderID: "ou_sender", ChatType: tc.chatType}}
			got, err := r.ResolveSender(context.Background(), inst, msg)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveSender: %v", err)
			}
			if got.UserID != tc.wantUser {
				t.Fatalf("UserID = %v, want %v", got.UserID, tc.wantUser)
			}
		})
	}
}

func TestFeishuIdentityResolver_LookupFailureIsNotARefusal(t *testing.T) {
	boom := errors.New("db down")
	r := &feishuIdentityResolver{
		store:           &fakeIdentityStore{bindingErr: boom},
		openGroupAccess: func(context.Context) bool { return true },
	}
	inst := engine.ResolvedInstallation{InstallerUserID: binderUUID(0x10), Platform: Installation{GroupSenderPolicy: GroupSendersAnyone}}
	msg := channel.InboundMessage{Source: channel.Source{ChatType: channel.ChatTypeGroup}}
	if _, err := r.ResolveSender(context.Background(), inst, msg); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the lookup failure", err)
	}
}

func TestInstallationFromRow_GroupSenderPolicy(t *testing.T) {
	for raw, want := range map[string]GroupSenderPolicy{
		`{"app_id":"a"}`: GroupSendersMembersOnly,
		`{"app_id":"a","group_sender_policy":"anyone_in_chat"}`: GroupSendersAnyone,
		`{"app_id":"a","group_sender_policy":"everyone"}`:       GroupSendersMembersOnly,
	} {
		got, err := installationFromRow(db.ChannelInstallation{Config: []byte(raw)})
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if got.GroupSenderPolicy != want {
			t.Fatalf("%s: policy = %q, want %q", raw, got.GroupSenderPolicy, want)
		}
	}
}
