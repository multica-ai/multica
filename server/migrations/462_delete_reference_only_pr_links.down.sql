-- Irreversible: the deleted rows carried no information beyond "this PR body
-- mentioned this key", which the PR body itself still records. Recreating them
-- would resurrect links that no read path is meant to return.
SELECT 1;
