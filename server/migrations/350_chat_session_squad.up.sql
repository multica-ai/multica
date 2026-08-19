ALTER TABLE squad
    ADD COLUMN leader_revision BIGINT NOT NULL DEFAULT 1;

ALTER TABLE chat_session
    ADD COLUMN squad_id UUID,
    ADD COLUMN squad_leader_revision BIGINT;
