// Package recurringissue implements the cerebro "recurring issues" feature
// (TECH-3064 / FIR-334).
//
// A user marks an individual issue as recurring from a panel on the issue
// (the FIR-334 mockup: frequency, "on status change", "create new task",
// "recur forever", "update status to", "sync recurrence to due date"). When
// the issue reaches its trigger status (default 'done'), a poll-based sweeper
// spawns the next occurrence with a fresh due date and the same data
// (assignee, title, description, labels, attachments), then repoints the rule
// onto the new issue so the series chains.
//
// The engine mirrors the sprint sweeper (server/internal/cerebro/sprints):
// one background goroutine started unconditionally by main.go, no-opping on
// workspaces where the cerebro_recurring_issues feature flag is OFF (default
// off in packages/cerebro-feature-flags/registry.ts). Firing is edge-
// triggered via the `armed` column so a closed issue spawns its successor
// exactly once, not on every tick.
//
// The feature lives entirely in the cerebro 9xxx namespace; the upstream
// issue schema is untouched. Issues are created through the upstream
// CreateIssue query (same as the sprint cloner); labels/attachments are
// copied with cerebro queries that read the upstream tables.
package recurringissue
