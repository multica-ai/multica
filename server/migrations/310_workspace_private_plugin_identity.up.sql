-- Workspace ownership is part of private Plugin identity, while official
-- identities remain global. Relationships stay application-owned: no FK.
ALTER TABLE plugin_identity
    ADD COLUMN owner_workspace_id UUID,
    ADD CONSTRAINT plugin_identity_scope_check CHECK (
        (publisher_type = 'private_dev' AND trust_tier = 'private_dev' AND owner_workspace_id IS NOT NULL)
        OR
        (publisher_type <> 'private_dev' AND trust_tier <> 'private_dev' AND owner_workspace_id IS NULL)
    );
