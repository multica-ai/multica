-- Records the durable directory that replaces a disposable task worktree
-- after the daemon has confirmed that worktree was finalized and removed.
-- NULL means no such handoff was reported, so clients must keep using work_dir.
--
-- Renumbered from 362: #7176 and #7182 both took prefix 362 before either
-- merged, and neither had shipped in a release when the collision was found.
-- The runner keys schema_migrations on the full stem, so a database that
-- already ran 362_agent_task_durable_work_dir re-runs this file under the new
-- stem; the DDL is idempotent, so that replay is a no-op. Keep it that way.
ALTER TABLE agent_task_queue
ADD COLUMN IF NOT EXISTS durable_work_dir TEXT;
