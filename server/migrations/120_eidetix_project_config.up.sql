-- Per-project binding to a partner Eidetix knowledge graph. One row per
-- project. The token selects the graph on Eidetix's side; we store it
-- application-encrypted (secretbox/AES-256-GCM), so a DB dump leaks ciphertext
-- only. enabled is a soft switch so an operator can pause Eidetix for a project
-- without losing the token.
CREATE TABLE eidetix_project_config (
    project_id       UUID PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    endpoint_url     TEXT NOT NULL DEFAULT 'https://eidetix.nodeops.xyz/mcp/sse',
    -- Ciphertext of the Eidetix Bearer token. Application-layer secretbox.
    -- DB never sees plaintext.
    token_encrypted  BYTEA NOT NULL,
    -- Human label only ("Marketing" / "Support"). NEVER the token.
    graph_label      TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
