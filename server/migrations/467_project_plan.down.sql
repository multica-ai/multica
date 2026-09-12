-- Roll back the Plan Overview relational model.
--
-- Locks: ACCESS EXCLUSIVE on the six plan tables while they are dropped; no
-- existing project or issue rows are rewritten. Expected duration: <1 second
-- excluding lock-queue time.
--
-- Data loss: irreversible for all plan structure, mappings, source snapshots,
-- and dependency edges. Export those rows before using this migration after
-- production writes.

DROP TABLE IF EXISTS project_plan_dependency;
DROP TABLE IF EXISTS project_plan_part_issue;
DROP TABLE IF EXISTS project_plan_part;
DROP TABLE IF EXISTS project_plan_phase;
DROP TABLE IF EXISTS project_plan;
DROP TABLE IF EXISTS project_plan_kind;
