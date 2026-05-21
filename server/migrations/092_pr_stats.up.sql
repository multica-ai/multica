-- PR diff stats (additions / deletions / changed_files) for the card layout.
-- Source: top-level `pull_request` object on every pull_request webhook event
-- (opened / synchronize / edited / labeled / ...). NOT NULL DEFAULT 0 means
-- legacy rows that pre-date this migration read as zero, which the frontend
-- detects via `total === 0` and hides the entire stats row — so the card never
-- renders a misleading "+0 −0 · 0 files" caption for rows that just haven't
-- been refreshed by a webhook yet.

ALTER TABLE github_pull_request
    ADD COLUMN additions     INT NOT NULL DEFAULT 0,
    ADD COLUMN deletions     INT NOT NULL DEFAULT 0,
    ADD COLUMN changed_files INT NOT NULL DEFAULT 0;
