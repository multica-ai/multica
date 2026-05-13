// Package persona owns the cerebro-side helpers for the firtal-persona
// integration. JEH-1080: at task-claim time the daemon needs to know which
// human user the agent is acting on behalf of, so the persona-hook can
// carry that user's group memberships as facts in /v1/check args.
//
// Helpers in this file resolve the (user_id, group_ids) tuple from an
// already-loaded issue row. They live outside server/internal/handler so
// the dependency direction stays one-way: handlers may call this package,
// nothing here imports handler.
package persona

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SpawnSubject is the persona-side identity for one task spawn. UserID is the
// human user the agent is acting on behalf of; GroupIDs is that user's group
// memberships at claim time, opaque UUID strings persona stores as fact
// tokens.
//
// Empty UserID and nil GroupIDs are the "no spawning user" case — agent-only
// tasks (autopilot system-runs, chat sessions without a member). The daemon
// treats this as "no group grants apply" rather than an error.
type SpawnSubject struct {
	UserID   string
	GroupIDs []string
}

// IssueSpawningUser picks the spawning user for an issue task.
//
// Resolution order:
//
//  1. issue.assignee_id when assignee_type == "member"
//  2. issue.creator_id when creator_type == "member"
//
// An agent-assigned issue with an agent creator returns ("", false) — no
// human is meaningfully the spawner, and persona group grants therefore
// don't apply. Callers should treat this as "skip group resolution".
//
// Pure function, no I/O — kept testable in isolation.
func IssueSpawningUser(issue db.Issue) (pgtype.UUID, bool) {
	if issue.AssigneeID.Valid && issue.AssigneeType.Valid && issue.AssigneeType.String == "member" {
		return issue.AssigneeID, true
	}
	if issue.CreatorID.Valid && issue.CreatorType == "member" {
		return issue.CreatorID, true
	}
	return pgtype.UUID{}, false
}

// ResolveSpawnSubject loads (UserID, GroupIDs) for a spawn-time persona
// context. Returns a zero-value SpawnSubject when no human user is
// resolvable for the issue, or when group resolution fails — callers must
// treat a degraded SpawnSubject as "no group grants", never as an error
// that should fail the claim.
//
// The fail-soft contract matches the JEH-1080 acceptance criterion that
// "fjern medlem fra G → mister adgangen næste gang permission-check kører":
// if cerebro cannot pin a user to a spawn, every permission check on that
// run falls through to deny via the missing fact, which is the safe default.
//
// gp may be nil — upstream-only fixtures plus the test path leave the
// group seam unwired. In that case GroupIDs stays empty and the spawn
// proceeds without group facts.
func ResolveSpawnSubject(ctx context.Context, gp GroupResolver, issue db.Issue) SpawnSubject {
	userUUID, ok := IssueSpawningUser(issue)
	if !ok {
		return SpawnSubject{}
	}
	sub := SpawnSubject{UserID: uuidString(userUUID)}
	if gp == nil {
		return sub
	}
	ids, err := gp.ResolveGroupIDs(ctx, issue.WorkspaceID, userUUID)
	if err != nil {
		// Failing to resolve groups must not fail the claim. The agent
		// still runs; the persona hook just won't carry group facts and
		// any group-grants will fall through to deny — fail-safe.
		return sub
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id.Valid {
			out = append(out, uuidString(id))
		}
	}
	sub.GroupIDs = out
	return sub
}

// GroupResolver is the minimal surface ResolveSpawnSubject needs. The
// handler-side GroupPermissionsInvoker and the cerebro-side
// grouppermissions.Service both satisfy it — kept as an interface here so
// this package does not import either and the dependency direction stays
// one-way (handler imports this; nothing here imports handler).
type GroupResolver interface {
	ResolveGroupIDs(ctx context.Context, workspaceID, userID pgtype.UUID) ([]pgtype.UUID, error)
}

func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	// pgtype.UUID's String() returns the canonical 8-4-4-4-12 hex form.
	// We rely on the same canonicalisation persona uses (lowercase, dashes).
	const hexDigits = "0123456789abcdef"
	var out [36]byte
	pos := 0
	for i, b := range u.Bytes {
		out[pos] = hexDigits[b>>4]
		out[pos+1] = hexDigits[b&0x0f]
		pos += 2
		if i == 3 || i == 5 || i == 7 || i == 9 {
			out[pos] = '-'
			pos++
		}
	}
	return string(out[:])
}
