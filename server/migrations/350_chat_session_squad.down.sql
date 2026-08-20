ALTER TABLE chat_session
    DROP COLUMN squad_leader_revision,
    DROP COLUMN squad_id;

ALTER TABLE squad DROP COLUMN leader_revision;
