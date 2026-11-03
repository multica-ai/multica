-- The repo-docs sync (plan 10.12): a workspace's setting, and the files it has
-- synced. A removed file keeps its row with archived_at set, so its path and
-- last hash stay on record. Neither table has a primary key or a foreign key:
-- the unique indexes are in 772 and 773, and the workspace teardown sweeps them.
CREATE TABLE knowledge_repo_sync (
    workspace_id UUID NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    repo_url TEXT NOT NULL DEFAULT '' CHECK (char_length(repo_url) <= 2048),
    branch TEXT NOT NULL DEFAULT 'main' CHECK (char_length(branch) BETWEEN 1 AND 255),
    docs_path TEXT NOT NULL DEFAULT 'docs' CHECK (char_length(docs_path) BETWEEN 1 AND 255),
    kb_key TEXT NOT NULL DEFAULT 'engineering' CHECK (char_length(kb_key) BETWEEN 1 AND 64),
    last_synced_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 4000),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_repo_file (
    workspace_id UUID NOT NULL,
    path TEXT NOT NULL CHECK (char_length(path) BETWEEN 1 AND 1024),
    knowledge_link_id UUID,
    content_hash TEXT NOT NULL,
    archived_at TIMESTAMPTZ,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
