-- A personal access token can be confined to one workspace. NULL keeps the
-- token account-wide, which is what every existing row is. No foreign key:
-- the workspace is validated when the token is minted, and the tokens are
-- removed in application code when the workspace is deleted or the owner
-- leaves it.
ALTER TABLE personal_access_token
    ADD COLUMN IF NOT EXISTS workspace_id UUID;
