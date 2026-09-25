DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM issue_source_context)
       OR EXISTS (SELECT 1 FROM attachment WHERE source_context_id IS NOT NULL)
       OR EXISTS (SELECT 1 FROM issue_source_context_object_intent) THEN
        RAISE EXCEPTION USING
            MESSAGE = 'cannot roll back issue source context while captured data or stored objects still exist',
            HINT = 'Remove source-context captures and their stored objects through application cleanup, then retry the rollback.';
    END IF;
END
$$;

ALTER TABLE attachment DROP COLUMN IF EXISTS source_context_id;
DROP TABLE IF EXISTS issue_source_context_object_intent;
DROP TABLE IF EXISTS issue_source_context;
