-- name: AddIssueSubscriber :execrows
-- Auto-subscribe path (creator / assignee / commenter / mentioned / autopilot /
-- delegated).
--
-- Two behaviors are load-bearing here:
--
--  1. A tombstoned row is NEVER resurrected. The WHERE on the DO UPDATE fails
--     for an opted-out row, which degrades to DO NOTHING — so unsubscribe still
--     sticks on a tree an agent keeps adding to (MUL-5483).
--
--  2. An ACTIVE 'delegated' row is upgraded when the user becomes directly
--     involved (assigned / mentioned / commented). Without this the reason
--     stays 'delegated' forever and someone who is now a real participant keeps
--     getting the reduced delivery tier.
--
-- Returns rows affected so the caller only broadcasts subscriber:added when an
-- active subscription actually changed. Publishing unconditionally made the
-- frontend insert a subscriber the DB had refused to write.
INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issue_id, user_type, user_id) DO UPDATE
SET reason = EXCLUDED.reason
WHERE issue_subscriber.unsubscribed_at IS NULL
  AND issue_subscriber.reason = 'delegated'
  AND EXCLUDED.reason <> 'delegated';

-- name: LockSubscriberWrites :exec
-- Transaction-scoped serialization boundary for (workspace, user) subscriber
-- state (MUL-5483 review round 7).
--
-- Three paths race over the same question — "should this user be an active
-- watcher?" — and each one is a check in one statement followed by a write in
-- another:
--
--   * the delegated rule: is the user a member / not opted out, then insert;
--   * subtree unsubscribe: tombstone the tree, which must cover children the
--     agent files a moment later;
--   * member revoke: delete the member row and their subscriptions.
--
-- Row locks cannot close this. The interleaving that breaks the escape hatch
-- inserts a subscriber for an issue that did not exist when the opt-out was
-- written, so there is no row to lock — a phantom, which under READ COMMITTED
-- needs an explicit lock object. Every path above takes THIS lock first, so
-- their lock ordering is identical and they cannot deadlock against each other.
--
-- Keyed on (workspace, user) rather than the issue: an opt-out is about a
-- person and a tree, and the tree's membership is exactly what is changing.
--
-- The key is derived from the UUID VALUE, not from however the caller happened
-- to spell it. ::uuid parses the input and the outer ::text renders PostgreSQL's
-- canonical lowercase form, so 'AB…' and 'ab…' — the same UUID everywhere else
-- in this file, and to every membership query — also produce the same lock.
-- Hashing the raw string instead let an uppercase request take a DIFFERENT lock
-- from the canonical holder, which silently reopened the very race this lock
-- exists to close. Casting at the DB boundary keeps that guarantee even if a
-- future caller forgets to canonicalize.
SELECT pg_advisory_xact_lock(
    hashtext((sqlc.arg(workspace_id)::uuid)::text),
    hashtext((sqlc.arg(user_id)::uuid)::text)
);

-- name: AddDelegatedSubscriber :execrows
-- The delegated rule's write (MUL-5483). Eligibility and insert are ONE
-- statement so they share a snapshot; callers must hold LockSubscriberWrites
-- for the same (workspace, user), which is what makes that snapshot stable.
--
-- Eligibility is two conditions the previous version checked in separate
-- round trips:
--
--   1. active_member — the originator must STILL be a workspace member.
--      originator_user_id is stamped when the task is queued and never
--      revisited, so a revoked member's still-running tasks would otherwise
--      keep filing ghost subscribers. FOR SHARE also blocks a concurrent
--      DELETE of that member row for the rest of this transaction, so the
--      check cannot go stale between here and the insert.
--
--   2. NOT EXISTS (...) — the ancestor opt-out walk from HasAncestorOptOut,
--      inlined for the same reason: a subtree tombstone committed between a
--      separate check and this insert would silently undo the user's opt-out
--      on every child the agent files next.
--
-- ON CONFLICT keeps AddIssueSubscriber's two rules: never resurrect a
-- tombstone, and upgrade an active 'delegated' row when the user becomes
-- directly involved.
WITH RECURSIVE ancestors(node_id, parent_id, depth) AS (
    SELECT root.id, root.parent_issue_id, 0 FROM issue root WHERE root.id = $1
    UNION ALL
    SELECT i.id, i.parent_issue_id, a.depth + 1
    FROM issue i JOIN ancestors a ON i.id = a.parent_id
),
active_member AS (
    SELECT m.user_id FROM member m
    WHERE m.user_id = $2 AND m.workspace_id = $4
    FOR SHARE
)
INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
SELECT $1, 'member', am.user_id, $3
FROM active_member am
WHERE NOT EXISTS (
    SELECT 1
    FROM issue_subscriber s
    JOIN ancestors a ON a.node_id = s.issue_id
    WHERE s.user_type = 'member' AND s.user_id = $2
      AND s.unsubscribed_at IS NOT NULL
      AND (a.depth = 0 OR s.opt_out_scope = 'subtree')
)
ON CONFLICT (issue_id, user_type, user_id) DO UPDATE
SET reason = EXCLUDED.reason
WHERE issue_subscriber.unsubscribed_at IS NULL
  AND issue_subscriber.reason = 'delegated'
  AND EXCLUDED.reason <> 'delegated';

-- name: LockActiveMember :one
-- Re-assert workspace membership INSIDE a serialized transaction, holding the
-- row so a concurrent revoke cannot complete underneath the caller
-- (MUL-5483 review round 8).
--
-- The subtree-unsubscribe handler validates membership from its own MVCC
-- snapshot before it opens a transaction, which is only a statement about the
-- past. A revoke that commits in between clears the user's subscriptions and
-- deletes the member row, and the handler would then write a fresh tombstone
-- behind it — leaving an opt-out that outlives the membership and is inherited
-- if the person is ever re-invited.
SELECT id FROM member
WHERE user_id = $1 AND workspace_id = $2
FOR SHARE;

-- name: DeleteSubscriptionsByMember :exec
-- Drop a departing member's subscriptions across the workspace, in the same tx
-- as the member-row delete (MUL-5483 review round 7).
--
-- Same application-layer cleanup rule the surrounding revoke path already
-- applies to channel bindings and invocation grants: issue_subscriber carries
-- no FK, so nothing removes these implicitly. Without it a revoked member keeps
-- accruing inbox rows, and re-inviting them silently restores visibility of
-- every issue they used to watch.
--
-- A hard DELETE, not a tombstone: a tombstone means "this person chose to
-- leave this issue" and would suppress re-subscription if they rejoin, which
-- is not what a workspace revoke means.
DELETE FROM issue_subscriber s
USING issue i
WHERE s.issue_id = i.id
  AND i.workspace_id = $1
  AND s.user_type = 'member'
  AND s.user_id = $2;

-- name: SubscribeToIssueExplicitly :exec
-- Explicit user action (the Subscribe button). Unlike the rule-driven path this
-- CLEARS an existing opt-out tombstone and its scope: the user is overriding
-- their own earlier unsubscribe, which is the one thing that should bring them
-- back.
INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issue_id, user_type, user_id)
DO UPDATE SET unsubscribed_at = NULL, opt_out_scope = NULL, reason = EXCLUDED.reason;

-- name: RemoveIssueSubscriber :exec
-- Leave THIS issue only. Tombstone rather than delete, so auto-subscribe rules
-- can tell "never subscribed" (no row) from "chose to leave" (row with
-- unsubscribed_at). scope='issue' keeps the opt-out from reaching descendants:
-- future children of this issue are still allowed to subscribe the user.
UPDATE issue_subscriber
SET unsubscribed_at = now(), opt_out_scope = 'issue'
WHERE issue_id = $1 AND user_type = $2 AND user_id = $3 AND unsubscribed_at IS NULL;

-- name: UnsubscribeFromIssueSubtree :many
-- Leave an issue AND every descendant in one action. An agent-built tree is
-- the unit a user actually wants to stop watching; leaving 30 sub-issues one
-- at a time is not a real escape hatch.
--
-- The root gets an UPSERTED tombstone rather than a conditional UPDATE, because
-- "I don't want this tree" has to persist even when the user holds no
-- subscription on the root itself — otherwise the opt-out records nothing and
-- the next child the agent files re-subscribes them. That root tombstone is
-- also what covers FUTURE descendants: HasAncestorOptOut walks up to it and
-- honors it because its scope is 'subtree'.
--
-- Returns every issue id it actually tombstoned so the caller can broadcast one
-- subscriber:removed per issue; publishing only the root left other open tabs
-- showing a stale subscription on the children.
WITH RECURSIVE subtree(node_id) AS (
    SELECT root.id FROM issue root WHERE root.id = $1
    UNION ALL
    SELECT i.id FROM issue i JOIN subtree s ON i.parent_issue_id = s.node_id
),
retire_descendants AS (
    UPDATE issue_subscriber sub
    SET unsubscribed_at = now(), opt_out_scope = 'subtree'
    WHERE sub.issue_id IN (SELECT node_id FROM subtree WHERE node_id <> $1)
      AND sub.user_type = $2 AND sub.user_id = $3
      AND sub.unsubscribed_at IS NULL
    RETURNING sub.issue_id
),
retire_root AS (
    INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason, unsubscribed_at, opt_out_scope)
    VALUES ($1, $2, $3, 'manual', now(), 'subtree')
    ON CONFLICT (issue_id, user_type, user_id)
    DO UPDATE SET unsubscribed_at = now(), opt_out_scope = 'subtree'
    RETURNING issue_id
)
SELECT issue_id FROM retire_descendants
UNION ALL
SELECT issue_id FROM retire_root;

-- name: HasAncestorOptOut :one
-- True when the user has opted out in a way that should keep them off THIS
-- issue.
--
-- Two distinct cases, and the distinction is the whole point of opt_out_scope:
--
--   * a tombstone on this issue itself, at any scope — they left this issue;
--   * a 'subtree'-scoped tombstone on a STRICT ancestor — they left a tree this
--     issue belongs to, so a child created an hour later is still covered.
--
-- An 'issue'-scoped tombstone on an ancestor deliberately does NOT match: the
-- user declined that one issue, not everything the agent files beneath it.
WITH RECURSIVE ancestors(node_id, parent_id, depth) AS (
    SELECT root.id, root.parent_issue_id, 0 FROM issue root WHERE root.id = $1
    UNION ALL
    SELECT i.id, i.parent_issue_id, a.depth + 1 FROM issue i JOIN ancestors a ON i.id = a.parent_id
)
SELECT EXISTS(
    SELECT 1
    FROM issue_subscriber s
    JOIN ancestors a ON a.node_id = s.issue_id
    WHERE s.user_type = $2 AND s.user_id = $3
      AND s.unsubscribed_at IS NOT NULL
      AND (a.depth = 0 OR s.opt_out_scope = 'subtree')
) AS opted_out;

-- name: ListIssueSubscribers :many
SELECT * FROM issue_subscriber
WHERE issue_id = $1 AND unsubscribed_at IS NULL
ORDER BY created_at;

-- name: IsIssueSubscriber :one
SELECT EXISTS(
    SELECT 1 FROM issue_subscriber
    WHERE issue_id = $1 AND user_type = $2 AND user_id = $3
      AND unsubscribed_at IS NULL
) AS subscribed;
