// Package workflows implements the cerebro workflow engine (JEH-1047).
//
// The engine reacts to domain events on the in-process bus and executes
// data-driven rules stored in cerebro_workflow. Each rule has a trigger
// (e.g. status_changed), an optional condition filter, and a single action
// in phase 1 (e.g. create_sub_issue).
//
// All cerebro_workflow_* writes are idempotent: inserting a row into
// cerebro_workflow_idempotency_key claims the event for a workflow, and
// double-fires from a replayed event are silently dropped.
//
// Retries follow the spec: at most 3 attempts with 1/5/15-minute exponential
// backoff. A 4th failure escalates by creating a sub-issue under the
// triggered issue, assigned to the workflow owner.
//
// The whole engine is gated behind the CEREBRO_WORKFLOWS_ENABLED environment
// variable in PR 1. The per-user UI feature flag (cerebro_workflows) is
// added in PR 2 when the form-builder and routes ship.
package workflows
