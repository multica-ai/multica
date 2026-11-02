package wecom

// language.go — which language the bot's own copy speaks, per DESTINATION.
//
// The agent's answer is never touched: it passes through in whatever language
// the model chose. What needs a decision is the adapter's own voice — the
// receipts, prompts and cards in strings.go.
//
// A destination is a person or a room, and only a person has a language of
// their own. Multica already carries a validated language on every user profile
// (the web UI's own setting), so a 1:1 goes to that. A group has many readers,
// no shared profile, and no member list WeCom hands us — so it goes to the
// deployment's language. Picking one member's personal setting to address a
// room is the same mistake whichever member you pick.
//
// There is no per-message language detection: a profile beats a heuristic, and
// the profile is the one signal the reader themselves controls.
//
// The lookup is two indexed reads (binding, then user) and every caller treats
// a miss as the deployment default rather than an error: a receipt in the wrong
// language still says something useful, and dropping it over a language choice
// would be worse than sending it in either one.

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// languageLookup resolves the person behind a message to their profile
// language. *db.Queries satisfies it.
type languageLookup interface {
	GetChannelUserBindingByUserID(ctx context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error)
	GetUser(ctx context.Context, id pgtype.UUID) (db.User, error)
}

// localeFor is the only locale question the rest of the package may ask: what
// language does THIS DESTINATION read?
//
// chatType says whether the destination is a person (chatTypeSingleInt) or a
// room. personID identifies the person when it is one — the sender's
// bot-scoped userid, or equivalently the 1:1 chatid, which IS that userid.
// It is ignored for a room.
//
// localeForSender below answers a different question — what does this PERSON
// read — and calling it for a room is exactly the mistake this exists to
// prevent. It stays private to this file.
func localeFor(ctx context.Context, q languageLookup, installationID pgtype.UUID, chatType int, personID string) Locale {
	if chatType != chatTypeSingleInt {
		return deploymentLocale()
	}
	return localeForSender(ctx, q, installationID, personID)
}

// localeForUser reads a Multica user's profile language. Anything missing —
// nil lookup, no row, an empty field — is the deployment default. Used where
// the reader is a named Multica user rather than a chat: the inbox push, which
// is addressed to one person by their binding row.
func localeForUser(ctx context.Context, q languageLookup, userID pgtype.UUID) Locale {
	if q == nil || !userID.Valid {
		return deploymentLocale()
	}
	user, err := q.GetUser(ctx, userID)
	if err != nil {
		return deploymentLocale()
	}
	return resolveLocale(strings.TrimSpace(user.Language.String))
}

// localeForSender resolves the copy language for the WeCom user behind
// senderID on this installation: their profile language when they are bound,
// the deployment default when they are not (or when nothing can be read). The
// binding row is the same one the identity resolver reads a moment earlier; the
// Replier seam does not carry that answer across, so this reads it again — one
// indexed row, off the ACK path.
//
// Private on purpose. Reach it through localeFor, which knows whether the
// destination is one person at all.
func localeForSender(ctx context.Context, q languageLookup, installationID pgtype.UUID, senderID string) Locale {
	senderID = strings.TrimSpace(senderID)
	if q == nil || !installationID.Valid || senderID == "" {
		return deploymentLocale()
	}
	binding, err := q.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: installationID,
		ChannelUserID:  senderID,
	})
	if err != nil {
		return deploymentLocale()
	}
	return localeForUser(ctx, q, binding.MulticaUserID)
}
