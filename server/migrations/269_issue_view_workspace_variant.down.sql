-- Rolling back to the my-only variant vocabulary: workspace/project rows
-- that adopted the assignee-type variants must be normalized FIRST, or the
-- restored constraints reject the data and the rollback dies half-applied.
-- NULL is the correct downgrade — the previous version has no assignee-type
-- axis, so these views fall back to the unrestricted tab.
UPDATE issue_view
SET scope_variant = NULL
WHERE scope_type IN ('workspace', 'project')
  AND scope_variant IS NOT NULL;

ALTER TABLE issue_view
    DROP CONSTRAINT issue_view_scope_variant_check,
    DROP CONSTRAINT issue_view_scope_variant_pairing;

ALTER TABLE issue_view
    ADD CONSTRAINT issue_view_scope_variant_check CHECK (
        scope_variant IN ('assigned', 'created', 'involved', 'any')
    ),
    ADD CONSTRAINT issue_view_check1 CHECK (
        (scope_type = 'my' AND scope_variant IS NOT NULL)
        OR (scope_type <> 'my' AND scope_variant IS NULL)
    );
