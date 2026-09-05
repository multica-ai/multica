-- Which task-token templates this agent has enabled: a JSON array of template
-- ids drawn from the server-configured catalog (MULTICA_TASK_TOKEN_TEMPLATES).
-- Ids only — no claims and no key material. The catalog is server
-- configuration; the UI may pick from it but never define what may be signed.
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS task_token_templates JSONB NOT NULL DEFAULT '[]'::jsonb;
