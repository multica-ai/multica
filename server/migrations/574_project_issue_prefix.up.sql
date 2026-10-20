-- Project-scoped issue numbering (SHAR-175).
--
-- A project may carry its own identifier prefix. Issues whose effective scope is
-- such a project are numbered by project.issue_counter and rendered as
-- "<project prefix>-<n>"; everything else keeps using the workspace counter.
ALTER TABLE project
    ADD COLUMN issue_prefix TEXT NOT NULL DEFAULT '',
    ADD COLUMN issue_counter BIGINT NOT NULL DEFAULT 0;
