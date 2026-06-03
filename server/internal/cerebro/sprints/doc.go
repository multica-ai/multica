// Package sprints implements the cerebro project-sprint feature (FIR-2666).
//
// The feature turns a project into a "sprint container": admins configure a
// duration, start day, lead-days for auto-create, and recurring tasks; the
// server then auto-creates the next sprint when the active one is within
// the configured lead days of its end, optionally moves incomplete issues
// across, marks the closing sprint done, and clones any matching-cadence
// recurring tasks into the new sprint as real issues.
//
// All periodic values (duration, lead-days, cadence) are settings on
// cerebro_sprint_settings or cerebro_sprint_recurring_task — there are no
// constants in this package's runtime code. The "2 days before" example in
// the design doc maps to auto_create_lead_days, which an admin can change
// per project.
//
// The package is gated by the cerebro_sprints feature flag (default off in
// packages/cerebro-feature-flags/registry.ts). The daily sweeper in
// server/cmd/server/cerebro_sprint_sweeper.go is started unconditionally —
// it no-ops on projects where settings.enabled = FALSE or
// auto_create_enabled = FALSE, so toggling the flag off in the UI is the
// kill switch.
package sprints
