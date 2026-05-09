-- Enable pg_trgm for path pattern matching (gin_trgm_ops).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Main document table. Path is the human key.
CREATE TABLE workspace_document (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    path                TEXT NOT NULL,
    title               TEXT,
    description         TEXT,
    content             TEXT NOT NULL DEFAULT '',
    format              TEXT NOT NULL DEFAULT 'markdown',
    tags                TEXT[] NOT NULL DEFAULT '{}',
    pinned              BOOLEAN NOT NULL DEFAULT false,
    archived_at         TIMESTAMPTZ,
    current_revision_id UUID,
    created_by          UUID REFERENCES "user"(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, path)
);

CREATE INDEX idx_workspace_doc_workspace_active
    ON workspace_document(workspace_id) WHERE archived_at IS NULL;
CREATE INDEX idx_workspace_doc_pinned
    ON workspace_document(workspace_id) WHERE pinned = true AND archived_at IS NULL;
CREATE INDEX idx_workspace_doc_tags
    ON workspace_document USING gin(tags);
CREATE INDEX idx_workspace_doc_path_trgm
    ON workspace_document USING gin(path gin_trgm_ops);
CREATE INDEX idx_workspace_doc_content_fts
    ON workspace_document USING gin(to_tsvector('simple', content));

-- Append-only revision history.
CREATE TABLE workspace_document_revision (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id     UUID NOT NULL REFERENCES workspace_document(id) ON DELETE CASCADE,
    revision_number INT  NOT NULL,
    parent_revision UUID REFERENCES workspace_document_revision(id),
    title           TEXT,
    description     TEXT,
    content         TEXT NOT NULL DEFAULT '',
    tags            TEXT[] NOT NULL DEFAULT '{}',
    author_type     TEXT NOT NULL CHECK (author_type IN
        ('human','agent_foreground','agent_background','import')),
    author_id       UUID,
    task_id         UUID REFERENCES agent_task_queue(id),
    operation       TEXT NOT NULL CHECK (operation IN
        ('create','edit','rename','restore','tag','pin','archive')),
    change_summary  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, revision_number)
);

CREATE INDEX idx_workspace_doc_rev_doc
    ON workspace_document_revision(document_id, revision_number DESC);

-- Now that the revision table exists, add the FK on workspace_document.
ALTER TABLE workspace_document
    ADD CONSTRAINT fk_workspace_document_current_revision
    FOREIGN KEY (current_revision_id) REFERENCES workspace_document_revision(id);

-- Issue <-> document N:N linking.
CREATE TABLE issue_document_link (
    issue_id     UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    document_id  UUID NOT NULL REFERENCES workspace_document(id) ON DELETE CASCADE,
    link_type    TEXT NOT NULL CHECK (link_type IN ('referenced','produced','consumed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, document_id)
);

CREATE INDEX idx_issue_doc_link_issue ON issue_document_link(issue_id);
CREATE INDEX idx_issue_doc_link_document ON issue_document_link(document_id);

-- Workspace-level setting: do agents have write permission?
ALTER TABLE workspace
    ADD COLUMN documents_agent_write_mode TEXT NOT NULL DEFAULT 'allow'
    CHECK (documents_agent_write_mode IN ('allow','read_only_for_agents','disabled'));
