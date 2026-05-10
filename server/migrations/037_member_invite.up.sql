-- Add invite token to workspace for invite link feature
ALTER TABLE workspace ADD COLUMN invite_token TEXT UNIQUE;

-- Add invited_by tracking to member for transparency
ALTER TABLE member ADD COLUMN invited_by UUID REFERENCES "user"(id) ON DELETE SET NULL;
