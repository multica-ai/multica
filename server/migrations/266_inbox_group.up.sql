-- Inbox v2, step 2: the entity the product has always operated on.
--
-- One row per (person, source). This is the "row" a user marks read, archives
-- or snoozes — until now it existed only as a fold each of the three clients
-- performed for itself, which is why unread counts, the archived view and the
-- jump target could all disagree with each other.
--
-- Member-only by construction. Agents receive work through the run queue; the
-- inbox_item rows written for them are dead rows nothing reads, and giving them
-- groups would put state in this table that no inbox will ever render. Both the
-- write path and the lazy migration filter on recipient_type = 'member'.
--
-- No foreign keys (repo rule): issue/workspace deletion, member removal and the
-- orphan reconciler clean up explicitly in application code.
--
-- The PRIMARY KEY is deliberately absent here. Per the project migration rules
-- it is added in two steps — CREATE UNIQUE INDEX CONCURRENTLY (267) then
-- ADD CONSTRAINT ... USING INDEX (268) — so the same file can never be the one
-- that takes a blocking lock on a table that may already be large by the time a
-- self-host instance upgrades.
CREATE TABLE IF NOT EXISTS inbox_group (
    id                UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    -- A user id, matching inbox_item.recipient_id. Members only.
    recipient_id      UUID NOT NULL,

    -- The group's identity, generalised from "issue_id". 'standalone' covers
    -- notifications with no durable parent (an autopilot pausing, a quick
    -- create that failed before an issue existed); project and chat are the
    -- sockets left for later. The state machine below never inspects these,
    -- which is what makes a new source a delivery-and-render change rather than
    -- a state change.
    source_kind       TEXT NOT NULL CHECK (source_kind IN ('issue', 'standalone')),
    -- NOT NULL for every kind. A nullable column here would defeat the identity
    -- index: Postgres treats NULLs as distinct, so one person could accumulate
    -- several groups for the same source and the counts would drift again.
    source_id         UUID NOT NULL,

    -- Representative-row pointers. These must support being recomputed
    -- DOWNWARD, not just advanced: event-level dismissal can retire the current
    -- representative, after which latest_* has to fall back to the newest
    -- surviving event and the unread state be recomputed from it.
    latest_seq        BIGINT NOT NULL DEFAULT 0,
    latest_event_id   UUID,
    latest_event_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Read cursor. Unread is a comparison, not a stored flag, so a new event is
    -- unread without anything writing "unread" anywhere and an event an older
    -- row already covered cannot come back as a ghost. Historical unread is
    -- expressed as read_through_seq = latest_seq - 1 rather than by occupying
    -- manual_unread, which belongs to the user alone.
    read_through_seq  BIGINT NOT NULL DEFAULT 0,
    -- Explicit user intent, outranking every automatic rule. Durable precisely
    -- because the old implementation kept it in a React ref, so it died on
    -- refresh and the user's own action was silently undone.
    manual_unread     BOOLEAN NOT NULL DEFAULT false,
    -- Advances on EVERY state change, unlike latest_seq which only tracks
    -- content. It is the only way to tell "the user re-opened this, having seen
    -- the manual unread" from "an automatic read issued before it and arriving
    -- after" — both carry the same observed_seq.
    state_version     BIGINT NOT NULL DEFAULT 0,

    archived_at       TIMESTAMPTZ,
    -- Reserved for snooze. A timestamp expires on its own, so the query side
    -- needs no sweeper to bring a group back.
    snoozed_until     TIMESTAMPTZ,
    -- Sort key, separate from latest_event_at because a snooze expiring brings a
    -- group back with no event to sort by; without this it would silently
    -- return below everything newer.
    surfaced_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
