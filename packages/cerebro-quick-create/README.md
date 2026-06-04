# @multica/cerebro-quick-create

Cerebro-fork extensions for the "Create with agent" (Quick Create) modal.

## `useQuickCreateVersionAutoSwitch`

FIR-33 (sub-issue of FIR-18). When the agent picked in the Quick Create modal
runs a daemon whose bundled multica CLI is below `MIN_QUICK_CREATE_CLI_VERSION`,
the upstream modal shows a warning banner and disables Create — a dead-end.

This hook instead auto-switches the modal to manual create (carrying the typed
prompt, project, and parent over via the existing `switchToManual`). The
warning banner stays in the upstream component as a backstop.

Gated behind the `cerebro_quick_create_version_autoswitch` feature flag. The
upstream modal wires it in with a small `// CEREBRO-PATCH(...)` marked import +
hook call; all of the gate/version/loaded decision logic lives here in the
cerebro zone.
