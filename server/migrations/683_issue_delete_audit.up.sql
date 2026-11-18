-- MOJ-2166 batch 2: remove destructive cascade from activity_log and persist
-- immutable issue deletion archives for all delete paths.
ALTER TABLE activity_log
    DROP CONSTRAINT IF EXISTS activity_log_issue_id_fkey;
ALTER TABLE activity_log
    ADD CONSTRAINT activity_log_issue_id_fkey
    FOREIGN KEY (issue_id) REFERENCES issue(id) ON DELETE SET NULL
    NOT VALID;
ALTER TABLE activity_log
    VALIDATE CONSTRAINT activity_log_issue_id_fkey;

ALTER TABLE activity_log
    ADD COLUMN IF NOT EXISTS issue_number INTEGER,
    ADD COLUMN IF NOT EXISTS issue_title TEXT;

-- Append-only deletion archive. It deliberately has no foreign keys to issue or
-- workspace so deletion of either cannot cascade the evidence away.
CREATE TABLE IF NOT EXISTS issue_archive (
    archive_id       BIGSERIAL PRIMARY KEY,
    issue_id         UUID        NOT NULL,
    issue_number     INTEGER     NOT NULL,
    workspace_id     UUID        NOT NULL,
    issue_row        JSONB       NOT NULL,
    actor_id         UUID,
    actor_type       TEXT        NOT NULL DEFAULT 'unknown',
    client_fingerprint TEXT,
    source_ip        INET,
    deleted_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    session_identity     TEXT        NOT NULL,
    effective_user     TEXT        NOT NULL,
    client_addr      INET,
    client_port      INTEGER,
    application_name TEXT,
    backend_pid      INTEGER,
    txid             BIGINT
);

CREATE INDEX IF NOT EXISTS issue_archive_workspace_number_idx
    ON issue_archive(workspace_id, issue_number);
CREATE INDEX IF NOT EXISTS issue_archive_deleted_at_idx
    ON issue_archive(deleted_at DESC);

-- Comment snapshots are keyed by archived issue, without a live-comment FK.
CREATE TABLE IF NOT EXISTS issue_archive_comment (
    archive_id  BIGSERIAL PRIMARY KEY,
    issue_id    UUID        NOT NULL,
    comment_row JSONB       NOT NULL,
    deleted_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS issue_archive_comment_issue_idx
    ON issue_archive_comment(issue_id);

CREATE OR REPLACE FUNCTION archive_issue_on_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    v_actor_id UUID;
    v_actor_type TEXT;
    v_fingerprint TEXT;
    v_source_ip INET;
BEGIN
    v_actor_id := nullif(current_setting('app.audit_actor_id', true), '')::uuid;
    v_actor_type := coalesce(nullif(current_setting('app.audit_actor_type', true), ''), 'unknown');
    v_fingerprint := nullif(current_setting('app.audit_client_fingerprint', true), '');
    v_source_ip := nullif(current_setting('app.audit_source_ip', true), '')::inet;

    INSERT INTO issue_archive (
        issue_id, issue_number, workspace_id, issue_row,
        actor_id, actor_type, client_fingerprint, source_ip,
        session_identity, effective_user, client_addr, client_port,
        application_name, backend_pid, txid
    ) VALUES (
        OLD.id, OLD.number, OLD.workspace_id, to_jsonb(OLD),
        v_actor_id, v_actor_type, v_fingerprint, v_source_ip,
        session_user, current_user, inet_client_addr(), inet_client_port(),
        current_setting('application_name', true), pg_backend_pid(), txid_current()
    );

    INSERT INTO issue_archive_comment (issue_id, comment_row)
    SELECT OLD.id, to_jsonb(c)
    FROM comment c
    WHERE c.issue_id = OLD.id;

    UPDATE activity_log
       SET issue_number = OLD.number,
           issue_title = OLD.title
     WHERE issue_id = OLD.id;

    RETURN OLD;
END;
$$;

DROP TRIGGER IF EXISTS trg_issue_archive_on_delete ON issue;
CREATE TRIGGER trg_issue_archive_on_delete
BEFORE DELETE ON issue
FOR EACH ROW
EXECUTE FUNCTION archive_issue_on_delete();

-- Make the evidence tables append-only. Retained archives survive the down
-- migration; only explicit owner-level trigger removal can rewrite them.
CREATE OR REPLACE FUNCTION prevent_issue_archive_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
        USING ERRCODE = '44000';
END;
$$;

DROP TRIGGER IF EXISTS trg_issue_archive_append_only ON issue_archive;
CREATE TRIGGER trg_issue_archive_append_only
BEFORE UPDATE OR DELETE ON issue_archive
FOR EACH ROW
EXECUTE FUNCTION prevent_issue_archive_mutation();

DROP TRIGGER IF EXISTS trg_issue_archive_truncate_only ON issue_archive;
CREATE TRIGGER trg_issue_archive_truncate_only
BEFORE TRUNCATE ON issue_archive
FOR EACH STATEMENT
EXECUTE FUNCTION prevent_issue_archive_mutation();

DROP TRIGGER IF EXISTS trg_issue_archive_comment_append_only ON issue_archive_comment;
CREATE TRIGGER trg_issue_archive_comment_append_only
BEFORE UPDATE OR DELETE ON issue_archive_comment
FOR EACH ROW
EXECUTE FUNCTION prevent_issue_archive_mutation();

DROP TRIGGER IF EXISTS trg_issue_archive_comment_truncate_only ON issue_archive_comment;
CREATE TRIGGER trg_issue_archive_comment_truncate_only
BEFORE TRUNCATE ON issue_archive_comment
FOR EACH STATEMENT
EXECUTE FUNCTION prevent_issue_archive_mutation();
