# IM 审核推送与决策回执 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When an inbox notification says "a human is needed now", push it as an IM direct message, and let a reply to that DM record the human decision and wake the agent.

**Architecture:** A new shared package `internal/integrations/channel/notify` subscribes to `protocol.EventInboxNew`, filters through a hardcoded whitelist, looks up the recipient's channel binding, and asks a per-platform adapter to `DeliverDM`. When the adapter returns a platform message id, the push is recorded in a new `channel_push_message` ledger. On the way back, `engine.Router` checks an inbound message's `ReplyTo` against that ledger *before* chat-session resolution; a hit is injected as an issue comment through a new narrow interface implemented by `*handler.Handler`, and the existing comment triggers wake the agent. Exact `审核通过` / `确认审核` replies additionally move an `in_review`-category issue to `done` as the replying member; contextual replies remain comment-only.

**Tech Stack:** Go 1.26, Chi, sqlc (pgx/v5, `pgtype`), `internal/events.Bus`, `internal/testutil` (`dbfx`, `testutil.Call`), PostgreSQL 17.

**Spec:** `docs/superpowers/specs/2026-09-03-im-review-push-design.md`

## Global Constraints

- **No database foreign keys.** No `FOREIGN KEY` / `REFERENCES`, no cascading deletes or updates. Resolve relationships in application code.
- **Every migration index uses `CREATE [UNIQUE] INDEX CONCURRENTLY`, alone in its own single-statement migration file.** A table-creation migration and its index migration are two separate numbered pairs.
- **Every `CREATE INDEX CONCURRENTLY` up-migration MUST get an entry in `concurrentIndexCleanups` in `server/cmd/migrate/main.go`.** `TestEveryConcurrentUpBuildHasCleanup` fails the build otherwise. `concurrentDownIndexCleanups` (`main.go:308`) is **not** its mirror: it registers migrations whose *down* direction **builds** an index concurrently (every entry is a `*_drop_*` migration). A down that merely drops an index gets no entry — adding one fails `TestConcurrentIndexCleanupsMatchTheirMigrations`.
- **Migrations are numbered `NNN_name.up.sql` + `NNN_name.down.sql` pairs.** Always write the down file. Highest existing number is **449**; this plan uses **450** and **451**.
- **`server/pkg/db/generated/` is sqlc output — never edit it.** Change `server/pkg/db/queries/*.sql` and run `make sqlc` from the repo root.
- **Code comments must be English.** (Product copy sent to users is Chinese — see the copy constants in Task 7.)
- **No compatibility layers, dual writes, or shims for internal non-boundary code.** When a path is replaced, delete the old one.
- **All queries filter by `workspace_id`.**
- **Go conventions:** `gofmt`, `go vet`, checked errors.
- **DB-backed Go tests use `internal/testutil`** (`dbfx.Issue`, `dbfx.Insert`, `testutil.Call`). Do not open-code `INSERT ... RETURNING id` + `t.Cleanup(DELETE ...)` pairs, or `httptest.NewRecorder()` + status-check + decode quartets.
- **Run backend tests with `make test`** (from repo root), not bare `go test ./...` — `make test` ensures the DB, applies migrations, and applies the agent-CLI guard. For a single package while iterating: `(cd server && go test ./internal/... -run TestX -count=1)`.
- **Adapter reality (do not design against the spec's older assumption):** WeCom can push but can **never** be replied to — `sendTextCtx` discards the send ack (`ws_sender.go:297-304`) and `aibotMsgCallback` carries no reply field (`ws_frame.go:74-94`). **Lark** is the only adapter that closes the full loop and is where the end-to-end test lives.

---

## File Structure

**New package — `server/internal/integrations/channel/notify/`** (decision only; never calls `channel.Channel.Send`):

| File | Responsibility |
| --- | --- |
| `decision.go` | The hardcoded whitelist. Pure: `(notifType, effectiveStatus) -> Decision`. |
| `decision_test.go` | Table-driven whitelist matrix. No DB, no bus. |
| `deliver.go` | `DMDeliverer` interface, `DeliverResult` three-state type, `Registry`. |
| `notifier.go` | `Notifier`: bus subscription, payload parsing, recipient gate, `issuestatus.Effective` call, binding lookup, adapter dispatch, ledger write, metrics. |
| `notifier_test.go` | Fake adapter + fake queries. Covers the three `DeliverResult` states and the ledger-write rule. |

**New migrations:**

| File | Responsibility |
| --- | --- |
| `server/migrations/450_channel_push_message.{up,down}.sql` | The ledger table. |
| `server/migrations/451_channel_push_message_id_index.{up,down}.sql` | The unique lookup index, `CONCURRENTLY`, alone. |

**Modified:**

| File | Change |
| --- | --- |
| `server/pkg/db/queries/channel.sql` | Add `CreateChannelPushMessage`, `FindChannelPushMessage`, `DeleteExpiredChannelPushMessages`. |
| `server/cmd/migrate/main.go` | Add the `concurrentIndexCleanups` entry for 451. |
| `server/internal/integrations/wecom/outbound.go` | Delete the `EventInboxNew` subscription and `handleInboxNew`; `tryDeliverInbox`'s body becomes the WeCom `DeliverDM`. |
| `server/internal/integrations/wecom/notify_dm.go` (new) | WeCom's `DMDeliverer` implementation. |
| `server/internal/integrations/lark/notify_dm.go` (new) | Lark's `DMDeliverer` implementation — the replyable one. |
| `server/internal/integrations/lark/client.go` | Add `SendDirectMessage` + `SendDirectParams` to `APIClient` (the existing sender addresses a `chat_id`; a member binding is an `open_id`). |
| `server/internal/integrations/lark/http_client.go` | Implement `SendDirectMessage`. |
| `server/internal/integrations/channel/engine/resolvers.go` | Add the `PushReplyResult` type, the `OutcomePushReply` / `OutcomePushReplyDenied` outcomes, `Result.PushReplyText`, and the `PushReplyPoster` interface. |
| `server/internal/integrations/channel/engine/router.go` | Add `PushReplies` to `RouterConfig`; insert the ledger pre-check into `processClaimed` after identity resolution, before session resolution. |
| `server/internal/handler/channel_push_reply.go` (new) | `*Handler`'s `PostPushReplyComment` + `LookupPush`, implementing `engine.PushReplyPoster`. Self-contained: create + publish + trigger, modeled on `TaskService.createAgentComment`. |
| `server/cmd/server/router.go` | Build the `notify.Notifier`, register adapters, pass `PushReplies: h` into `engine.RouterConfig`. |
| `server/cmd/server/channel_push_sweeper.go` (new) | Hourly retention sweep for the ledger. |
| `server/cmd/server/main.go` | Start the sweeper alongside the existing ones. |
| Every `replier.go` (lark / slack / telegram / wecom / dingtalk) | One `case` rendering `Result.PushReplyText`. |

**Deliberately not modified:**

- `server/internal/handler/comment.go` — see Task 6 for why the extraction was rejected.
- Anything in the trigger path. A push reply becomes an ordinary member comment, and `triggerTasksForComment` then behaves exactly as it does for a comment typed in the app. An issue with no agent assignee gets the comment and no run — the existing in-app behavior, and the spec asks for no special case.

---

## Task 1: The `channel_push_message` ledger — migrations, sqlc, cleanup entry

**Files:**
- Create: `server/migrations/450_channel_push_message.up.sql`
- Create: `server/migrations/450_channel_push_message.down.sql`
- Create: `server/migrations/451_channel_push_message_id_index.up.sql`
- Create: `server/migrations/451_channel_push_message_id_index.down.sql`
- Modify: `server/pkg/db/queries/channel.sql` (append at end of file)
- Modify: `server/cmd/migrate/main.go` (the `concurrentIndexCleanups` map, ~line 300)
- Test: `server/internal/handler/channel_push_message_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `db.ChannelPushMessage` model; `db.CreateChannelPushMessageParams{InstallationID, ChannelType, ChannelMessageID, WorkspaceID, RecipientUserID, IssueID, InboxItemID pgtype.UUID/string}`; `(*db.Queries).CreateChannelPushMessage(ctx, arg) (db.ChannelPushMessage, error)`; `db.FindChannelPushMessageParams{InstallationID pgtype.UUID, ChannelMessageID string}`; `(*db.Queries).FindChannelPushMessage(ctx, arg) (db.ChannelPushMessage, error)`; `(*db.Queries).DeleteExpiredChannelPushMessages(ctx, before pgtype.Timestamptz) (int64, error)`.

- [ ] **Step 1: Write the table migration**

`server/migrations/450_channel_push_message.up.sql`:

```sql
-- IM review push: the reply-attribution ledger.
--
-- channel_outbound_message cannot be reused: its binding_id and
-- route_revision are NOT NULL chat-session-route concepts, and an inbox
-- push has neither. Widening them to nullable would blur a table whose
-- identity is "an outbound message belonging to a chat route".
CREATE TABLE channel_push_message (
    installation_id    UUID NOT NULL,
    channel_type       TEXT NOT NULL,
    channel_message_id TEXT NOT NULL,
    workspace_id       UUID NOT NULL,
    recipient_user_id  UUID NOT NULL,
    issue_id           UUID,
    inbox_item_id      UUID NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`server/migrations/450_channel_push_message.down.sql`:

```sql
-- IM review push: the reply-attribution ledger.
DROP TABLE IF EXISTS channel_push_message;
```

- [ ] **Step 2: Write the index migration**

`server/migrations/451_channel_push_message_id_index.up.sql`:

```sql
-- IM review push: the reply-attribution ledger.
CREATE UNIQUE INDEX CONCURRENTLY idx_channel_push_message_id ON channel_push_message (installation_id, channel_message_id);
```

`server/migrations/451_channel_push_message_id_index.down.sql`:

```sql
-- IM review push: the reply-attribution ledger.
DROP INDEX CONCURRENTLY IF EXISTS idx_channel_push_message_id;
```

- [ ] **Step 3: Register the concurrent-index cleanup**

Open `server/cmd/migrate/main.go`, find the `concurrentIndexCleanups` map (the last entry is around line 300, `"446_issue_properties_bigm_index": "idx_issue_properties_bigm"`). Add, keeping numeric order:

```go
	"451_channel_push_message_id_index": "idx_channel_push_message_id",
```

**That is the only entry to add. Do not touch `concurrentDownIndexCleanups`.** The two maps are not symmetric: `concurrentIndexCleanups` registers migrations whose **up** builds an index concurrently, while `concurrentDownIndexCleanups` registers migrations whose **down** *rebuilds* one — which is why every entry in it is a `*_drop_*` migration. Migration 451's down drops the index rather than creating one, so an entry there would fail `TestConcurrentIndexCleanupsMatchTheirMigrations` with "has a cleanup hook but builds no index concurrently": the assertion greps the down file with `concurrentIndexNamePattern`, which matches `CREATE [UNIQUE] INDEX CONCURRENTLY` only (`migrate_mul5999_index_retry_test.go:18`). Migration 426, `426_channel_outbound_message_id_index`, is the structural twin of 451 and appears in the up map only.

- [ ] **Step 4: Run the guard tests to verify the cleanup entry is accepted**

Run: `(cd server && go test ./cmd/migrate -run 'TestEveryConcurrentUpBuildHasCleanup|TestEveryConcurrentDownBuildHasCleanup|TestConcurrentIndexCleanupsMatchTheirMigrations' -count=1)`
Expected: PASS on all three. Run all three, not just the up-direction one — the up guard alone cannot catch a wrong entry in the down map.

If `TestEveryConcurrentUpBuildHasCleanup` FAILS naming `451_channel_push_message_id_index`, the map key does not match the migration file's basename — fix the key, do not rename the migration. If `TestConcurrentIndexCleanupsMatchTheirMigrations` FAILS with "has a cleanup hook but builds no index concurrently", an entry was added to `concurrentDownIndexCleanups` — remove it.

- [ ] **Step 5: Write the sqlc queries**

Append to `server/pkg/db/queries/channel.sql`:

```sql
-- name: CreateChannelPushMessage :one
-- IM review push: records that a platform message we just sent is a push
-- for a specific inbox item, so a reply to it can be attributed back to an
-- issue. Only written when the adapter returned a real platform message id
-- — a push we cannot identify later is a push that cannot be replied to.
INSERT INTO channel_push_message (
    installation_id, channel_type, channel_message_id,
    workspace_id, recipient_user_id, issue_id, inbox_item_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (installation_id, channel_message_id) DO NOTHING
RETURNING *;

-- name: FindChannelPushMessage :one
-- The inbound attribution lookup: is this platform message id one of our
-- pushes? Keyed on the unique index, so at most one row.
SELECT * FROM channel_push_message
WHERE installation_id = $1 AND channel_message_id = $2;

-- name: DeleteExpiredChannelPushMessages :execrows
-- Retention sweep. A push older than the cutoff is no longer a live
-- decision prompt; keeping the row would only grow the table.
DELETE FROM channel_push_message WHERE created_at < $1;
```

Note the `ON CONFLICT ... DO NOTHING RETURNING *` on the insert: with `:one`, a conflicting insert returns `pgx.ErrNoRows`. Callers must treat that as benign (the same push already recorded), not as an error.

- [ ] **Step 6: Regenerate sqlc**

Run: `make sqlc` (from the repo root)
Expected: `server/pkg/db/generated/channel.sql.go` gains `CreateChannelPushMessage`, `FindChannelPushMessage`, `DeleteExpiredChannelPushMessages`, and `server/pkg/db/generated/models.go` gains `ChannelPushMessage`. Do not hand-edit either file.

- [ ] **Step 7: Write the failing round-trip test**

`server/internal/handler/channel_push_message_test.go`:

```go
package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// The ledger is the whole reply half's index: a push that is not findable by
// (installation, platform message id) is a push nobody can reply to.
func TestChannelPushMessageRoundTrip(t *testing.T) {
	ctx := context.Background()
	workspaceID := dbfx.Workspace(t, "Push ledger", "push-ledger-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	issueID := dbfx.Issue(t, "Pushed issue", testutil.Cols{
		"workspace_id": workspaceID, "status": "in_review",
	})
	inboxItemID := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": workspaceID, "recipient_type": "member",
		"recipient_id": testUserID, "type": "status_changed",
		"severity": "info", "issue_id": issueID, "title": "Needs review",
	})
	installationID := uuid.NewString()

	created, err := testQueries.CreateChannelPushMessage(ctx, db.CreateChannelPushMessageParams{
		InstallationID:   parseUUID(installationID),
		ChannelType:      "lark",
		ChannelMessageID: "om_abc123",
		WorkspaceID:      parseUUID(workspaceID),
		RecipientUserID:  parseUUID(testUserID),
		IssueID:          parseUUID(issueID),
		InboxItemID:      parseUUID(inboxItemID),
	})
	if err != nil {
		t.Fatalf("CreateChannelPushMessage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel_push_message WHERE installation_id = $1`, parseUUID(installationID))
	})
	if uuidToString(created.IssueID) != issueID {
		t.Errorf("created.IssueID = %q, want %q", uuidToString(created.IssueID), issueID)
	}

	found, err := testQueries.FindChannelPushMessage(ctx, db.FindChannelPushMessageParams{
		InstallationID:   parseUUID(installationID),
		ChannelMessageID: "om_abc123",
	})
	if err != nil {
		t.Fatalf("FindChannelPushMessage: %v", err)
	}
	if uuidToString(found.RecipientUserID) != testUserID {
		t.Errorf("found.RecipientUserID = %q, want %q", uuidToString(found.RecipientUserID), testUserID)
	}

	// A miss must be pgx.ErrNoRows, not a zero row: the router branches on it.
	_, err = testQueries.FindChannelPushMessage(ctx, db.FindChannelPushMessageParams{
		InstallationID:   parseUUID(installationID),
		ChannelMessageID: "om_not_ours",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("FindChannelPushMessage(miss) error = %v, want pgx.ErrNoRows", err)
	}
}

// A quick-create failure has no issue to inject a reply into. The column is
// nullable precisely for this, and the router uses it to decide replyability.
func TestChannelPushMessageAllowsNullIssue(t *testing.T) {
	ctx := context.Background()
	workspaceID := dbfx.Workspace(t, "Push ledger null issue", "push-null-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	inboxItemID := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": workspaceID, "recipient_type": "member",
		"recipient_id": testUserID, "type": "quick_create_failed",
		"severity": "attention", "title": "Quick create failed",
	})
	installationID := uuid.NewString()

	created, err := testQueries.CreateChannelPushMessage(ctx, db.CreateChannelPushMessageParams{
		InstallationID:   parseUUID(installationID),
		ChannelType:      "lark",
		ChannelMessageID: "om_null_issue",
		WorkspaceID:      parseUUID(workspaceID),
		RecipientUserID:  parseUUID(testUserID),
		InboxItemID:      parseUUID(inboxItemID),
	})
	if err != nil {
		t.Fatalf("CreateChannelPushMessage: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM channel_push_message WHERE installation_id = $1`, parseUUID(installationID))
	})
	if created.IssueID.Valid {
		t.Errorf("created.IssueID.Valid = true, want false for a push with no issue")
	}
}
```

Before running, confirm the package-level identifiers this test uses actually exist in `server/internal/handler/handler_test.go`: `dbfx`, `testUserID`, and a `*db.Queries` plus a `*pgxpool.Pool`. If the pool/queries vars are named differently than `testPool` / `testQueries`, use the existing names rather than adding new ones.

- [ ] **Step 8: Run the test to verify it fails**

Run: `(cd server && go test ./internal/handler -run 'TestChannelPushMessage' -count=1)`
Expected: FAIL — either a compile error (`undefined: db.CreateChannelPushMessageParams`) if Step 6 was skipped, or `relation "channel_push_message" does not exist` if migrations have not been applied.

- [ ] **Step 9: Apply migrations and run the test to verify it passes**

Run: `make migrate-up && (cd server && go test ./internal/handler -run 'TestChannelPushMessage' -count=1)`
Expected: PASS

- [ ] **Step 10: Verify the down migrations are reversible**

Run: `make migrate-down && make migrate-up`
Expected: both succeed. If `migrate-down` errors on `DROP INDEX CONCURRENTLY` inside a transaction, the index down-migration is not alone in its file — split it.

- [ ] **Step 11: Commit**

```bash
git add server/migrations/450_channel_push_message.up.sql \
        server/migrations/450_channel_push_message.down.sql \
        server/migrations/451_channel_push_message_id_index.up.sql \
        server/migrations/451_channel_push_message_id_index.down.sql \
        server/pkg/db/queries/channel.sql \
        server/pkg/db/generated/ \
        server/cmd/migrate/main.go \
        server/internal/handler/channel_push_message_test.go
git commit -m "feat(channel): add channel_push_message reply-attribution ledger"
```

---

## Task 2: The push whitelist

**Files:**
- Create: `server/internal/integrations/channel/notify/decision.go`
- Test: `server/internal/integrations/channel/notify/decision_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `notify.Decision{Push bool, Replyable bool}`; `notify.Decide(notifType, effectiveStatus string) Decision`.

The function is deliberately pure — it takes an *already-effective* status so it needs no context, no querier, and no bus. The caller (Task 3) resolves `issuestatus.Effective` before calling.

- [ ] **Step 1: Write the failing test**

`server/internal/integrations/channel/notify/decision_test.go`:

```go
package notify

import "testing"

func TestDecide(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		notifType       string
		effectiveStatus string
		want            Decision
	}{
		// in_review is the dominant "this needs you now" transition: an agent
		// parks completed work there. Mirrors delegatedStatusNotify's reasoning
		// in cmd/server/notification_listeners.go:89-104.
		{"in_review pushes and is replyable", "status_changed", "in_review", Decision{Push: true, Replyable: true}},
		// A custom status is normalised to its category by the caller, so it
		// arrives here already reading "in_review".
		{"custom status in the in_review category pushes", "status_changed", "in_review", Decision{Push: true, Replyable: true}},
		{"done does not push", "status_changed", "done", Decision{}},
		{"in_progress does not push", "status_changed", "in_progress", Decision{}},
		{"backlog does not push", "status_changed", "backlog", Decision{}},
		{"cancelled does not push", "status_changed", "cancelled", Decision{}},
		{"blocked pushes and is replyable", "status_changed", "blocked", Decision{Push: true, Replyable: true}},
		{"empty status does not push", "status_changed", "", Decision{}},

		// task_failed carries ReasonAgentBlocked ("Waiting on human input"),
		// which is the existing vehicle for "the agent needs a human".
		{"task_failed pushes and is replyable", "task_failed", "", Decision{Push: true, Replyable: true}},
		{"task_failed ignores status", "task_failed", "in_progress", Decision{Push: true, Replyable: true}},

		// Quick-create failures have no issue, so there is nothing to inject a
		// reply into. They still push — the user asked for something and it
		// did not happen.
		{"quick_create_failed pushes but is not replyable", "quick_create_failed", "", Decision{Push: true}},
		{"quick_create_unconfirmed pushes but is not replyable", "quick_create_unconfirmed", "", Decision{Push: true}},

		// Everything else is noise for an IM DM.
		{"new_comment does not push", "new_comment", "", Decision{}},
		{"mentioned does not push", "mentioned", "", Decision{}},
		{"issue_assigned does not push", "issue_assigned", "", Decision{}},
		{"agent_completed does not push", "agent_completed", "", Decision{}},
		{"task_completed does not push", "task_completed", "", Decision{}},
		{"unknown type does not push", "some_future_type", "in_review", Decision{}},
		{"empty type does not push", "", "in_review", Decision{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Decide(tt.notifType, tt.effectiveStatus); got != tt.want {
				t.Errorf("Decide(%q, %q) = %+v, want %+v", tt.notifType, tt.effectiveStatus, got, tt.want)
			}
		})
	}
}

// Replyable must never be true without Push: an unpushed notification has no
// message for anyone to reply to, and the ledger would record a dangling row.
func TestDecideNeverReplyableWithoutPush(t *testing.T) {
	t.Parallel()
	types := []string{
		"status_changed", "task_failed", "quick_create_failed",
		"quick_create_unconfirmed", "new_comment", "mentioned", "",
	}
	statuses := []string{"", "backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}
	for _, ty := range types {
		for _, st := range statuses {
			if d := Decide(ty, st); d.Replyable && !d.Push {
				t.Errorf("Decide(%q, %q) = %+v: replyable without push", ty, st, d)
			}
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `(cd server && go test ./internal/integrations/channel/notify -count=1)`
Expected: FAIL — the package does not exist yet (`no Go files in .../notify`).

- [ ] **Step 3: Write the implementation**

`server/internal/integrations/channel/notify/decision.go`:

```go
// Package notify forwards a narrow whitelist of inbox notifications to the
// recipient's bound IM channel as a direct message, and records the ones a
// reply can be attributed back to.
//
// This package decides; it never sends. Sending belongs to the per-platform
// DMDeliverer adapters, because the generic channel.Channel.Send seam does
// not exist for every platform — wecomChannel.Send deliberately returns
// ErrSendNotSupported rather than keep a heuristic second outbound path
// alive (wecom/wecom_channel.go:566).
package notify

// Decision is the whitelist's verdict for one inbox notification.
type Decision struct {
	// Push reports whether this notification is worth interrupting a human
	// in their IM client.
	Push bool
	// Replyable reports whether a reply to the pushed message can be
	// attributed back to an issue. False when the notification has no issue
	// to inject a comment into.
	Replyable bool
}

// pushableStatusChange lists the effective status categories whose
// status_changed notification is worth a DM.
//
// Only in_review. In Multica's agent flow an agent parks completed work in
// in_review, so that is the transition that needs a human now — not done,
// which is the outcome of one. Same product judgement as
// delegatedStatusNotify (cmd/server/notification_listeners.go:89-104), scoped
// tighter because a DM is more interruptive than an inbox row.
//
// Changing this map is a code change on purpose. Whether a class of event
// deserves to interrupt anyone is a global product judgement; whether a given
// person wants to be interrupted is already answered by notification_preference,
// which gates before the inbox row exists.
var pushableStatusChange = map[string]bool{
	"in_review": true,
}

// pushableTypes lists non-status notification types that always push.
// The bool is Replyable: an entry is false when the notification carries no
// issue, so a reply would have nowhere to land.
var pushableTypes = map[string]bool{
	// task_failed is where the daemon's blocked classifications surface,
	// including taskfailure.ReasonAgentBlocked ("Waiting on human input").
	"task_failed": true,

	// Quick-create outcomes are about an issue that was never created, so
	// there is no issue_id and no comment target.
	"quick_create_failed":      false,
	"quick_create_unconfirmed": false,
}

// Decide applies the push whitelist.
//
// effectiveStatus must already be normalised through issuestatus.Effective so
// a workspace's custom status is judged by the category it inherits, not by
// its literal key — the same reasoning deliverToSubscriber's allowlist uses.
// It is ignored for types other than status_changed.
func Decide(notifType, effectiveStatus string) Decision {
	if notifType == "status_changed" {
		if pushableStatusChange[effectiveStatus] {
			return Decision{Push: true, Replyable: true}
		}
		return Decision{}
	}
	replyable, ok := pushableTypes[notifType]
	if !ok {
		return Decision{}
	}
	return Decision{Push: true, Replyable: replyable}
}
```

Note what is deliberately absent: severity. `status_changed` rows are written with severity `"info"`, the same level as passing noise, so gating on severity would drop exactly the notification this feature exists for. The "you are needed" semantics live in the type plus the target status category.

- [ ] **Step 4: Run the test to verify it passes**

Run: `(cd server && go test ./internal/integrations/channel/notify -count=1 -v)`
Expected: PASS, all subtests.

- [ ] **Step 5: Commit**

```bash
git add server/internal/integrations/channel/notify/decision.go \
        server/internal/integrations/channel/notify/decision_test.go
git commit -m "feat(notify): add the IM push whitelist"
```

---

## Task 3: The notifier — bus subscription, adapter dispatch, ledger write

**Files:**
- Create: `server/internal/integrations/channel/notify/deliver.go`
- Create: `server/internal/integrations/channel/notify/notifier.go`
- Test: `server/internal/integrations/channel/notify/notifier_test.go`

**Interfaces:**
- Consumes: `notify.Decide` (Task 2); `db.CreateChannelPushMessageParams`, `(*db.Queries).CreateChannelPushMessage` (Task 1).
- Produces:
  - `notify.DeliverState` (`StateDelivered` / `StateHandedOff` / `StateUnsupported`)
  - `notify.DeliverResult{State DeliverState, MessageID string}`
  - `notify.PushRef{InboxItemID, RecipientUserID string}` and the `notify.DMDeliverer` interface: `DeliverDM(ctx context.Context, ref PushRef, binding db.ChannelUserBinding, text string) (DeliverResult, error)`
  - `notify.Notifier` + `notify.New(q Queries, logger *slog.Logger) *Notifier`
  - `(*Notifier).Register(adapters map[string]DMDeliverer)` and `(*Notifier).Subscribe(bus *events.Bus)`
  - `notify.Queries` interface (what the notifier needs from `*db.Queries`)

- [ ] **Step 1: Write the delivery contract**

`server/internal/integrations/channel/notify/deliver.go`:

```go
package notify

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DeliverState is the outcome of asking an adapter to send a direct message.
type DeliverState int

const (
	// StateUnsupported means the adapter does not do direct messages. The
	// zero value, so an adapter that returns an empty result is treated as
	// having done nothing rather than as having succeeded.
	StateUnsupported DeliverState = iota

	// StateDelivered means the platform accepted the message. MessageID is
	// the platform's identifier for it when the platform gives one out.
	//
	// An empty MessageID is legal and means "sent, but not addressable": the
	// push happened, and no reply to it can be attributed back. WeCom is
	// exactly this case — sendTextCtx discards the send ack and the inbound
	// callback carries no reply context at all.
	StateDelivered

	// StateHandedOff means this replica could not send but passed the
	// message to the replica that can. Whether it goes out, and what message
	// id it gets, are both unknowable from here.
	//
	// This exists for WeCom: it has no outbound REST path, every write goes
	// over the aibot WebSocket, and the WS lease means exactly one replica
	// holds it — while EventInboxNew fires on whichever replica the load
	// balancer picked. The hand-off must be asynchronous (a synchronous
	// round-trip would let one unhealthy bot stall an entire shard), so no
	// receipt comes back.
	//
	// It is its own state because counting it as delivered makes the metrics
	// lie, and counting it as failure would trip a fallback that should not
	// fire.
	StateHandedOff
)

// DeliverResult is what an adapter reports back.
type DeliverResult struct {
	State DeliverState
	// MessageID is the platform message identifier, when the platform
	// returned one. Only a non-empty value makes a push replyable.
	MessageID string
}

// PushRef identifies the inbox row a push came from.
//
// It exists because WeCom's cross-replica relay claims work under
// relayInboxEventID(inboxItemID, recipientUserID). Once the subscription
// moved up into this package the adapter stopped being able to look those
// ids up, so they are passed down. Adapters that do not relay ignore it.
type PushRef struct {
	InboxItemID     string
	RecipientUserID string
}

// DMDeliverer sends one direct message to a bound member.
//
// This is deliberately not channel.Channel.Send. That seam does not exist on
// every platform — wecomChannel.Send returns ErrSendNotSupported by design —
// so a shared layer built on it would be dead on arrival for WeCom. The
// binding is passed whole so the adapter, not this package, decides how to
// address a single chat; the shared layer never guesses single-vs-group.
type DMDeliverer interface {
	DeliverDM(ctx context.Context, ref PushRef, binding db.ChannelUserBinding, text string) (DeliverResult, error)
}
```

- [ ] **Step 2: Write the failing notifier test**

`server/internal/integrations/channel/notify/notifier_test.go`:

```go
package notify

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	testWorkspace = "11111111-1111-1111-1111-111111111111"
	testRecipient = "22222222-2222-2222-2222-222222222222"
	testIssue     = "33333333-3333-3333-3333-333333333333"
	testInboxItem = "44444444-4444-4444-4444-444444444444"
	testInstall   = "55555555-5555-5555-5555-555555555555"
)

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return u
}

type fakeQueries struct {
	binding    db.ChannelUserBinding
	bindingErr error
	workspace  db.Workspace
	statusKey  string // what Effective should report for a custom status
	created    []db.CreateChannelPushMessageParams
	createErr  error
}

func (f *fakeQueries) FindChannelBindingForMember(context.Context, db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error) {
	if f.bindingErr != nil {
		return db.ChannelUserBinding{}, f.bindingErr
	}
	return f.binding, nil
}

func (f *fakeQueries) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return f.workspace, nil
}

func (f *fakeQueries) GetIssueStatusEntryByKey(_ context.Context, arg db.GetIssueStatusEntryByKeyParams) (db.IssueStatusEntry, error) {
	if f.statusKey == "" || arg.Key != f.statusKey {
		return db.IssueStatusEntry{}, pgx.ErrNoRows
	}
	return db.IssueStatusEntry{Key: arg.Key, Category: "in_review"}, nil
}

func (f *fakeQueries) CreateChannelPushMessage(_ context.Context, arg db.CreateChannelPushMessageParams) (db.ChannelPushMessage, error) {
	if f.createErr != nil {
		return db.ChannelPushMessage{}, f.createErr
	}
	f.created = append(f.created, arg)
	return db.ChannelPushMessage{}, nil
}

type fakeAdapter struct {
	result DeliverResult
	err    error
	calls  int
	lastTo db.ChannelUserBinding
	lastTx string
}

func (a *fakeAdapter) DeliverDM(_ context.Context, _ PushRef, binding db.ChannelUserBinding, text string) (DeliverResult, error) {
	a.calls++
	a.lastTo = binding
	a.lastTx = text
	return a.result, a.err
}

func newTestNotifier(t *testing.T, q *fakeQueries, a *fakeAdapter) *Notifier {
	t.Helper()
	q.binding.InstallationID = mustUUID(t, testInstall)
	q.binding.ChannelType = "lark"
	if q.binding.ChannelUserID == "" {
		q.binding.ChannelUserID = "ou_target"
	}
	n := New(q, slog.Default())
	n.Register(map[string]DMDeliverer{"lark": a})
	return n
}

func inReviewEvent() events.Event {
	return events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: testWorkspace,
		Payload: map[string]any{"item": map[string]any{
			"id":             testInboxItem,
			"workspace_id":   testWorkspace,
			"recipient_type": "member",
			"recipient_id":   testRecipient,
			"type":           "status_changed",
			"severity":       "info",
			"issue_id":       testIssue,
			"issue_status":   "in_review",
			"title":          "Ship the thing",
		}},
	}
}

func TestNotifierDeliversAndRecordsAnInReviewPush(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_123"}}
	newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())

	if a.calls != 1 {
		t.Fatalf("adapter calls = %d, want 1", a.calls)
	}
	if a.lastTo.ChannelUserID != "ou_target" {
		t.Errorf("delivered to %q, want ou_target", a.lastTo.ChannelUserID)
	}
	if a.lastTx == "" {
		t.Error("delivered empty text")
	}
	if len(q.created) != 1 {
		t.Fatalf("ledger rows = %d, want 1", len(q.created))
	}
	got := q.created[0]
	if got.ChannelMessageID != "om_123" {
		t.Errorf("ledger ChannelMessageID = %q, want om_123", got.ChannelMessageID)
	}
	if util.UUIDToString(got.IssueID) != testIssue {
		t.Errorf("ledger IssueID = %q, want %q", util.UUIDToString(got.IssueID), testIssue)
	}
	if util.UUIDToString(got.RecipientUserID) != testRecipient {
		t.Errorf("ledger RecipientUserID = %q, want %q", util.UUIDToString(got.RecipientUserID), testRecipient)
	}
}

// The ledger is an index of replyable pushes. A push nobody can reply to must
// not create a row: it would be a permanent miss that only grows the table.
func TestNotifierDoesNotRecordWhenTheresNothingToReplyTo(t *testing.T) {
	tests := []struct {
		name   string
		result DeliverResult
	}{
		{"handed off to another replica", DeliverResult{State: StateHandedOff}},
		{"delivered without a platform message id", DeliverResult{State: StateDelivered}},
		{"adapter does not support DMs", DeliverResult{State: StateUnsupported}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
			a := &fakeAdapter{result: tt.result}
			newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())
			if len(q.created) != 0 {
				t.Errorf("ledger rows = %d, want 0", len(q.created))
			}
		})
	}
}

// A pushed-but-unreplyable notification type must reach the user and still
// leave no ledger row, even though the adapter handed back a message id.
func TestNotifierDoesNotRecordUnreplyableTypes(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_qc"}}
	e := inReviewEvent()
	item := e.Payload.(map[string]any)["item"].(map[string]any)
	item["type"] = "quick_create_failed"
	delete(item, "issue_id")
	delete(item, "issue_status")

	newTestNotifier(t, q, a).HandleInboxNew(e)

	if a.calls != 1 {
		t.Errorf("adapter calls = %d, want 1 (quick_create_failed still pushes)", a.calls)
	}
	if len(q.created) != 0 {
		t.Errorf("ledger rows = %d, want 0 (no issue to reply into)", len(q.created))
	}
}

func TestNotifierSkipsWhatTheWhitelistRejects(t *testing.T) {
	tests := []struct {
		name       string
		notifType  string
		issueState string
	}{
		{"a done transition", "status_changed", "done"},
		{"an in_progress transition", "status_changed", "in_progress"},
		{"a new comment", "new_comment", ""},
		{"a mention", "mentioned", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
			a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
			e := inReviewEvent()
			item := e.Payload.(map[string]any)["item"].(map[string]any)
			item["type"] = tt.notifType
			item["issue_status"] = tt.issueState

			newTestNotifier(t, q, a).HandleInboxNew(e)

			if a.calls != 0 {
				t.Errorf("adapter calls = %d, want 0", a.calls)
			}
		})
	}
}

// A workspace's custom status inherits its category's behaviour. Judging the
// literal key would silently exclude every workspace that renamed in_review.
func TestNotifierNormalisesACustomStatusToItsCategory(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}, statusKey: "awaiting_signoff"}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_custom"}}
	e := inReviewEvent()
	e.Payload.(map[string]any)["item"].(map[string]any)["issue_status"] = "awaiting_signoff"

	newTestNotifier(t, q, a).HandleInboxNew(e)

	if a.calls != 1 {
		t.Errorf("adapter calls = %d, want 1 for a custom status in the in_review category", a.calls)
	}
}

func TestNotifierIgnoresAgentRecipients(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
	e := inReviewEvent()
	e.Payload.(map[string]any)["item"].(map[string]any)["recipient_type"] = "agent"

	newTestNotifier(t, q, a).HandleInboxNew(e)

	if a.calls != 0 {
		t.Errorf("adapter calls = %d, want 0", a.calls)
	}
}

// An unbound member is not an error. They keep seeing the notification in the
// in-app inbox, which is the degradation WeCom's existing path already set.
func TestNotifierIsANoOpForAnUnboundMember(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}, bindingErr: pgx.ErrNoRows}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
	newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())

	if a.calls != 0 {
		t.Errorf("adapter calls = %d, want 0", a.calls)
	}
	if len(q.created) != 0 {
		t.Errorf("ledger rows = %d, want 0", len(q.created))
	}
}

func TestNotifierSurvivesAnAdapterError(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{err: errors.New("platform refused the message")}
	newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())

	if len(q.created) != 0 {
		t.Errorf("ledger rows = %d, want 0 after a failed send", len(q.created))
	}
}

// The binding names the platform; a workspace bound to a channel we have no
// adapter for must not panic on the map miss.
func TestNotifierIgnoresAChannelWithNoAdapter(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
	n := newTestNotifier(t, q, a)
	q.binding.ChannelType = "dingtalk"

	n.HandleInboxNew(inReviewEvent())

	if a.calls != 0 {
		t.Errorf("adapter calls = %d, want 0", a.calls)
	}
}

func TestNotifierIgnoresMalformedPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload any
	}{
		{"not a map", "nonsense"},
		{"no item key", map[string]any{}},
		{"item is not a map", map[string]any{"item": 42}},
		{"no recipient", map[string]any{"item": map[string]any{
			"workspace_id": testWorkspace, "recipient_type": "member", "type": "task_failed",
		}}},
		{"no workspace", map[string]any{"item": map[string]any{
			"recipient_id": testRecipient, "recipient_type": "member", "type": "task_failed",
		}}},
		{"unparseable recipient", map[string]any{"item": map[string]any{
			"workspace_id": testWorkspace, "recipient_id": "not-a-uuid",
			"recipient_type": "member", "type": "task_failed",
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
			a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
			// Must not panic.
			newTestNotifier(t, q, a).HandleInboxNew(events.Event{
				Type: protocol.EventInboxNew, WorkspaceID: testWorkspace, Payload: tt.payload,
			})
			if a.calls != 0 {
				t.Errorf("adapter calls = %d, want 0", a.calls)
			}
		})
	}
}
```

Before writing the implementation, check the real field names on `db.IssueStatusEntry` and `db.GetIssueStatusEntryByKeyParams` (`server/pkg/db/generated/`) and on `db.Workspace`; adjust the fake to match rather than inventing fields.

- [ ] **Step 3: Run the test to verify it fails**

Run: `(cd server && go test ./internal/integrations/channel/notify -count=1)`
Expected: FAIL — `undefined: New`, `undefined: Notifier`.

- [ ] **Step 4: Write the notifier**

`server/internal/integrations/channel/notify/notifier.go`:

```go
package notify

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// deliverTimeout bounds one push. The bus dispatches handlers synchronously,
// so an unbounded send here would stall whichever request published the
// notification.
const deliverTimeout = 5 * time.Second

// Queries is what the notifier needs from the database. *db.Queries
// satisfies it. issuestatus.Querier is embedded because Effective resolves a
// workspace's custom statuses through it.
type Queries interface {
	issuestatus.Querier
	FindChannelBindingForMember(ctx context.Context, arg db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	CreateChannelPushMessage(ctx context.Context, arg db.CreateChannelPushMessageParams) (db.ChannelPushMessage, error)
}

// Notifier forwards whitelisted inbox notifications to bound IM channels.
type Notifier struct {
	q        Queries
	logger   *slog.Logger
	adapters map[string]DMDeliverer
}

func New(q Queries, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{q: q, logger: logger, adapters: map[string]DMDeliverer{}}
}

// Register installs the per-channel_type adapters. Channels with no adapter
// are silently skipped.
func (n *Notifier) Register(adapters map[string]DMDeliverer) {
	for k, v := range adapters {
		n.adapters[k] = v
	}
}

func (n *Notifier) Subscribe(bus *events.Bus) {
	bus.Subscribe(protocol.EventInboxNew, n.HandleInboxNew)
}

// HandleInboxNew is the inbox:new subscriber.
//
// Every miss is a no-op, never an error: a non-member recipient, a type the
// whitelist rejects, an unbound member, a channel with no adapter, a send
// that failed. In all of them the member still sees the notification in the
// in-app inbox, which is the degradation WeCom's original path established.
//
// Muting needs no handling here. notifTypeToGroup + isNotifMuted run before
// the inbox row is created, so a muted notification produces no row, no
// EventInboxNew, and therefore no push.
func (n *Notifier) HandleInboxNew(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return
	}
	// Only member recipients. Agents have no IM identity to DM.
	if rt, _ := item["recipient_type"].(string); rt != "member" {
		return
	}
	recipientID, ok := parseItemUUID(item, "recipient_id")
	if !ok {
		return
	}
	workspaceID, ok := parseItemUUID(item, "workspace_id")
	if !ok {
		return
	}
	inboxItemID, _ := parseItemUUID(item, "id")

	ctx, cancel := context.WithTimeout(context.Background(), deliverTimeout)
	defer cancel()

	notifType, _ := item["type"].(string)
	rawStatus, _ := item["issue_status"].(string)
	effective := rawStatus
	if rawStatus != "" {
		effective = issuestatus.Effective(ctx, n.q, workspaceID, rawStatus)
	}
	decision := Decide(notifType, effective)
	if !decision.Push {
		return
	}

	binding, err := n.q.FindChannelBindingForMember(ctx, db.FindChannelBindingForMemberParams{
		WorkspaceID:   workspaceID,
		MulticaUserID: recipientID,
		ChannelType:   n.boundChannelType(ctx, workspaceID, recipientID),
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			n.logger.WarnContext(ctx, "notify: member binding lookup failed",
				"error", err, "workspace_id", util.UUIDToString(workspaceID))
		}
		return
	}
	adapter, ok := n.adapters[binding.ChannelType]
	if !ok {
		return
	}

	slug := ""
	if ws, wsErr := n.q.GetWorkspace(ctx, workspaceID); wsErr == nil {
		slug = ws.Slug
	}
	text := renderPush(item, util.UUIDToString(workspaceID), slug, effective, decision.Replyable)
	if text == "" {
		return
	}

	ref := PushRef{
		InboxItemID:     util.UUIDToString(inboxItemID),
		RecipientUserID: util.UUIDToString(recipientID),
	}
	res, err := adapter.DeliverDM(ctx, ref, binding, text)
	if err != nil {
		n.logger.WarnContext(ctx, "notify: push failed",
			"error", err, "channel_type", binding.ChannelType,
			"workspace_id", util.UUIDToString(workspaceID))
		return
	}

	// Only a delivered push with a real platform message id can be replied
	// to. StateHandedOff has no id by construction (the relay is one-way),
	// and some platforms deliver without returning one at all.
	if !decision.Replyable || res.State != StateDelivered || res.MessageID == "" {
		return
	}
	issueID, _ := parseItemUUID(item, "issue_id")
	if _, err := n.q.CreateChannelPushMessage(ctx, db.CreateChannelPushMessageParams{
		InstallationID:   binding.InstallationID,
		ChannelType:      binding.ChannelType,
		ChannelMessageID: res.MessageID,
		WorkspaceID:      workspaceID,
		RecipientUserID:  recipientID,
		IssueID:          issueID,
		InboxItemID:      inboxItemID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// ErrNoRows is the ON CONFLICT DO NOTHING path: already recorded.
		n.logger.WarnContext(ctx, "notify: recording the push for reply attribution failed",
			"error", err, "channel_type", binding.ChannelType)
	}
}

func parseItemUUID(item map[string]any, key string) (pgtype.UUID, bool) {
	s, _ := item[key].(string)
	if s == "" {
		return pgtype.UUID{}, false
	}
	u, err := util.ParseUUID(s)
	if err != nil || !u.Valid {
		return pgtype.UUID{}, false
	}
	return u, true
}
```

`boundChannelType` and `renderPush` are stubs to fill in this same step. Take the simplest thing that satisfies the tests and the spec:

```go
// boundChannelType picks which channel to DM. FindChannelBindingForMember
// takes one channel_type, so the notifier tries the registered adapters in a
// fixed order and uses the first bound one. Only one message is ever sent.
func (n *Notifier) boundChannelType(ctx context.Context, workspaceID, recipientID pgtype.UUID) string {
	for _, ct := range n.channelOrder() {
		if _, err := n.q.FindChannelBindingForMember(ctx, db.FindChannelBindingForMemberParams{
			WorkspaceID: workspaceID, MulticaUserID: recipientID, ChannelType: ct,
		}); err == nil {
			return ct
		}
	}
	return ""
}
```

Note the shape problem this creates: `HandleInboxNew` then calls `FindChannelBindingForMember` twice. Prefer collapsing it — have `boundChannelType` return the `db.ChannelUserBinding` it already fetched and delete the second lookup from `HandleInboxNew`. Rename it `findBinding(ctx, workspaceID, recipientID) (db.ChannelUserBinding, bool)`. Adjust the calling code accordingly; the tests above are written against observable behaviour (adapter calls, ledger rows), not against this helper, so they do not need changing.

`channelOrder()` returns the registered adapter keys in a deterministic order — sort them, so two replicas make the same choice for a member bound to two platforms.

`renderPush(item map[string]any, workspaceID, slug, effectiveStatus string, replyable bool) string` builds the message. Put it in `notifier.go` for now with this behaviour:
- Returns `""` when both `title` and `type` are empty (nothing worth sending).
- Renders `**[<label>] <title>**\n<body>\n<deep link>`.
- A `status_changed` push whose effective category is `in_review` renders the action label `待你审核` and the body `任务已进入 in_review，等待你的审核。`; it must not expose the transport label `状态变更`.
- When that review push is replyable, append `审核通过可回复「审核通过」；需要修改请直接说明，Multica 会结合任务上下文继续处理。` An exact approval reply is a direct `done` transition; a reply with additional instructions stays contextual and does not auto-transition.
- Other replyable notification types retain the generic hint `直接回复本条消息即可处理。` and their own event-specific label/body.
- The deep link is `<app base>/<slug or workspace uuid>/issues/<issue_id>`; when there is no `issue_id`, link to the inbox instead.

Do not re-derive the base URL here — find how `wecom/inbox_message.go`'s `inboxItemLink` reads it and use the same source.

- [ ] **Step 5: Run the test to verify it passes**

Run: `(cd server && go test ./internal/integrations/channel/notify -count=1 -v)`
Expected: PASS, all subtests.

- [ ] **Step 6: Vet and format**

Run: `(cd server && gofmt -l ./internal/integrations/channel/notify && go vet ./internal/integrations/channel/notify)`
Expected: no output from either.

- [ ] **Step 7: Commit**

```bash
git add server/internal/integrations/channel/notify/
git commit -m "feat(notify): dispatch whitelisted inbox pushes to bound IM channels"
```

---

## Task 4: Move WeCom's inbox push under the notifier

WeCom already pushes every inbox item to bound members. After this task the notifier owns the subscription and the filtering, and WeCom owns only the sending. Leaving both paths registered would send WeCom users two copies of every whitelisted notification.

WeCom pushes are never replyable. Its send ack is discarded (`ws_sender.go:297-304` — `_, err = s.request(...)`), and `aibotMsgCallback` (`ws_frame.go:74-94`) has no reply/quote field, so an inbound WeCom reply carries `ReplyTo == nil`. The adapter therefore returns `Delivered` with an **empty** `MessageID`, and the notifier records nothing.

**Files:**
- Create: `server/internal/integrations/wecom/notify_dm.go`
- Modify: `server/internal/integrations/wecom/outbound.go` (delete `handleInboxNew`; drop the `EventInboxNew` line from `Register`; move `tryDeliverInbox`'s body)
- Modify: `server/internal/integrations/wecom/outbound_test.go` (existing inbox tests move to the new entry point)
- Test: `server/internal/integrations/wecom/notify_dm_test.go`

**Interfaces:**
- Consumes: `notify.DMDeliverer`, `notify.DeliverResult`, `notify.StateDelivered`, `notify.StateHandedOff` (Task 3).
- Produces: `(*wecom.Outbound).DeliverDM(ctx context.Context, ref notify.PushRef, binding db.ChannelUserBinding, text string) (notify.DeliverResult, error)` — `*Outbound` itself implements the interface, since it already owns the senders registry, the relay and the logger.

- [ ] **Step 1: Write the failing adapter test**

`server/internal/integrations/wecom/notify_dm_test.go`:

```go
package wecom

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WeCom sends over the aibot socket, which reports no platform message id.
// Delivered-with-no-id is the honest answer: the push happened and nobody can
// reply to it.
func TestDeliverDMReportsDeliveredWithoutAMessageID(t *testing.T) {
	q := &fakeOutboundQueries{}
	o, instID, conn := newOutboundWithConn(t, q)
	binding := db.ChannelUserBinding{InstallationID: instID, ChannelUserID: "T_USER_1"}

	res, err := o.DeliverDM(context.Background(), testPushRef, binding, "**[in_review] Ship it**")
	if err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}
	if res.State != notify.StateDelivered {
		t.Errorf("State = %v, want StateDelivered", res.State)
	}
	if res.MessageID != "" {
		t.Errorf("MessageID = %q, want empty: wecom send acks carry no id", res.MessageID)
	}

	body := conn.sendBody(t, 0)
	if body["chatid"] != "T_USER_1" {
		t.Errorf("chatid = %v, want T_USER_1", body["chatid"])
	}
	// The binding's channel_user_id is the bot-scoped T-* userid, which WeCom
	// treats as the chatid of a single chat. Never guess group.
	if body["chat_type"] != float64(chatTypeSingleInt) {
		t.Errorf("chat_type = %v, want %d", body["chat_type"], chatTypeSingleInt)
	}
}

// EventInboxNew fires on whichever replica the load balancer picked; the WS
// lease lives on exactly one. Handing the frame to that replica is neither a
// delivery nor a failure.
func TestDeliverDMHandsOffWhenThisReplicaHasNoSocket(t *testing.T) {
	q := &fakeOutboundQueries{}
	o := NewOutbound(q, newSendersRegistry(), slog.Default(), WithRelay(newTestRelay(t)))
	binding := db.ChannelUserBinding{InstallationID: mustTestUUID(t), ChannelUserID: "T_USER_1"}

	res, err := o.DeliverDM(context.Background(), testPushRef, binding, "text")
	if err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}
	if res.State != notify.StateHandedOff {
		t.Errorf("State = %v, want StateHandedOff", res.State)
	}
}
```

Read `outbound_test.go` and `relay_outbound_test.go` first: reuse whatever they already provide for `newOutboundWithConn`, `mustTestUUID`, `fakeOutboundQueries`, and a relay test double. Do not invent `newTestRelay` if an equivalent exists under another name; if none exists, build the smallest one that lets `o.relay.publish` return true.

- [ ] **Step 2: Run the test to verify it fails**

Run: `(cd server && go test ./internal/integrations/wecom -run TestDeliverDM -count=1)`
Expected: FAIL — `o.DeliverDM undefined`.

- [ ] **Step 3: Write the adapter**

`server/internal/integrations/wecom/notify_dm.go`:

```go
package wecom

import (
	"context"

	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DeliverDM sends an inbox push to a bound member over the aibot socket.
//
// This is the body that used to live in tryDeliverInbox. The subscription,
// the whitelist and the binding lookup moved up into the notify package; what
// stays here is the part that is genuinely WeCom's — addressing a single chat
// by the bot-scoped T-* userid, and routing across replicas when this one
// does not hold the WS lease.
//
// The result never carries a MessageID. sendTextCtx reads the ack only for
// its error code and discards the body, and WeCom's inbound callback carries
// no reply context, so a WeCom push cannot be replied to no matter what we
// record. Reporting an empty id keeps that honest rather than writing a
// ledger row no inbound message will ever match.
func (o *Outbound) DeliverDM(ctx context.Context, ref notify.PushRef, binding db.ChannelUserBinding, text string) (notify.DeliverResult, error) {
	if text == "" {
		return notify.DeliverResult{}, nil
	}
	var sender *wsSender
	if o.senders != nil {
		sender = o.senders.get(binding.InstallationID)
	}
	if sender == nil {
		if o.relay.publish(relayFrame{
			Kind:           relayKindInbox,
			InstallationID: util.UUIDToString(binding.InstallationID),
			ChatID:         binding.ChannelUserID,
			ChatType:       chatTypeSingleInt,
			Content:        text,
		}, relayInboxEventID(ref.InboxItemID, ref.RecipientUserID)) {
			o.logger.DebugContext(ctx, "wecom outbound: routed an inbox push to the replica holding the socket",
				"installation_id", uuidStringPub(binding.InstallationID))
			return notify.DeliverResult{State: notify.StateHandedOff}, nil
		}
		o.logger.WarnContext(ctx, "wecom outbound: inbox push not delivered and not routable",
			"installation_id", uuidStringPub(binding.InstallationID))
		return notify.DeliverResult{}, nil
	}
	if err := sender.sendTextCtx(ctx, binding.ChannelUserID, chatTypeSingleInt, text); err != nil {
		return notify.DeliverResult{}, err
	}
	o.logger.DebugContext(ctx, "wecom outbound: inbox delivered via bot",
		"installation_id", uuidStringPub(binding.InstallationID))
	return notify.DeliverResult{State: notify.StateDelivered}, nil
}
```

One thing to check while writing this: `relayInboxEventID(itemID, recipientID)` is the relay's dedup/claim key, and both ids now arrive in `ref` instead of being looked up locally. That is the whole reason `PushRef` is on the interface. Confirm the two strings the notifier passes are the same two the deleted `handleInboxNew` passed, so an in-flight claim from the previous build is not orphaned by a silent key change.

`testPushRef` in the test file is any non-empty `notify.PushRef`; the WeCom tests do not assert on it beyond the relay key.

- [ ] **Step 4: Run the adapter test to verify it passes**

Run: `(cd server && go test ./internal/integrations/wecom -run TestDeliverDM -count=1 -v)`
Expected: PASS

- [ ] **Step 5: Delete the old subscription and handler**

In `server/internal/integrations/wecom/outbound.go`:

- In `Register` (line ~137), delete the line `bus.Subscribe(protocol.EventInboxNew, o.handleInboxNew)`. Leave the `EventChatDone` line.
- Delete `handleInboxNew` entirely (lines ~342-371).
- Delete `tryDeliverInbox` entirely (lines ~373-454). Its binding lookup, workspace-slug lookup and `buildInboxMarkdown` call are now the notifier's job; its send is now `DeliverDM`.
- Remove any imports and `outboundQueries` methods left unused by those deletions. `FindChannelBindingForMember` and `GetWorkspace` may now be unused on that interface — if so, drop them from `outboundQueries`; do not leave dead interface methods behind.
- `buildInboxMarkdown` in `inbox_message.go` becomes unreferenced from WeCom. Do not delete it yet: Task 3's `renderPush` should be built from it. Either move it into the notify package in this task and update `inbox_message_test.go` accordingly, or leave it and open the move as the first step of a follow-up. Prefer moving it now — a live copy nobody calls is exactly the parallel-path problem this task exists to remove.

- [ ] **Step 6: Move the existing inbox tests to the new entry point**

`outbound_test.go` has tests driving `tryDeliverInbox` and `handleInboxNew` (e.g. `TestTryDeliverInbox_PushesToBoundMemberPrivately`). For each one, decide where its assertion now belongs:

- Assertions about *whether* a notification is pushed (recipient type, binding presence, whitelist) → already covered by `notify/notifier_test.go` in Task 3. Delete the WeCom copy rather than duplicating the matrix.
- Assertions about *how* WeCom addresses the chat (chatid, chat_type, relay routing) → keep, rewritten against `DeliverDM` as in Step 1.

- [ ] **Step 7: Run the full WeCom suite**

Run: `(cd server && go test ./internal/integrations/wecom -count=1)`
Expected: PASS. Compile errors naming `handleInboxNew` or `tryDeliverInbox` mean a test still references a deleted function — move or delete it per Step 6.

- [ ] **Step 8: Verify no second push path survives**

Run: `(cd server && grep -rn "EventInboxNew" --include='*.go' internal/ cmd/ | grep -v '_test.go')`
Expected: exactly two non-test hits — the constant's definition in `pkg/protocol/events.go`, and `notify/notifier.go`'s `Subscribe`. Plus the publish sites in `cmd/server/notification_listeners.go`, `internal/service/`, etc. **No `Subscribe(protocol.EventInboxNew, ...)` outside the notify package.** If `cmd/server/listeners.go:108` has one, read it — if it serves a different purpose (websocket fan-out to browsers), leave it; if it is a second IM push, fold it in.

- [ ] **Step 9: Commit**

```bash
git add server/internal/integrations/wecom/
git commit -m "refactor(wecom): move the inbox push behind the shared notify layer"
```

---

## Task 5: The Lark adapter — the replyable one

Lark is the only platform where the whole loop works: `Send` returns a real message id and inbound messages carry a per-message `ReplyCtx`. But the existing send path cannot be reused as-is — `outboundMessageRequest` (`http_client.go:326-342`) posts with `receive_id_type=chat_id`, while a member binding's `ChannelUserID` is an **open_id**. The only open_id sender today is `SendBindingPromptCard` (`http_client.go:501-520`), and it returns `error` with no message id.

So this task adds one client method: send text to an open_id, return the message id.

**Files:**
- Modify: `server/internal/integrations/lark/client.go` (add `SendDirectMessage` to the `APIClient` interface, the `SendDirectParams` struct, and the `stubAPIClient` implementation)
- Modify: `server/internal/integrations/lark/http_client.go` (implement `SendDirectMessage`)
- Create: `server/internal/integrations/lark/notify_dm.go`
- Test: `server/internal/integrations/lark/notify_dm_test.go`

**Interfaces:**
- Consumes: `notify.DMDeliverer`, `notify.DeliverResult`, `notify.StateDelivered` (Task 3).
- Produces:
  - `lark.SendDirectParams{InstallationID InstallationCredentials, OpenID OpenID, Text string}`
  - `APIClient.SendDirectMessage(ctx context.Context, p SendDirectParams) (string, error)` — returns Lark's `message_id`
  - `lark.NewDMDeliverer(client APIClient, creds func(installationID pgtype.UUID) (InstallationCredentials, error), logger *slog.Logger) notify.DMDeliverer`

- [ ] **Step 1: Write the failing test**

`server/internal/integrations/lark/notify_dm_test.go`:

```go
package lark

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type recordingDMClient struct {
	APIClient // embedded so only the method under test needs implementing
	lastOpen  OpenID
	lastText  string
	messageID string
	err       error
}

func (c *recordingDMClient) SendDirectMessage(_ context.Context, p SendDirectParams) (string, error) {
	c.lastOpen, c.lastText = p.OpenID, p.Text
	return c.messageID, c.err
}

func testCreds(pgtype.UUID) (InstallationCredentials, error) {
	return InstallationCredentials{AppID: "cli_test"}, nil
}

// The message id is what makes the push replyable. Losing it here silently
// turns the whole reply half off for Lark.
func TestDeliverDMReturnsTheLarkMessageID(t *testing.T) {
	c := &recordingDMClient{messageID: "om_abc123"}
	d := NewDMDeliverer(c, testCreds, slog.Default())

	res, err := d.DeliverDM(context.Background(), notify.PushRef{},
		db.ChannelUserBinding{ChannelUserID: "ou_recipient"}, "**[in_review] Ship it**")
	if err != nil {
		t.Fatalf("DeliverDM: %v", err)
	}
	if res.State != notify.StateDelivered {
		t.Errorf("State = %v, want StateDelivered", res.State)
	}
	if res.MessageID != "om_abc123" {
		t.Errorf("MessageID = %q, want om_abc123", res.MessageID)
	}
	// A member binding's channel_user_id is an open_id, not a chat_id.
	if c.lastOpen != OpenID("ou_recipient") {
		t.Errorf("OpenID = %q, want ou_recipient", c.lastOpen)
	}
	if c.lastText == "" {
		t.Error("sent empty text")
	}
}

func TestDeliverDMPropagatesAnAPIError(t *testing.T) {
	c := &recordingDMClient{err: errors.New("lark refused")}
	d := NewDMDeliverer(c, testCreds, slog.Default())

	res, err := d.DeliverDM(context.Background(), notify.PushRef{},
		db.ChannelUserBinding{ChannelUserID: "ou_recipient"}, "text")
	if err == nil {
		t.Fatal("DeliverDM error = nil, want the API error")
	}
	if res.State != notify.StateUnsupported {
		t.Errorf("State = %v, want the zero state on failure", res.State)
	}
}

// A delivered-but-idless result would be recorded as replyable by nothing and
// silently drop the reply path; treat a missing id as a failed push.
func TestDeliverDMTreatsAMissingMessageIDAsAFailure(t *testing.T) {
	c := &recordingDMClient{messageID: ""}
	d := NewDMDeliverer(c, testCreds, slog.Default())

	_, err := d.DeliverDM(context.Background(), notify.PushRef{},
		db.ChannelUserBinding{ChannelUserID: "ou_recipient"}, "text")
	if err == nil {
		t.Fatal("DeliverDM error = nil, want an error when Lark returns no message_id")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `(cd server && go test ./internal/integrations/lark -run TestDeliverDM -count=1)`
Expected: FAIL — `undefined: NewDMDeliverer`, `undefined: SendDirectParams`.

- [ ] **Step 3: Add the client method**

In `server/internal/integrations/lark/client.go`, add to the `APIClient` interface, next to `SendTextMessage`:

```go
	// SendDirectMessage posts plain text straight to a user's open_id and
	// returns Lark's message_id.
	//
	// Distinct from SendTextMessage, which addresses a chat_id
	// (outboundMessageRequest sets receive_id_type=chat_id). An inbox push
	// targets the member's binding, whose channel_user_id is an open_id, and
	// it needs the message_id back so a reply to the push can be attributed
	// to the issue it was about.
	SendDirectMessage(ctx context.Context, p SendDirectParams) (string, error)
```

and the params struct next to `SendTextParams`:

```go
// SendDirectParams is the input shape for a 1:1 push to a bound member.
type SendDirectParams struct {
	InstallationID InstallationCredentials
	OpenID         OpenID
	Text           string
}
```

Add the `stubAPIClient` implementation alongside the others (~line 413):

```go
func (s *stubAPIClient) SendDirectMessage(ctx context.Context, p SendDirectParams) (string, error) {
	s.log.Warn("lark stub client: SendDirectMessage called", "open_id", string(p.OpenID))
	return "", ErrAPIClientNotConfigured
}
```

In `server/internal/integrations/lark/http_client.go`, implement it. Model the request on `SendBindingPromptCard` (~line 501) for the `receive_id_type=open_id` query, and on `SendInteractiveCard` (~line 345) for decoding `data.message_id` out of the response. Text content is JSON-encoded the same way `SendTextMessage` does it — reuse that encoding helper rather than hand-building `{"text":...}`.

- [ ] **Step 4: Write the adapter**

`server/internal/integrations/lark/notify_dm.go`:

```go
package lark

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CredentialsFunc resolves an installation's API credentials.
type CredentialsFunc func(installationID pgtype.UUID) (InstallationCredentials, error)

type dmDeliverer struct {
	client APIClient
	creds  CredentialsFunc
	logger *slog.Logger
}

// NewDMDeliverer adapts the Lark client to the shared push layer.
//
// Lark is the reference implementation of a replyable push: it hands back a
// per-message id on send and reports a per-message ReplyCtx on inbound, so a
// reply to a push can be traced to the issue it was about. WeCom does
// neither.
func NewDMDeliverer(client APIClient, creds CredentialsFunc, logger *slog.Logger) notify.DMDeliverer {
	if logger == nil {
		logger = slog.Default()
	}
	return &dmDeliverer{client: client, creds: creds, logger: logger}
}

func (d *dmDeliverer) DeliverDM(ctx context.Context, _ notify.PushRef, binding db.ChannelUserBinding, text string) (notify.DeliverResult, error) {
	if text == "" || binding.ChannelUserID == "" {
		return notify.DeliverResult{}, nil
	}
	creds, err := d.creds(binding.InstallationID)
	if err != nil {
		return notify.DeliverResult{}, err
	}
	messageID, err := d.client.SendDirectMessage(ctx, SendDirectParams{
		InstallationID: creds,
		OpenID:         OpenID(binding.ChannelUserID),
		Text:           text,
	})
	if err != nil {
		return notify.DeliverResult{}, err
	}
	if messageID == "" {
		// Without an id the push is unaddressable, so a reply to it would
		// fall through to the ordinary chat path and confuse the user.
		// Surfacing this as a failure keeps the metric honest.
		return notify.DeliverResult{}, errors.New("lark: send returned no message_id")
	}
	return notify.DeliverResult{State: notify.StateDelivered, MessageID: messageID}, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `(cd server && go test ./internal/integrations/lark -count=1)`
Expected: PASS. Other Lark tests may fail to compile because a test fake implements `APIClient` and is now missing `SendDirectMessage` — add the method to each fake. That is the interface change doing its job; do not weaken the interface to avoid it.

- [ ] **Step 6: Commit**

```bash
git add server/internal/integrations/lark/
git commit -m "feat(lark): deliver inbox pushes as replyable direct messages"
```

---

## Task 6: Record the reply and explicit review decision

This is the load-bearing half. Every accepted reply becomes a member comment and `triggerTasksForComment` wakes the assignee agent **in any status** (`issue_trigger.go`: "issue writes park on backlog while comments fire in any status"). In addition, an exact `审核通过` or `确认审核` reply against an `in_review`-category task is the replying human's explicit terminal decision: commit the comment and `done` transition together, publish the ordinary member-authored status event, and invoke `notifyParentOfChildDone`. Replies with any additional text remain comment-only so contextual stage instructions are not misread as a terminal decision.

**Why this does not refactor `CreateComment`.** The File Structure originally proposed extracting the create+publish+trigger sequence out of `CreateComment` (`comment.go:1687-1915`) and sharing it. Reading that function, the sequence is not separable: it is threaded with attachment linking, thread-root unresolve, an agent-only escalation cancel, and the `X-Task-ID` lineage stamp. A shared helper would need every one of those as a parameter and would end up a second signature of the same function.

The repo already answered this question once. `TaskService.createAgentComment` (`service/task.go:7174`) is a self-contained non-HTTP comment path that does its own `CreateComment` + `Bus.Publish(EventCommentCreated)` + `AutoUnresolveThreadOnReply`. Following that precedent is using the existing pattern, not adding a parallel abstraction. **Model the new method on `createAgentComment`, and read it before writing.**

**Authorization lives here, not in the router.** The router knows a ledger row matched; it does not know whether the sender is still a workspace member or whether the issue still exists. Both are checked in this method, and both fail closed with a message and no writes.

**Files:**
- Modify: `server/internal/integrations/channel/engine/resolvers.go` (the `PushReplyResult` type only; the interface and router wiring are Task 7)
- Create: `server/internal/handler/channel_push_reply.go`
- Test: `server/internal/handler/channel_push_reply_test.go`

**Interfaces:**
- Consumes: `db.ChannelPushMessage` (Task 1), `h.Queries`, `h.publish` (`handler.go:696`), `h.triggerTasksForComment` (`comment.go:1953`).
- Produces:
  - `engine.PushReplyResult{Posted bool, Message string}`
  - `(*Handler).PostPushReplyComment(ctx context.Context, push db.ChannelPushMessage, senderUserID pgtype.UUID, content string) (engine.PushReplyResult, error)`

  The type lives in `engine`, not `handler`, because the dependency runs one way: `handler` already imports `engine` (`handler.go:27`) so that `*Handler` can satisfy `engine.ChannelChatLifecycle`. `engine` importing `handler` back would be a cycle. Task 7 declares the interface this method satisfies; the signature must match exactly. `Message` is what the adapter echoes back in IM; `Posted=false` with a non-empty `Message` is the denial case.

- [ ] **Step 1: Add the result type to `engine`**

In `server/internal/integrations/channel/engine/resolvers.go`, next to the other seam types:

```go
// PushReplyResult is the verdict on a reply to an inbox push, produced by the
// server side and echoed back to the sender in IM.
//
// A denial is a result, not an error: the sender is a real person who typed
// something and is owed an answer. The error return is reserved for faults the
// user cannot act on.
type PushReplyResult struct {
	Posted  bool
	Message string
}
```

- [ ] **Step 2: Write the failing test**

`server/internal/handler/channel_push_reply_test.go`:

```go
package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/testutil/dbfx"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The happy path: the person the push was addressed to replies, and the reply
// becomes a member-authored comment on the issue the push was about.
func TestPostPushReplyCommentCreatesAMemberComment(t *testing.T) {
	h, ctx := newTestHandler(t), context.Background()
	ws, user := dbfx.Workspace(t, h.Queries), dbfx.User(t, h.Queries)
	dbfx.Member(t, h.Queries, ws.ID, user.ID)
	issue := dbfx.Issue(t, h.Queries, dbfx.IssueOpts{WorkspaceID: ws.ID, Status: "in_review"})

	push := db.ChannelPushMessage{
		WorkspaceID:     ws.ID,
		RecipientUserID: user.ID,
		IssueID:         issue.ID,
	}

	res, err := h.PostPushReplyComment(ctx, push, user.ID, "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if !res.Posted {
		t.Fatalf("Posted = false, message = %q", res.Message)
	}

	comments, err := h.Queries.ListCommentsByIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("ListCommentsByIssue: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}
	if comments[0].AuthorType != "member" {
		t.Errorf("AuthorType = %q, want member", comments[0].AuthorType)
	}
	// The comment must be attributed to the human who replied — an agent
	// woken by it inherits this identity as the originator for its own
	// invocation checks.
	if uuidToString(comments[0].AuthorID) != uuidToString(user.ID) {
		t.Errorf("AuthorID = %v, want the replying user", comments[0].AuthorID)
	}
	if comments[0].Content != "确认审核" {
		t.Errorf("Content = %q", comments[0].Content)
	}
	// "comment", never a machine type: this is a human speaking.
	if comments[0].Type != "comment" {
		t.Errorf("Type = %q, want comment", comments[0].Type)
	}
}

// Someone else in the same workspace replying to a push addressed to another
// person must not be able to speak as them.
func TestPostPushReplyCommentRejectsAnotherUser(t *testing.T) {
	h, ctx := newTestHandler(t), context.Background()
	ws := dbfx.Workspace(t, h.Queries)
	owner, other := dbfx.User(t, h.Queries), dbfx.User(t, h.Queries)
	dbfx.Member(t, h.Queries, ws.ID, owner.ID)
	dbfx.Member(t, h.Queries, ws.ID, other.ID)
	issue := dbfx.Issue(t, h.Queries, dbfx.IssueOpts{WorkspaceID: ws.ID})

	push := db.ChannelPushMessage{WorkspaceID: ws.ID, RecipientUserID: owner.ID, IssueID: issue.ID}

	res, err := h.PostPushReplyComment(ctx, push, other.ID, "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if res.Posted {
		t.Fatal("Posted = true; a non-recipient must not be able to reply")
	}
	if res.Message == "" {
		t.Error("denial carried no message; the user would see silence")
	}
	assertNoComments(t, h, issue.ID)
}

// A ledger row outlives membership. Removal from the workspace must revoke
// the reply path even though the row still points at the issue.
func TestPostPushReplyCommentRejectsANonMember(t *testing.T) {
	h, ctx := newTestHandler(t), context.Background()
	ws, user := dbfx.Workspace(t, h.Queries), dbfx.User(t, h.Queries)
	issue := dbfx.Issue(t, h.Queries, dbfx.IssueOpts{WorkspaceID: ws.ID})
	// deliberately no dbfx.Member

	push := db.ChannelPushMessage{WorkspaceID: ws.ID, RecipientUserID: user.ID, IssueID: issue.ID}

	res, _ := h.PostPushReplyComment(ctx, push, user.ID, "确认审核")
	if res.Posted {
		t.Fatal("Posted = true for a user who is no longer a member")
	}
	assertNoComments(t, h, issue.ID)
}

// quick_create_failed pushes have no issue, so there is nowhere to inject.
// The user gets told rather than ignored.
func TestPostPushReplyCommentWithoutAnIssueExplainsItself(t *testing.T) {
	h, ctx := newTestHandler(t), context.Background()
	ws, user := dbfx.Workspace(t, h.Queries), dbfx.User(t, h.Queries)
	dbfx.Member(t, h.Queries, ws.ID, user.ID)

	push := db.ChannelPushMessage{
		WorkspaceID:     ws.ID,
		RecipientUserID: user.ID,
		IssueID:         pgtype.UUID{}, // NULL
	}

	res, err := h.PostPushReplyComment(ctx, push, user.ID, "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if res.Posted {
		t.Fatal("Posted = true with no issue to post to")
	}
	if res.Message == "" {
		t.Error("no explanation for an unreplyable push")
	}
}

// An issue deleted between push and reply must not 500 the inbound pipeline.
func TestPostPushReplyCommentOnAMissingIssue(t *testing.T) {
	h, ctx := newTestHandler(t), context.Background()
	ws, user := dbfx.Workspace(t, h.Queries), dbfx.User(t, h.Queries)
	dbfx.Member(t, h.Queries, ws.ID, user.ID)

	push := db.ChannelPushMessage{
		WorkspaceID:     ws.ID,
		RecipientUserID: user.ID,
		IssueID:         parseUUID("00000000-0000-7000-8000-000000000001"),
	}

	res, err := h.PostPushReplyComment(ctx, push, user.ID, "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment returned err %v; want a denial result", err)
	}
	if res.Posted {
		t.Fatal("Posted = true for a missing issue")
	}
}

// Empty content (an IM sticker or an image-only reply) is not a decision.
func TestPostPushReplyCommentRejectsEmptyContent(t *testing.T) {
	h, ctx := newTestHandler(t), context.Background()
	ws, user := dbfx.Workspace(t, h.Queries), dbfx.User(t, h.Queries)
	dbfx.Member(t, h.Queries, ws.ID, user.ID)
	issue := dbfx.Issue(t, h.Queries, dbfx.IssueOpts{WorkspaceID: ws.ID})

	push := db.ChannelPushMessage{WorkspaceID: ws.ID, RecipientUserID: user.ID, IssueID: issue.ID}

	res, _ := h.PostPushReplyComment(ctx, push, user.ID, "   ")
	if res.Posted {
		t.Fatal("Posted = true for whitespace-only content")
	}
	assertNoComments(t, h, issue.ID)
}

func assertNoComments(t *testing.T, h *Handler, issueID pgtype.UUID) {
	t.Helper()
	comments, err := h.Queries.ListCommentsByIssue(context.Background(), issueID)
	if err != nil {
		t.Fatalf("ListCommentsByIssue: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("got %d comments, want none — a denied reply must not write", len(comments))
	}
}

var _ = testutil.Call // keep the import if the file grows an HTTP case
```

**Before writing this file, check the actual fixture names.** `dbfx.Workspace` / `dbfx.User` / `dbfx.Member` / `dbfx.IssueOpts` and `newTestHandler` are written here as the shapes this test needs; the repo's real names and signatures win. Read `server/internal/testutil/dbfx` and one existing `internal/handler/*_test.go` first and adjust. Likewise confirm the query name for listing an issue's comments — the test needs any read that proves zero or one row.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `(cd server && go test ./internal/handler -run TestPostPushReply -count=1)`
Expected: FAIL — `h.PostPushReplyComment undefined`.

Requires the checkout database. If this errors on connection rather than compilation, run `make migrate-up` first.

- [ ] **Step 4: Write the implementation**

`server/internal/handler/channel_push_reply.go`:

```go
package handler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/pkg/dbid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// PostPushReplyComment turns a reply-to-a-push into an issue comment and, for
// a narrow explicit approval, records the human-owned review decision.
//
// Exact "审核通过" / "确认审核" replies move an in-review issue to done as the
// replying member. Contextual replies remain comment-only.
//
// Modeled on TaskService.createAgentComment, the other non-HTTP comment path.
func (h *Handler) PostPushReplyComment(
	ctx context.Context,
	push db.ChannelPushMessage,
	senderUserID pgtype.UUID,
	content string,
) (engine.PushReplyResult, error) {
	content = sanitizeNullBytes(strings.TrimSpace(content))
	if content == "" {
		return engine.PushReplyResult{Message: "回复内容为空，未提交。"}, nil
	}

	// The push was addressed to one person. Anyone else replying to it —
	// possible in a shared IM context, or after a forwarded message — must not
	// be able to author a decision under that person's name.
	if uuidToString(senderUserID) != uuidToString(push.RecipientUserID) {
		return engine.PushReplyResult{Message: "你没有权限回复这条推送。"}, nil
	}

	// A ledger row outlives membership; re-check rather than trusting it.
	if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      senderUserID,
		WorkspaceID: push.WorkspaceID,
	}); err != nil {
		return engine.PushReplyResult{Message: "你没有权限回复这条推送。"}, nil
	}

	// quick_create pushes carry no issue: there is no thread to inject into.
	if !push.IssueID.Valid {
		return engine.PushReplyResult{Message: "这条推送不能直接回复决策，请打开 Multica 处理。"}, nil
	}

	issue, err := h.Queries.GetIssue(ctx, push.IssueID)
	if err != nil {
		return engine.PushReplyResult{Message: "关联的 issue 已不存在。"}, nil
	}
	if uuidToString(issue.WorkspaceID) != uuidToString(push.WorkspaceID) {
		return engine.PushReplyResult{Message: "你没有权限回复这条推送。"}, nil
	}

	created, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    senderUserID,
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		return engine.PushReplyResult{}, err
	}
	comment := created.Comment()

	actorID := uuidToString(senderUserID)
	resp := commentToResponse(comment, nil, nil)
	resp.IssueRevision = created.IssueRevision
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "member", actorID, map[string]any{
		"comment":             resp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
		"issue_revision":      created.IssueRevision,
	})

	// The wake. originatorUserID is the replying human, so the agent this
	// starts inherits exactly that person's invocation authority — the same
	// value CreateComment passes for a member author
	// (invokeOriginatorFromRequest returns actorID unchanged for "member").
	//
	// Two recipients each approving produces two comments; the second is
	// folded into the pending task by mergeCommentIntoPendingTask rather than
	// starting a second run. Do not add a second guard here.
	h.triggerTasksForComment(ctx, issue, comment, nil, "member", actorID, actorID, nil)

	slog.Info("push reply posted as comment",
		"issue_id", uuidToString(issue.ID),
		"comment_id", uuidToString(comment.ID),
		"channel_type", push.ChannelType,
	)
	return engine.PushReplyResult{Posted: true, Message: "已记录审核意见，Multica 会结合任务上下文继续处理。"}, nil
}
```

Two things to verify while writing, because the plan asserts them from a read rather than a compile:

1. `commentToResponse(comment, nil, nil)` — confirm the second and third parameters accept nil (in `CreateComment` they are a nil reactions arg and a grouped-attachment map lookup). If attachments are `[]X` rather than a map value, nil is still correct: a push reply never has any.
2. `triggerTasksForComment`'s last parameter is `suppressAgentIDs []pgtype.UUID`; nil means suppress nothing.

The test file must import `engine` too, and compare against `engine.PushReplyResult`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `(cd server && go test ./internal/handler -run TestPostPushReply -count=1 -v)`
Expected: PASS, six tests.

- [ ] **Step 6: Confirm nothing else regressed in the package**

Run: `(cd server && go test ./internal/handler ./internal/integrations/channel/engine -count=1)`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add server/internal/integrations/channel/engine/resolvers.go server/internal/handler/channel_push_reply.go server/internal/handler/channel_push_reply_test.go
git commit -m "feat(server): post an IM push reply as an issue comment"
```

---

## Task 7: Route a reply-to-a-push out of the chat pipeline

`InboundMessage.ReplyTo *ReplyCtx{MessageID, RootID}` (`channel/message.go:109-117`) is already the cross-adapter normalization of "you replied to this message". No new adapter capability is needed — only a lookup and a branch.

**Where the branch goes, and why exactly there.** `processClaimed` (`router.go:334`) runs numbered steps. The pre-check goes **after step 4** and **before steps 5-6**:

- After step 4 (`set.Identity.ResolveSender`) because the authorization check needs the Multica user id, and because an unbound sender should still get the binding card rather than a permission denial.
- Before steps 5-6 (session resolve / start) because a push reply must not create or append to a `chat_session`. Its meaning was fixed by the push it answers; running it through the session pipeline would also parse `/issue` out of it, which the spec forbids.

The group-mention filter at step 3 stays ahead of it: pushes are DMs, so a group message can never match a ledger row anyway, and keeping the order means no message skips the audit path.

**Two outcomes, not one.** `OutcomePushReply` and `OutcomePushReplyDenied` render identically (both post `Result.PushReplyText`), but they are separate so the outcome metric can tell a working loop from a permissions problem. This mirrors how `OutcomeIssueUsage` is distinct from `OutcomeIngested` despite both being replies.

**Not a drop.** A denied reply is *not* routed through `r.drop`. `r.drop` returns `OutcomeDropped`, and every replier's switch ignores that outcome — the user would get silence, which contradicts the spec's 「回复你没有权限」.

**Files:**
- Modify: `server/internal/integrations/channel/engine/resolvers.go`
- Modify: `server/internal/integrations/channel/engine/router.go`
- Test: `server/internal/integrations/channel/engine/router_push_reply_test.go`

**Interfaces:**
- Consumes: `engine.PushReplyResult` (Task 6), `db.ChannelPushMessage` and `FindChannelPushMessage` (Task 1).
- Produces:
  - `engine.OutcomePushReply`, `engine.OutcomePushReplyDenied`
  - `Result.PushReplyText string`
  - ```go
    type PushReplyPoster interface {
        LookupPush(ctx context.Context, installationID pgtype.UUID, channelMessageID string) (db.ChannelPushMessage, bool, error)
        PostPushReplyComment(ctx context.Context, push db.ChannelPushMessage, senderUserID pgtype.UUID, content string) (PushReplyResult, error)
    }
    ```
  - `RouterConfig.PushReplies PushReplyPoster` (nil-safe: nil means the feature is off and every message takes the old path)

  Task 8 satisfies `LookupPush` on `*Handler` with a thin wrapper over `FindChannelPushMessage` that maps `pgx.ErrNoRows` to `(_, false, nil)`.

- [ ] **Step 1: Write the failing test**

`server/internal/integrations/channel/engine/router_push_reply_test.go`:

```go
package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakePushReplies struct {
	byMessageID map[string]db.ChannelPushMessage
	posted      []string
	result      PushReplyResult
	lookupErr   error
}

func (f *fakePushReplies) LookupPush(_ context.Context, _ pgtype.UUID, id string) (db.ChannelPushMessage, bool, error) {
	if f.lookupErr != nil {
		return db.ChannelPushMessage{}, false, f.lookupErr
	}
	row, ok := f.byMessageID[id]
	return row, ok, nil
}

func (f *fakePushReplies) PostPushReplyComment(_ context.Context, _ db.ChannelPushMessage, _ pgtype.UUID, content string) (PushReplyResult, error) {
	f.posted = append(f.posted, content)
	return f.result, nil
}

// The core routing claim: a reply to a push leaves the chat pipeline.
func TestPushReplyDoesNotTouchTheChatPipeline(t *testing.T) {
	h := newRouterHarness(t) // existing harness in router_test.go
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"om_push_1": {}},
		result:      PushReplyResult{Posted: true, Message: "已记录"},
	}
	h.cfg.PushReplies = f

	msg := h.p2pMessage("确认审核")
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	res := h.handle(t, msg)

	if res.Outcome != OutcomePushReply {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, OutcomePushReply)
	}
	if res.PushReplyText != "已记录" {
		t.Errorf("PushReplyText = %q", res.PushReplyText)
	}
	if len(f.posted) != 1 || f.posted[0] != "确认审核" {
		t.Errorf("posted = %v, want one entry with the reply text", f.posted)
	}
	// The two things the spec forbids on this path.
	if h.sessions.ensureCalls != 0 || h.sessions.startCalls != 0 {
		t.Errorf("session resolution ran: ensure=%d start=%d", h.sessions.ensureCalls, h.sessions.startCalls)
	}
	if h.issues.createCalls != 0 {
		t.Error("issue creation ran; /issue must not be parsed on this path")
	}
}

// A "/issue Foo" typed as a reply to a push is a decision, not a command.
func TestPushReplyDoesNotParseIssueCommand(t *testing.T) {
	h := newRouterHarness(t)
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"om_push_1": {}},
		result:      PushReplyResult{Posted: true, Message: "已记录"},
	}
	h.cfg.PushReplies = f

	msg := h.p2pMessage("/issue 顺便再建一个")
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	res := h.handle(t, msg)

	if res.Outcome != OutcomePushReply {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, OutcomePushReply)
	}
	if h.issues.createCalls != 0 {
		t.Error("an /issue command was parsed out of a push reply")
	}
}

// Slack reports only a thread-level id, so RootID is the fallback key.
func TestPushReplyFallsBackToRootID(t *testing.T) {
	h := newRouterHarness(t)
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"root_1": {}},
		result:      PushReplyResult{Posted: true, Message: "已记录"},
	}
	h.cfg.PushReplies = f

	msg := h.p2pMessage("确认审核")
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "not_a_push", RootID: "root_1"}

	if res := h.handle(t, msg); res.Outcome != OutcomePushReply {
		t.Fatalf("Outcome = %q, want the RootID fallback to hit", res.Outcome)
	}
}

// A reply to an ordinary agent message must behave exactly as before.
func TestReplyToANonPushTakesTheChatPath(t *testing.T) {
	h := newRouterHarness(t)
	h.cfg.PushReplies = &fakePushReplies{byMessageID: map[string]db.ChannelPushMessage{}}

	msg := h.p2pMessage("再补充一句")
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_ordinary"}

	res := h.handle(t, msg)
	if res.Outcome == OutcomePushReply || res.Outcome == OutcomePushReplyDenied {
		t.Fatalf("Outcome = %q; a non-push reply must stay on the chat path", res.Outcome)
	}
	if h.sessions.ensureCalls == 0 {
		t.Error("chat path did not run")
	}
}

// No ReplyTo at all: the lookup must not even be attempted.
func TestNoReplyToSkipsTheLookup(t *testing.T) {
	h := newRouterHarness(t)
	f := &fakePushReplies{lookupErr: errors.New("LookupPush must not be called")}
	h.cfg.PushReplies = f

	if res := h.handle(t, h.p2pMessage("你好")); res.Outcome == OutcomePushReply {
		t.Fatal("a message with no ReplyTo entered the push path")
	}
}

// A denial still reaches the user; silence would look like the bot is broken.
func TestPushReplyDenialRepliesAndWritesNothing(t *testing.T) {
	h := newRouterHarness(t)
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"om_push_1": {}},
		result:      PushReplyResult{Posted: false, Message: "你没有权限回复这条推送。"},
	}
	h.cfg.PushReplies = f

	msg := h.p2pMessage("确认审核")
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	res := h.handle(t, msg)
	if res.Outcome != OutcomePushReplyDenied {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, OutcomePushReplyDenied)
	}
	if res.PushReplyText == "" {
		t.Error("denial carried no text; the user would see silence")
	}
	if h.sessions.ensureCalls != 0 {
		t.Error("a denied reply still ran the chat path")
	}
}

// The feature must be inert until Task 8 wires it.
func TestNilPushRepliesLeavesTheOldPathUntouched(t *testing.T) {
	h := newRouterHarness(t)
	h.cfg.PushReplies = nil

	msg := h.p2pMessage("确认审核")
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if res := h.handle(t, msg); res.Outcome == OutcomePushReply {
		t.Fatal("push path ran with no poster configured")
	}
}
```

**`newRouterHarness`, `h.p2pMessage`, `h.handle`, `h.sessions.ensureCalls`, `h.issues.createCalls` are placeholders for the existing test scaffolding in `router_test.go`** (see `fakeChannelChatLifecycle` at `router_test.go:426` and the harness struct around `:488-513`). Read that file and use the real names and counters; if the fakes do not already count calls, add counters to them rather than inventing a parallel harness.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `(cd server && go test ./internal/integrations/channel/engine -run 'PushReply|NoReplyTo' -count=1)`
Expected: FAIL — `undefined: OutcomePushReply`, `cfg.PushReplies undefined`.

- [ ] **Step 3: Add the outcomes, the result field, and the interface**

In `server/internal/integrations/channel/engine/resolvers.go`, extend the `Outcome` block:

```go
	// OutcomePushReply — the message replied to an inbox push and was
	// injected as an issue comment. It never touched a chat session.
	OutcomePushReply Outcome = "push_reply"
	// OutcomePushReplyDenied — the message replied to a known push but the
	// sender was not the person it was addressed to, or the push had no
	// issue to reply into. Nothing was written; the sender is told why.
	OutcomePushReplyDenied Outcome = "push_reply_denied"
```

Add to `Result`, below `IssueUsageHadMedia`:

```go
	// PushReplyText is the message to echo back for the two push-reply
	// outcomes. Carried on Result rather than derived from the Outcome
	// because the reason for a denial is decided server-side, and every
	// replier must render the same words.
	PushReplyText string
```

Add the interface next to `ChannelChatLifecycle` (`resolvers.go:182`), and note in its doc comment why it is a sibling rather than a new method on that interface:

```go
// PushReplyPoster resolves a reply-to-an-inbox-push and injects it as an
// issue comment.
//
// Deliberately a sibling of ChannelChatLifecycle, not a fourth method on it:
// that interface's three methods are all chat-session lifecycle, and an issue
// comment is not part of a chat session's life. *Handler implements both.
type PushReplyPoster interface {
	// LookupPush finds the ledger row for a platform message id. A miss is
	// (_, false, nil), not an error: most replies are ordinary chat.
	LookupPush(ctx context.Context, installationID pgtype.UUID, channelMessageID string) (db.ChannelPushMessage, bool, error)
	PostPushReplyComment(ctx context.Context, push db.ChannelPushMessage, senderUserID pgtype.UUID, content string) (PushReplyResult, error)
}
```

- [ ] **Step 4: Wire it into the Router**

In `server/internal/integrations/channel/engine/router.go`, add to `RouterConfig` (after `Lifecycle`, ~line 79):

```go
	// PushReplies handles replies to inbox pushes. Nil disables the path
	// entirely: every message takes the ordinary chat route.
	PushReplies PushReplyPoster
```

Add the matching field to the `Router` struct (next to `lifecycle`, ~line 42) and copy it in `NewRouter` (~line 105):

```go
	pushReplies  PushReplyPoster
```
```go
		pushReplies:  cfg.PushReplies,
```

In `processClaimed`, insert between step 4 and the `// 5-6.` comment:

```go
	// 4b. Inbox-push reply. A reply whose target is a push we sent is a
	// decision on that push's issue, not chat. It short-circuits here —
	// before session resolution — because its meaning is already fixed by
	// the message it answers: it must not open or extend a chat_session,
	// and ParseIssueCommand must never see it.
	if r.pushReplies != nil && msg.ReplyTo != nil {
		if res, handled, err := r.handlePushReply(ctx, inst, msg, identity); err != nil {
			return Result{}, finalizeRelease, err
		} else if handled {
			return res, finalizeMark, nil
		}
	}
```

and add the helper near `drop` (~line 1046):

```go
// handlePushReply reports handled=false when the reply targets something that
// is not one of our pushes, which is the common case.
func (r *Router) handlePushReply(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage, identity ResolvedIdentity) (Result, bool, error) {
	push, ok, err := r.pushReplies.LookupPush(ctx, inst.ID, msg.ReplyTo.MessageID)
	if err != nil {
		return Result{}, false, fmt.Errorf("lookup push: %w", err)
	}
	if !ok && msg.ReplyTo.RootID != "" && msg.ReplyTo.RootID != msg.ReplyTo.MessageID {
		// Slack reports only a thread-level ts on inbound, and a DM push
		// starts its own thread, so the root is the push itself.
		push, ok, err = r.pushReplies.LookupPush(ctx, inst.ID, msg.ReplyTo.RootID)
		if err != nil {
			return Result{}, false, fmt.Errorf("lookup push root: %w", err)
		}
	}
	if !ok {
		return Result{}, false, nil
	}

	reply, err := r.pushReplies.PostPushReplyComment(ctx, push, identity.UserID, msg.CommandText)
	if err != nil {
		return Result{}, false, fmt.Errorf("post push reply: %w", err)
	}
	outcome := OutcomePushReplyDenied
	if reply.Posted {
		outcome = OutcomePushReply
	}
	return Result{
		Outcome:        outcome,
		InstallationID: inst.ID,
		Sender:         msg.Source.SenderID,
		IssueID:        push.IssueID,
		PushReplyText:  reply.Message,
	}, true, nil
}
```

Check which field of `InboundMessage` holds the user's text on this path. `processClaimed` uses `msg.CommandText` for command parsing; if the raw text lives elsewhere (e.g. a separate `Text`), use the raw one — a push reply is prose, and any command-stripping applied to `CommandText` would silently mangle it.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `(cd server && go test ./internal/integrations/channel/engine -count=1)`
Expected: PASS, including the pre-existing router suite. The `Result` struct gained a field and `RouterConfig` gained a nil-defaulted one, so nothing existing should change behavior.

- [ ] **Step 6: Render the outcome in every replier**

Each adapter has its own `Reply` switch on `res.Outcome`. Add one case to each, posting `res.PushReplyText`:

- `server/internal/integrations/lark/outcome_replier.go` (note `:55` also lists the outcomes that warrant a reply at all — add both there too)
- `server/internal/integrations/slack/replier.go:139`
- `server/internal/integrations/telegram/replier.go:125`
- `server/internal/integrations/wecom/replier.go:129`
- `server/internal/integrations/dingtalk/replier.go:136`

Pattern, adapted to each file's existing style:

```go
	case engine.OutcomePushReply, engine.OutcomePushReplyDenied:
		if err := r.postResult(ctx, inst, msg, res, res.PushReplyText); err != nil {
			r.logger.WarnContext(ctx, "slack replier: push reply ack failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
```

WeCom and DingTalk can never reach these outcomes (neither populates `ReplyTo`), but add the case anyway — omitting it would encode "this platform is special" in five places instead of the one place that already says it, the capability matrix in the spec.

- [ ] **Step 7: Run the adapter tests**

Run: `(cd server && go test ./internal/integrations/... -count=1)`
Expected: PASS. Each replier package has a table test keyed on outcome (e.g. `slack/replier_test.go:135`); add a row for `OutcomePushReply` asserting the text comes from `res.PushReplyText`.

- [ ] **Step 8: Commit**

```bash
git add server/internal/integrations/
git commit -m "feat(server): route replies to inbox pushes out of the chat pipeline"
```

---

## Task 8: Wire it up, and prove the Lark loop end to end

Everything so far is inert: the notifier has no adapters and no bus, and `RouterConfig.PushReplies` is nil. This task turns it on and adds the one end-to-end test the spec asks for.

**Where each piece goes in `cmd/server/router.go`:**

- The `notify.Notifier` is built **unconditionally**, next to `channelRouter` (~line 519), for the same stated reason: it is platform-agnostic, and a deployment with only Slack configured has no Lark master key. With no adapters registered it is a no-op.
- Each adapter registers itself inside its own existing `if <platform> configured` block — Lark near `channelRouter.Register(channel.TypeFeishu, ...)` (~line 669), WeCom near `wecomOutbound.Register(bus)` (~line 1015).
- `notifier.Subscribe(bus)` runs **after** all the platform blocks, so registration is complete before the first event can arrive.

**Files:**
- Modify: `server/cmd/server/router.go`
- Modify: `server/internal/handler/channel_push_reply.go` (add `LookupPush`)
- Test: `server/internal/handler/channel_push_reply_e2e_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–7.
- Produces: `(*Handler).LookupPush(ctx context.Context, installationID pgtype.UUID, channelMessageID string) (db.ChannelPushMessage, bool, error)`, completing `engine.PushReplyPoster` on `*Handler`.

- [ ] **Step 1: Write the failing end-to-end test**

Lark is the only platform where the whole loop closes (see the capability matrix in the spec), so this test lives with the handler and drives the two halves directly rather than through a live socket.

`server/internal/handler/channel_push_reply_e2e_test.go`:

```go
package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The full loop the design exists to deliver: an in_review inbox item becomes
// a DM, the DM's message id is recorded, a reply to that id resolves back to
// the issue, and the resulting comment fires the trigger that wakes the agent.
//
// Split across packages in production; asserted here in one place because no
// single package sees both ends.
func TestLarkPushReplyRoundTrip(t *testing.T) {
	h, ctx := newTestHandler(t), context.Background()
	ws, user := dbfx.Workspace(t, h.Queries), dbfx.User(t, h.Queries)
	dbfx.Member(t, h.Queries, ws.ID, user.ID)
	agent := dbfx.Agent(t, h.Queries, ws.ID)
	issue := dbfx.Issue(t, h.Queries, dbfx.IssueOpts{
		WorkspaceID:  ws.ID,
		Status:       "in_review",
		AssigneeType: "agent",
		AssigneeID:   agent.ID,
	})
	inst := dbfx.ChannelInstallation(t, h.Queries, ws.ID, "feishu")
	dbfx.ChannelUserBinding(t, h.Queries, inst.ID, ws.ID, user.ID, "ou_recipient")

	// --- push half -------------------------------------------------------
	fake := &recordingDeliverer{messageID: "om_push_1"}
	n := notify.New(h.Queries, nil)
	n.Register(map[string]notify.DMDeliverer{"feishu": fake})

	n.HandleInboxNew(inboxNewEvent(t, h, ws.ID, user.ID, issue.ID, "status_changed", "in_review"))

	if fake.calls != 1 {
		t.Fatalf("adapter called %d times, want 1", fake.calls)
	}

	// --- the ledger row is the hinge -------------------------------------
	push, ok, err := h.LookupPush(ctx, inst.ID, "om_push_1")
	if err != nil {
		t.Fatalf("LookupPush: %v", err)
	}
	if !ok {
		t.Fatal("no ledger row: the push was sent but can never be replied to")
	}
	if uuidToString(push.IssueID) != uuidToString(issue.ID) {
		t.Fatalf("ledger points at %v, want issue %v", push.IssueID, issue.ID)
	}

	// --- reply half ------------------------------------------------------
	res, err := h.PostPushReplyComment(ctx, push, user.ID, "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if !res.Posted {
		t.Fatalf("Posted = false: %q", res.Message)
	}

	// --- the wake --------------------------------------------------------
	// The comment trigger is the mechanism the whole design leans on; assert
	// it fired rather than trusting the comment row alone.
	tasks, err := h.Queries.ListAgentTasksByIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("ListAgentTasksByIssue: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("no agent task enqueued: the reply landed but nothing woke up")
	}
}

type recordingDeliverer struct {
	calls     int
	messageID string
}

func (d *recordingDeliverer) DeliverDM(context.Context, notify.PushRef, db.ChannelUserBinding, string) (notify.DeliverResult, error) {
	d.calls++
	return notify.DeliverResult{State: notify.StateDelivered, MessageID: d.messageID}, nil
}
```

Three things to resolve while writing this, all by reading rather than guessing:

1. `inboxNewEvent` is a helper you write in this file. It must build the **real** `EventInboxNew` payload, not a hand-shaped map: create an inbox row through the real path if one exists, or copy the exact shape `inboxItemToResponse` produces plus the separately-injected `issue_status` key. A test that invents the payload shape proves nothing about the production one — the payload has no typed struct, so this is the only place the shape gets checked.
2. `dbfx.Agent` / `dbfx.ChannelInstallation` / `dbfx.ChannelUserBinding` / `ListAgentTasksByIssue` are the shapes needed; use the repo's real fixture and query names.
3. The task-queue assertion must survive an agent that cannot actually run. If enqueueing depends on a live daemon, assert on whatever `triggerTasksForComment` returns (`[]CommentTriggerOutcome`) instead — the point is that the trigger ran and chose to wake the assignee, not that a process started.

- [ ] **Step 2: Run it to verify it fails**

Run: `(cd server && go test ./internal/handler -run TestLarkPushReplyRoundTrip -count=1)`
Expected: FAIL — `h.LookupPush undefined`.

- [ ] **Step 3: Add `LookupPush`**

Append to `server/internal/handler/channel_push_reply.go`:

```go
// LookupPush finds the push a reply is answering. A miss is not an error:
// nearly every inbound reply is ordinary chat, and the router uses the bool
// to fall through to that path.
func (h *Handler) LookupPush(ctx context.Context, installationID pgtype.UUID, channelMessageID string) (db.ChannelPushMessage, bool, error) {
	if channelMessageID == "" {
		return db.ChannelPushMessage{}, false, nil
	}
	row, err := h.Queries.FindChannelPushMessage(ctx, db.FindChannelPushMessageParams{
		InstallationID:   installationID,
		ChannelMessageID: channelMessageID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelPushMessage{}, false, nil
	}
	if err != nil {
		return db.ChannelPushMessage{}, false, err
	}
	return row, true, nil
}
```

Add a compile-time assertion at the bottom of the file so a future signature drift fails the build rather than silently disabling the feature:

```go
var _ engine.PushReplyPoster = (*Handler)(nil)
```

- [ ] **Step 4: Run the end-to-end test**

Run: `(cd server && go test ./internal/handler -run TestLarkPushReplyRoundTrip -count=1 -v)`
Expected: PASS.

- [ ] **Step 5: Build the notifier in the composition root**

In `server/cmd/server/router.go`, after the `channelRouter` block (~line 522), add:

```go
	// Inbox-push notifier (this design): forwards the narrow whitelist of
	// inbox items that mean "a human is needed now" to the recipient's IM
	// DM. Built unconditionally for the same reason channelRouter is —
	// it is platform-agnostic, and adapters register into it below. With no
	// adapters registered it does nothing.
	pushNotifier := notify.New(queries, slog.Default())
	pushAdapters := map[string]notify.DMDeliverer{}
```

Pass the poster into the router config in the same statement that already passes `Lifecycle`:

```go
	channelRouter := engine.NewRouter(h.IssueService, h.TaskService, queries, engine.RouterConfig{
		Logger: slog.Default(), Lifecycle: h, PushReplies: h,
	})
```

- [ ] **Step 6: Register the two adapters**

In the Lark block, next to `channelRouter.Register(channel.TypeFeishu, ...)` (~line 669):

```go
				pushAdapters[string(channel.TypeFeishu)] = lark.NewDMDeliverer(
					larkClient,
					installSvc.Credentials, // whatever the block already uses to resolve creds
					slog.Default(),
				)
```

In the WeCom block, next to `wecomOutbound.Register(bus)` (~line 1015):

```go
				pushAdapters[string(wecom.TypeWecom)] = wecomOutbound
```

`wecomOutbound` already satisfies `notify.DMDeliverer` after Task 4. Confirm the credentials accessor named above actually exists on `installSvc`; if Lark resolves credentials some other way in that block, use that.

- [ ] **Step 7: Subscribe after all platform blocks**

Below the last platform block, before the router returns:

```go
	// Subscribe last: registration must be complete before the first
	// EventInboxNew can be dispatched. The bus is synchronous, so an event
	// arriving during startup would otherwise find an empty adapter map and
	// silently drop the push.
	pushNotifier.Register(pushAdapters)
	pushNotifier.Subscribe(bus)
```

- [ ] **Step 8: Give the ledger a retention sweep**

Task 1 added `DeleteExpiredChannelPushMessages` and it has no caller yet. Without one the table only grows, and a query with no caller is dead code. The retention window is short by nature: a reply arriving days after the push is not a decision anyone is waiting on, and the app inbox remains the durable record.

Create `server/cmd/server/channel_push_sweeper.go`, modeled on `source_context_sweeper.go`:

```go
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// channelPushRetention bounds how long a push stays replyable. A reply
	// arriving a week later is not a decision anyone is waiting on, and the
	// app inbox is the durable record either way.
	channelPushRetention = 7 * 24 * time.Hour
	// Hourly: the table is small and nothing downstream is latency-sensitive.
	channelPushSweepInterval = time.Hour
)

type channelPushExpirer interface {
	DeleteExpiredChannelPushMessages(ctx context.Context, before pgtype.Timestamptz) (int64, error)
}

func runChannelPushSweeper(ctx context.Context, q channelPushExpirer) {
	ticker := time.NewTicker(channelPushSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := pgtype.Timestamptz{Time: time.Now().Add(-channelPushRetention), Valid: true}
			n, err := q.DeleteExpiredChannelPushMessages(ctx, cutoff)
			if err != nil {
				slog.Warn("channel push sweep failed", "error", err)
				continue
			}
			if n > 0 {
				slog.Info("channel push sweep", "deleted", n)
			}
		}
	}
}
```

Start it in `main.go` next to `go runSourceContextSweeper(sweepCtx, taskSvc)` (~line 657):

```go
	go runChannelPushSweeper(sweepCtx, queries)
```

Add one test in `server/cmd/server/channel_push_sweeper_test.go` asserting the loop returns when its context is cancelled — mirror `TestSourceContextSweeperStopsWithItsContext` (`source_context_sweeper_test.go:81`). The delete itself is Task 1's `TestChannelPushMessageRoundTrip` territory; do not re-test it here.

- [ ] **Step 9: Verify WeCom did not end up with two push paths**

Task 4 deleted WeCom's own `EventInboxNew` subscription. Prove it stayed deleted now that the shared one is live:

```bash
(cd server && grep -rn "EventInboxNew" internal/ cmd/)
```

Expected: the only `Subscribe`-side occurrence is in `internal/integrations/channel/notify/`. Publishers in `cmd/server/notification_listeners.go` are expected and unchanged.

- [ ] **Step 10: Full verification**

```bash
make test
```

Expected: PASS. This is the first point where the whole design is on, so run the full suite rather than a package.

- [ ] **Step 11: Commit**

```bash
git add server/cmd/server/ server/internal/handler/
git commit -m "feat(server): enable IM review pushes and decision replies"
```

---

## Done means

- An issue moving to `in_review` (or a custom status in that category) DMs the inbox recipient on Lark or WeCom, if they are bound and not muted.
- An issue moving to `blocked` (or a custom status in that category) sends a replyable DM with explicit blocked-state guidance.
- A Lark reply to that DM becomes a member comment on the issue and wakes the assignee agent.
- An exact `审核通过` or `确认审核` reply moves an `in_review`-category task to `done` as the replying member, including the standard activity, inbox, and parent-child completion side effects.
- Contextual approval text and requested changes remain comment-only and never auto-transition the task.
- A WeCom user receives the push and can act on it in the app; replying in WeCom behaves exactly as it did before.
- Nothing else about inbox behavior changed, except that WeCom users now receive the whitelist instead of every inbox row.
