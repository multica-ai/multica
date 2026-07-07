package dingtalk

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the DingTalk identity auto-binder: an unbound org member
// who messages the bot is resolved to their Multica account through the
// corp directory, without the reverse "click to bind" round trip.
//
// It works because both halves of the identity meet at the DingTalk
// unionid: DingTalk login (and members added via the DingTalk picker)
// records users under the synthetic email `<unionId>@dingtalk.com`
// (handler.dingTalkIdentityEmail), and the installation's own app can map
// the callback's senderStaffId to that same unionid via topapi/v2/user/get
// — internal apps of one corp share a unionid namespace. When the
// directory profile exposes a real email the binder also tries that, for
// users who signed up by email. A successful match writes the same
// channel_user_binding row the manual redeem flow writes, so the resolver
// hits the binding cache on every later message; when nothing matches,
// the "click to bind" prompt remains the fallback.

// autoBindQueries is the narrow DB surface the binder needs. *db.Queries
// satisfies it.
type autoBindQueries interface {
	GetUserByEmail(ctx context.Context, email string) (db.User, error)
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	CreateChannelUserBinding(ctx context.Context, arg db.CreateChannelUserBindingParams) (db.ChannelUserBinding, error)
}

// unionIDLookup is the directory surface the binder needs.
// *RobotMessenger satisfies it.
type unionIDLookup interface {
	LookupUserUnionID(ctx context.Context, creds channelCredentials, staffID string) (unionID, email string, err error)
}

// AutoBinder resolves an unbound sender through the corp directory and
// persists the binding on success.
type AutoBinder struct {
	q       autoBindQueries
	lookup  unionIDLookup
	decrypt Decrypter
	logger  *slog.Logger
}

// NewAutoBinder constructs the binder. All dependencies are required.
func NewAutoBinder(q autoBindQueries, lookup unionIDLookup, decrypt Decrypter, logger *slog.Logger) *AutoBinder {
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoBinder{q: q, lookup: lookup, decrypt: decrypt, logger: logger}
}

// Resolve maps the sender to a Multica user via the directory. It returns
// the engine sentinels on the product outcomes: ErrSenderUnbound when no
// account matches (or the directory is unreachable — the bind prompt is
// the graceful fallback), ErrSenderNotMember when the matched account is
// not a member of the installation's workspace.
func (b *AutoBinder) Resolve(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage) (engine.ResolvedIdentity, error) {
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || raw.SenderStaffID == "" {
		// No staff id — the sender is outside the app's org; the directory
		// cannot resolve them.
		return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
	}
	instRow, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
	}
	creds, err := decodeChannelCredentials(instRow.Config, b.decrypt)
	if err != nil {
		b.logger.WarnContext(ctx, "dingtalk auto-bind: decode credentials failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
		return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
	}
	unionID, contactEmail, err := b.lookup.LookupUserUnionID(ctx, creds, raw.SenderStaffID)
	if err != nil {
		// Directory misses (permission not granted, transient API failure)
		// degrade to the manual bind prompt, never to a hard error.
		b.logger.WarnContext(ctx, "dingtalk auto-bind: directory lookup failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
		return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
	}

	user, ok, err := b.findUser(ctx, unionID, contactEmail)
	if err != nil {
		return engine.ResolvedIdentity{}, err
	}
	if !ok {
		return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
	}

	if _, err := b.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      user.ID,
		WorkspaceID: inst.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedIdentity{}, engine.ErrSenderNotMember
		}
		return engine.ResolvedIdentity{}, err
	}

	if _, err := b.q.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
		WorkspaceID:    inst.WorkspaceID,
		MulticaUserID:  user.ID,
		InstallationID: inst.ID,
		ChannelType:    string(TypeDingtalk),
		ChannelUserID:  msg.Source.SenderID,
		Config:         []byte(`{"source":"directory_auto_bind"}`),
	}); err != nil {
		// pgx.ErrNoRows: this DingTalk user id is already bound to a
		// DIFFERENT Multica user (the ON CONFLICT gating rejected the
		// update) — fall back to the explicit bind flow rather than
		// silently re-assigning the identity.
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
		}
		return engine.ResolvedIdentity{}, err
	}

	b.logger.InfoContext(ctx, "dingtalk auto-bind: sender bound via directory",
		"installation_id", util.UUIDToString(inst.ID),
		"user_id", util.UUIDToString(user.ID))
	return engine.ResolvedIdentity{UserID: user.ID}, nil
}

// findUser tries the unionid synthetic email first (the canonical DingTalk
// identity), then the directory profile email for email-signup accounts.
func (b *AutoBinder) findUser(ctx context.Context, unionID, contactEmail string) (db.User, bool, error) {
	candidates := make([]string, 0, 2)
	if unionID != "" {
		candidates = append(candidates, unionID+"@dingtalk.com")
	}
	if e := strings.ToLower(strings.TrimSpace(contactEmail)); e != "" {
		candidates = append(candidates, e)
	}
	for _, email := range candidates {
		user, err := b.q.GetUserByEmail(ctx, email)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return db.User{}, false, err
		}
		return user, true, nil
	}
	return db.User{}, false, nil
}
