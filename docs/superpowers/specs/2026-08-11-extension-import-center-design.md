# Extension Import Center Redesign

## Goal

Make the Extension page explain what was imported and how each Extension primitive becomes a Multica resource. The page should be useful during manual client verification, not only as a release list.

## Page structure

The page has three persistent regions:

1. **Import history** — a selectable list of imported Extension releases, showing name, version, import time, resource counts, and current/previous status.
2. **Extension details** — the selected release, with an atomic mapping view and compact tabs for resource inventory and import metadata.
3. **Current import status** — release state, Runtime readiness, resource creation checks, and the next verification action.

The selected history row points visually to the details panel. The mapping view uses only one relationship arrow per row: `source primitive → generated Multica resource`.

## Atomic mapping

The details view groups the imported document into these mappings:

- a Flow Command ending in `-e2e` → Squad Instructions;
- every other Command → a Generated Skill whose content preserves the Command content and whose config preserves Command metadata;
- an Extension Agent → an Agent resource;
- a source Skill → a Skill resource.

The UI must not show intermediate workflow labels such as “挂载”, “等待任务”, or “绑定” in the mapping rows.

## Command classification

The canonical marker is defined once:

```go
const PlatformExtensionFlowCommandSuffix = "-e2e"
```

Flow classification uses `strings.HasSuffix(command.Name, PlatformExtensionFlowCommandSuffix)`. The new mock and UI use `delegate-e2e` with no dot suffix. Existing `.flow` documents remain accepted as a legacy compatibility format until their migration is complete; new documents should use `-e2e`.

## Import interaction

- The existing Import button and JSON validation remain unchanged.
- Import success inserts the selected release into the history list and selects it.
- Re-importing the same key/version keeps the existing idempotent behavior.
- Import/runtime errors remain inline and do not replace the history list.
- The page remains responsive: history and detail panels collapse to a single column on narrow screens.

## Scope boundaries

- No change to Runtime Pool allocation behavior.
- No ZIP/binary Skill support in this redesign.
- No new import endpoint; the current list/import/detail APIs remain the data source.
- The implementation may extend the canonical manifest/native mapping so Generated Skills are visible and persisted, but it must not change fixed-agent behavior.

## Acceptance criteria

- The Extensions page visibly contains a history list, selected-release detail panel, and current import status panel.
- Selecting a history row changes the detail content and selected arrow target.
- `delegate-e2e` is displayed as a Flow Command mapped to Squad Instructions.
- Non-`-e2e` Commands are displayed as Generated Skills.
- Agents and source Skills are displayed as direct resource mappings.
- Existing import success, idempotency, invalid JSON, and runtime-unavailable states remain covered by tests.
