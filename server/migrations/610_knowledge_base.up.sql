-- Knowledge base (MUL-8370): workspace docs written by members and agents.
--
-- doc_folder   nested folders; parent_id NULL means the workspace root. A
--              folder is only a container — it has no body.
-- doc          the live document. `revision` is the optimistic-concurrency
--              counter: every content write bumps it, and writers must send
--              the revision they read (stale writes get 409). `version_count`
--              is the number of the newest doc_version row.
-- doc_version  full snapshot per version, with author and an optional
--              message. Consecutive edits by one member within a short window
--              update the newest row in place (updated_at moves), so the
--              history reads as editing sessions instead of keystrokes.
--              Restore and revert append a new version; history is never
--              rewritten.
-- doc_read     when a member last opened a doc, for the unread feed.
-- project_doc  many-to-many link between projects and docs.
--
-- No foreign keys by project rule; workspace teardown and doc/project
-- deletes sweep the child rows in application code. Primary keys are
-- attached in 612-620 from indexes built CONCURRENTLY.
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
    format TEXT NOT NULL DEFAULT 'markdown' CHECK (format IN ('markdown', 'html')),
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
    action TEXT NOT NULL DEFAULT 'edit' CHECK (action IN ('create', 'edit', 'restore', 'revert')),
    action_ref INTEGER,
    task_id UUID,
    issue_id UUID,
    lines_added INTEGER NOT NULL DEFAULT 0,
    lines_removed INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS doc_read (
    doc_id UUID NOT NULL,
    user_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS project_doc (
    project_id UUID NOT NULL,
    doc_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
