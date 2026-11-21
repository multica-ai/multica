-- Workspace Markdown documents and immutable snapshots. Keys attach after concurrent builds.
CREATE TABLE IF NOT EXISTS doc_folder (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    parent_id UUID,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS doc (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    folder_id UUID,
    title TEXT NOT NULL DEFAULT '' CHECK (char_length(title) <= 300),
    content TEXT NOT NULL DEFAULT '',
    revision BIGINT NOT NULL DEFAULT 1,
    version_count INTEGER NOT NULL DEFAULT 1,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    updated_by_type TEXT NOT NULL CHECK (updated_by_type IN ('member', 'agent')),
    updated_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS doc_version (
    doc_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    number INTEGER NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    author_type TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id UUID NOT NULL,
    message TEXT NOT NULL DEFAULT '' CHECK (char_length(message) <= 500),
    action TEXT NOT NULL DEFAULT 'edit' CHECK (action IN ('create', 'edit', 'restore')),
    action_ref INTEGER,
    task_id UUID,
    issue_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
