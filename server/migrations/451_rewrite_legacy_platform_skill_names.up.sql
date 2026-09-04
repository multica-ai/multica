-- Merging the eight built-in platform skills into `multica-platform` retired
-- seven names, and they live on in user-persisted text. `agent.instructions`
-- and `squad.instructions` are both copied verbatim into a task brief, so an
-- agent is still told to load a skill this server no longer ships. A snapshot
-- taken before this migration found 275 agents across 148 workspaces naming at
-- least one, 48 of them having started a task in the preceding seven days.
--
-- Rewriting persisted user text needs a reason beyond tidiness. These names
-- were not typed by a person: nothing in this repo templates them into an
-- agent, so they arrived at runtime -- most plausibly an agent copying the
-- skill listing out of its own brief into the instructions of an agent it was
-- creating. The distribution supports that reading, since all seven names
-- appear rather than the one or two a hand-written pointer would use. So this
-- corrects platform-generated text, not a sentence someone composed.
--
-- The redirect-stub route is deliberately not extended to cover these.
-- A stub under builtin_skills_legacy answers for a daemon whose own brief
-- still names the old skill, and it retires on its own because the server
-- gates it on a daemon capability. Instructions carry no version, so a stub
-- serving them would have no retirement condition -- it would keep the stale
-- text working, which is what stops the text from ever being fixed.
--
-- Every retired name maps to the same target because each former skill is now
-- one domain inside `multica-platform`, named in that skill's routing table.
-- Instructions that referenced several of them therefore end up naming
-- `multica-platform` more than once. That is left alone: the pointer resolves
-- either way, and collapsing the repetition would mean editing the surrounding
-- sentence, which is the one thing a mechanical rewrite must not attempt.
--
-- The boundaries around the alternation keep the rewrite to whole slugs, so a
-- workspace skill that merely starts with a retired name (`multica-squads-qa`)
-- and the still-shipped `multica-working-on-issues` stub are both left intact.
-- `updated_at` moves with the row, matching every other write to these tables;
-- nothing reads it for cache invalidation or daemon sync.

UPDATE agent AS a
SET instructions = regexp_replace(
        a.instructions, pattern.re, 'multica-platform', 'g'
    ),
    updated_at = now()
FROM (
    SELECT '(?<![A-Za-z0-9-])multica-(autopilots|creating-agents|mentioning'
        || '|projects-and-resources|runtimes-and-repos|skill-importing'
        || '|squads)(?![A-Za-z0-9-])' AS re
) AS pattern
WHERE a.instructions ~ pattern.re;

-- Squad instructions reach a leader's brief through the same path and were
-- written by the same agents, so they carry the same stale names. They were
-- outside the original count, which only looked at `agent`.
UPDATE squad AS s
SET instructions = regexp_replace(
        s.instructions, pattern.re, 'multica-platform', 'g'
    ),
    updated_at = now()
FROM (
    SELECT '(?<![A-Za-z0-9-])multica-(autopilots|creating-agents|mentioning'
        || '|projects-and-resources|runtimes-and-repos|skill-importing'
        || '|squads)(?![A-Za-z0-9-])' AS re
) AS pattern
WHERE s.instructions ~ pattern.re;
