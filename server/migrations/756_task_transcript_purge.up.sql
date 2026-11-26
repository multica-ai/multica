-- A workspace admin can clear a finished run's log (MUL-10284): its transcript
-- rows are deleted and its final output dropped. These columns are the record
-- of that, and they keep the log empty: message ingest drops batches for a run
-- that has them. No foreign key on purged_by, so removing the admin later does
-- not touch the record.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS transcript_purged_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS transcript_purged_by UUID;
