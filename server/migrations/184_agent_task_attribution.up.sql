-- Human Attribution, Phase 1 foundation (MUL-4302, parent MUL-4274; upstream
-- discussion GH #5108). Every agent run must be traceable to exactly one
-- accountable human, and that attribution must be EXPLAINABLE: a report has to
-- state not just WHO but at WHICH waterfall level the human was resolved
-- (direct action, delegation copy, comment-source chain, autopilot rule owner,
-- or degraded owner fallback). Today agent_task_queue.originator_user_id records
-- the human but carries no provenance — a NULL originator could equally mean
-- "autopilot" or "we failed to resolve a human who exists", and the two must be
-- distinguishable for the compliance/coverage story.
--
-- This migration adds the provenance + lineage columns that the Attribution
-- Resolver stamps at enqueue time. It deliberately does NOT change the value or
-- semantics of originator_user_id, which feeds authorization decisions
-- (canInvokeAgent A2A gate, Composio overlay history) — per the design axiom
-- "attribution is on-behalf-of, never blame and never authorization"
-- (MUL-4302 §1.3). These columns are audit/visibility metadata only.
--
-- Constraints honored (MUL-4302 §7 + workspace DB rules): NO foreign keys, NO
-- cascades (integrity is resolved in the application layer), and NO CHECK
-- constraint on the source enum so that a newly-modeled trigger path can add a
-- source label without a schema migration. All columns are nullable with no
-- default so the ALTER is a fast metadata-only change on the hot queue table
-- (no table rewrite, no long lock).

-- originator_source: which waterfall level resolved originator_user_id for this
-- run. One of: 'direct_human', 'delegation', 'comment_source', 'rule_owner',
-- 'owner_fallback', 'backfill', 'unattributed'. TEXT with no CHECK on purpose
-- (see header). NULL only on rows that predate this migration.
ALTER TABLE agent_task_queue
    ADD COLUMN originator_source TEXT NULL;

COMMENT ON COLUMN agent_task_queue.originator_source IS
    'Waterfall level that resolved originator_user_id for this run: direct_human | delegation | comment_source | rule_owner | owner_fallback | backfill | unattributed. Audit/visibility metadata only — never consulted for authorization. TEXT with no CHECK so new trigger paths can add a source without a migration (MUL-4302 §7). NULL on pre-migration rows.';

-- delegated_from_task_id: for source='delegation', the parent task whose
-- accountable human was COPIED onto this run (agent A @-mentions agent B, or an
-- agent creates a sub-issue). The chain root stays stable and copying the value
-- (not walking the chain) keeps delegation cycles harmless (MUL-4302 §3.2).
-- No FK by design; a stale id degrades to "evidence not resolvable", never a
-- corrupt run.
ALTER TABLE agent_task_queue
    ADD COLUMN delegated_from_task_id UUID NULL;

COMMENT ON COLUMN agent_task_queue.delegated_from_task_id IS
    'For originator_source=delegation: the parent task whose accountable human was copied onto this run. Value is copied, not chained, so delegation cycles are harmless (MUL-4302 §3.2). No FK; app-layer integrity only.';

-- retry_of_task_id / rerun_of_task_id: the design requires retry and rerun to be
-- kept strictly apart in reporting (MUL-4302 §5). parent_task_id already links
-- system auto-retries, but it is overloaded and does not distinguish the two.
--   * retry_of_task_id — system transient-failure retry; INHERITS the parent's
--     attribution unchanged (not a new attribution event).
--   * rerun_of_task_id — human manual rerun of a historical task; a NEW
--     direct_human attribution to the person who clicked rerun, with lineage
--     preserved back to the original task.
-- Both no-FK, nullable.
ALTER TABLE agent_task_queue
    ADD COLUMN retry_of_task_id UUID NULL;

COMMENT ON COLUMN agent_task_queue.retry_of_task_id IS
    'System transient-failure retry lineage: the task this run re-attempts. Inherits the parent attribution unchanged. Kept distinct from rerun_of_task_id so retry vs rerun report separately (MUL-4302 §5). No FK.';

ALTER TABLE agent_task_queue
    ADD COLUMN rerun_of_task_id UUID NULL;

COMMENT ON COLUMN agent_task_queue.rerun_of_task_id IS
    'Human manual-rerun lineage: the historical task a member re-ran. The rerun itself is a NEW direct_human attribution to the rerunning member; this column preserves the link to the original (MUL-4302 §5). No FK.';

-- rule_version_id: for source='rule_owner', the published autopilot rule version
-- whose publisher is the accountable human. The rule-version snapshot table and
-- its wiring land in a later Phase 1 increment; the column ships now so the
-- resolver contract and the queue schema are stable. No FK, nullable.
ALTER TABLE agent_task_queue
    ADD COLUMN rule_version_id UUID NULL;

COMMENT ON COLUMN agent_task_queue.rule_version_id IS
    'For originator_source=rule_owner: the published autopilot rule version snapshot whose publisher is the accountable human. No FK; snapshot table wiring lands in a later Phase 1 increment (MUL-4302 §3.4/§7).';

-- trigger_evidence_kind / trigger_evidence_ref_id: the direct cause of the run,
-- so every attribution can jump to its evidence (which comment, which assign,
-- which rule version, which external event). kind is a free TEXT tag
-- ('comment' | 'issue_assignment' | 'autopilot_run' | 'rule_version' | 'rerun'
-- | ...); ref_id points at that row. trigger_comment_id / autopilot_run_id
-- still exist for their existing consumers — this pair is the uniform,
-- kind-tagged evidence handle the attribution UI will link from (MUL-4302 §2).
ALTER TABLE agent_task_queue
    ADD COLUMN trigger_evidence_kind TEXT NULL;

COMMENT ON COLUMN agent_task_queue.trigger_evidence_kind IS
    'Uniform kind tag for the direct cause of this run (comment | issue_assignment | autopilot_run | rule_version | rerun | ...), paired with trigger_evidence_ref_id. Free TEXT so new evidence kinds need no migration (MUL-4302 §2).';

ALTER TABLE agent_task_queue
    ADD COLUMN trigger_evidence_ref_id UUID NULL;

COMMENT ON COLUMN agent_task_queue.trigger_evidence_ref_id IS
    'The row id referenced by trigger_evidence_kind (a comment id, autopilot_run id, rule_version id, source task id, ...). No FK; resolvable per-kind in the app layer (MUL-4302 §2).';
