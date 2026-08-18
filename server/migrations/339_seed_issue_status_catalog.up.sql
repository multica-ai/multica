-- Seed the 7 built-in statuses for every EXISTING workspace (MUL-6243).
--
-- New workspaces are seeded inside their create transaction; this covers the
-- ones that already exist. Idempotent via the unique (workspace_id, key)
-- index from migration 335, so a re-run or a concurrent seed from a pod that
-- already booted the new code is a no-op rather than an error.
--
-- Not a hazard if it is slow: until a workspace has custom statuses, the
-- resolver treats a built-in key as itself without consulting this table at
-- all, so an unseeded workspace keeps behaving exactly as it does today.
INSERT INTO issue_status (workspace_id, key, name, description, category, color, is_system, position)
SELECT w.id, s.key, s.name, s.description, s.key, s.color, TRUE, 0
FROM workspace w
CROSS JOIN (
    VALUES
        ('backlog',     'Backlog',     'Parked. Assigning an issue here never starts an agent run.',        '#6b7280'),
        ('todo',        'Todo',        'Queued for work. Moving an issue here starts the assigned agent.',  '#6b7280'),
        ('in_progress', 'In Progress', 'Actively being worked on.',                                         '#f59e0b'),
        ('in_review',   'In Review',   'Work delivered, waiting on human review. Finalizes the autopilot run.', '#22c55e'),
        ('done',        'Done',        'Completed.',                                                        '#3b82f6'),
        ('blocked',     'Blocked',     'Stalled on an external dependency.',                                '#ef4444'),
        ('cancelled',   'Cancelled',   'Decided not to do.',                                                '#6b7280')
) AS s(key, name, description, color)
ON CONFLICT DO NOTHING;
