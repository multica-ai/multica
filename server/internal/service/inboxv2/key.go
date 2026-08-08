package inboxv2

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// keyVersion prefixes every delivery key.
//
// Versioned because the composition below is a contract with the rows already
// in the table: changing what goes into the hash changes every key, and without
// a prefix the new keys would silently coexist with the old ones under the same
// uniqueness rule. Bumping the prefix makes a composition change a visible
// re-keying rather than a quiet loss of idempotency.
const keyVersion = "v1"

// keySeparator cannot appear in a UUID, an enum or a type name, so no
// combination of parts can be reinterpreted as a different combination.
// Concatenating with ":" would let ("a:b", "c") and ("a", "b:c") collide.
const keySeparator = "\x1f"

// DeliveryKey builds the idempotency key for one notification.
//
// Two properties matter, and they pull in opposite directions:
//
//   - A retry of the SAME logical event must produce the SAME key, or the user
//     gets the notification twice. So the key is derived from the originating
//     entity, never from a row id, a timestamp or a random value.
//   - Two DIFFERENT events must produce different keys, or the second one is
//     silently dropped. So the anchor has to be specific enough to tell two
//     events on the same issue apart — a comment id, or the content of the
//     transition when there is no comment.
//
// workspace and recipient are folded in even though inbox_item_delivery_key_uidx
// is already scoped to them. The index is the thing that enforces tenancy; this
// is so a key that escapes into a log or a test fixture cannot be interpreted
// as addressing anyone else's notification either.
func DeliveryKey(workspaceID, recipientID, notifType string, anchor ...string) pgtype.Text {
	parts := make([]string, 0, len(anchor)+3)
	parts = append(parts, workspaceID, recipientID, notifType)
	parts = append(parts, anchor...)
	sum := sha256.Sum256([]byte(strings.Join(parts, keySeparator)))
	return pgtype.Text{String: keyVersion + ":" + hex.EncodeToString(sum[:]), Valid: true}
}

// StringDetails coerces a details map to the all-strings shape the mobile
// client's schema requires.
//
// apps/mobile parses `details` as z.record(z.string(), z.string()) through
// parseWithFallback, which validates the WHOLE list at once: a single numeric
// value anywhere in the response makes the entire array fail to parse and the
// user's inbox renders empty. That is a live failure, not a hypothetical —
// the autopilot failure monitor writes counters into details today.
//
// Producers go through this so the constraint is enforced in one place rather
// than remembered at seven call sites.
func StringDetails(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}

// NewStandaloneSource mints the source id for a notification with no durable
// parent.
//
// Standalone sources are opaque: nothing ever looks one up by identity, because
// there is no entity to look up. What matters is that each such notification
// gets its OWN id, so an autopilot pause and an unrelated quick-create failure
// do not end up sharing one read cursor and one archive state.
//
// (The lazy migration keys historical standalone rows on the row's own id
// instead — same property, and the only stable identity a legacy row has.)
func NewStandaloneSource() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.New(), Valid: true}
}
