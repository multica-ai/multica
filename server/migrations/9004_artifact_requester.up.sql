-- Requester: the user who asked for the document. Distinct from author —
-- when an agent creates an artifact at a user's request, author is the agent
-- and requester is the user who prompted them. Member-authored artifacts can
-- leave this null (the author IS the requester).

ALTER TABLE artifact ADD COLUMN requester_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL;

CREATE INDEX idx_artifact_requester ON artifact(requester_user_id) WHERE requester_user_id IS NOT NULL;
