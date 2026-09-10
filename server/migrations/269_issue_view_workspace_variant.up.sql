-- Workspace views gain an optional scope_variant mirroring the built-in
-- Members / Agents tabs — the same "which tab" concept my-views already
-- persist. NULL means the unrestricted All tab; a set value narrows the
-- view's scope to that assignee type (injected as scope-level
-- assignee_types at query time). Project views share the same optional
-- vocabulary: the project pages carry the same Members / Agents tabs.
ALTER TABLE issue_view
    DROP CONSTRAINT issue_view_scope_variant_check,
    DROP CONSTRAINT issue_view_check1;

ALTER TABLE issue_view
    ADD CONSTRAINT issue_view_scope_variant_check CHECK (
        scope_variant IN ('assigned', 'created', 'involved', 'any', 'members', 'agents')
    ),
    ADD CONSTRAINT issue_view_scope_variant_pairing CHECK (
        (scope_type = 'my' AND scope_variant IN ('assigned', 'created', 'involved', 'any'))
        OR (scope_type = 'workspace' AND (scope_variant IS NULL OR scope_variant IN ('members', 'agents')))
        OR (scope_type = 'project' AND (scope_variant IS NULL OR scope_variant IN ('members', 'agents')))
    );
