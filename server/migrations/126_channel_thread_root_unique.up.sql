-- A thread is anchored to exactly one root message (the top-level message it
-- was opened on). Before this migration, the check-then-create pattern in
-- ReplyToMessage / move-converge could race: two concurrent first-replies on
-- the same root both missed GetThreadByRootMessage and each ran
-- CreateChannelThread, producing two thread rows rooted at one message —
-- splitting replies across threads and breaking issue linkage.
--
-- This migration makes the invariant enforceable: a UNIQUE partial index on
-- root_message_id so the second insert conflicts (used by
-- UpsertChannelThreadByRoot's ON CONFLICT). Partial (WHERE root_message_id IS
-- NOT NULL): legacy threads predating the column (migration 110) have NULL
-- roots and are exempt.
--
-- Existing duplicates would make CREATE UNIQUE INDEX fail, so first collapse
-- any pre-existing duplicates per root: keep the survivor with the most
-- messages (lowest id tiebreak), reparent the losers' messages + linked issues
-- onto it, then delete the losers. A no-op when there are no duplicates.
-- channel_message.thread_id is ON DELETE CASCADE and issue.source_thread_id is
-- ON DELETE SET NULL, so messages/issues MUST be reparented before the loser
-- threads are deleted. The loser→survivor map lives in a temp table so all
-- three statements see it (golang-migrate runs each file in one transaction).

CREATE TEMP TABLE tmp_thread_dedup AS
WITH dup_roots AS (
    SELECT root_message_id
    FROM channel_thread
    WHERE root_message_id IS NOT NULL
    GROUP BY root_message_id
    HAVING count(*) > 1
), ranked AS (
    SELECT t.id, t.root_message_id,
           ROW_NUMBER() OVER (
               PARTITION BY t.root_message_id
               ORDER BY t.message_count DESC, t.id ASC
           ) AS rn
    FROM channel_thread t
    JOIN dup_roots d ON d.root_message_id = t.root_message_id
)
SELECT l.id AS loser_id, s.id AS survivor_id
FROM ranked l
JOIN ranked s ON s.root_message_id = l.root_message_id AND s.rn = 1
WHERE l.rn > 1;

-- Reparent messages from each loser onto its survivor.
UPDATE channel_message m
SET thread_id = d.survivor_id
FROM tmp_thread_dedup d
WHERE m.thread_id = d.loser_id;

-- Reparent issues linked to a loser onto its survivor.
UPDATE issue i
SET source_thread_id = d.survivor_id
FROM tmp_thread_dedup d
WHERE i.source_thread_id = d.loser_id;

-- Drop the losers (no messages/issues reference them now, so neither the
-- channel_message ON DELETE CASCADE nor the issue ON DELETE SET NULL fires).
DELETE FROM channel_thread
WHERE id IN (SELECT loser_id FROM tmp_thread_dedup);

DROP TABLE tmp_thread_dedup;

CREATE UNIQUE INDEX idx_channel_thread_root_message_unique
    ON channel_thread(root_message_id)
    WHERE root_message_id IS NOT NULL;
