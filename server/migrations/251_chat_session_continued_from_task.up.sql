-- "Continue in chat": a chat session may be seeded from a finished agent task so
-- the conversation resumes that task's provider session and work_dir instead of
-- cold-starting. continued_from_task_id records which task it came from.
--
-- No foreign key, per the project convention that relationships are validated in
-- the application layer (see developers/conventions.zh.mdx) — and it suits this
-- column anyway: nothing downstream depends on the target row still existing. The
-- resume pointer itself was copied into session_id / work_dir at create time, so a
-- task pruned later leaves the chat fully functional and only loses its provenance
-- label. Readers must therefore tolerate a dangling id rather than assume a join
-- succeeds.
--
-- The uniqueness that makes the button idempotent is added separately in 252,
-- because a CONCURRENTLY index cannot share a migration file with another
-- statement (the runner sends each file as one simple query, which Postgres wraps
-- in an implicit transaction).
ALTER TABLE chat_session
  ADD COLUMN IF NOT EXISTS continued_from_task_id UUID;
