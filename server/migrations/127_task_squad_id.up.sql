-- agent_task_queue.squad_id records the squad a leader-task belongs to. The
-- daemon uses it at claim time to locate the squad whose briefing (Operating
-- Protocol + Roster + Instructions) should be injected onto the leader agent's
-- instructions, instead of inferring the squad by reverse-looking-up
-- "which squad is this agent the leader of" (which is ambiguous when one
-- agent leads multiple squads).
--
-- No FK to squad(id) on purpose: agent_task_queue is a hot, high-write task
-- queue, and we don't want squad maintenance (archive / hard-delete) to take
-- cross-table locks against it. If a squad is hard-deleted and a stale UUID
-- lingers here, the daemon's GetSquadInWorkspace lookup simply returns no row
-- and the claim path skips injection (err != nil branch) — exactly the same
-- observable behavior as "injection condition not matched". No stale briefing
-- is ever emitted.
ALTER TABLE agent_task_queue
    ADD COLUMN squad_id UUID NULL;

-- Partial index over leader-task rows only: small, high hit-rate. It serves
-- admin / debug queries ("which leader tasks is this squad currently
-- running"). The daemon claim path does NOT use it — that goes through the
-- task_id primary-key path.
CREATE INDEX agent_task_queue_squad_id_idx
    ON agent_task_queue (squad_id)
    WHERE squad_id IS NOT NULL;
