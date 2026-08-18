-- Visibility promotion is an irreversible data correction. Re-hiding rows on
-- rollback would remove legitimate working PRs from issue views.
SELECT 1;
