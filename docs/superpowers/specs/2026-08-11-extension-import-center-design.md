# Extension Import Center Redesign

## Goal

Make the Extension page explain what was imported and how each Extension primitive becomes a Multica resource. The page should be useful during manual client verification, not only as a release list.

## Page structure

The page keeps one persistent workspace rather than a separate import or confirmation page:

1. **Import history** on the left — a selectable list of imported Extension releases, grouped by Extension name and version.
2. **Selected release** on the right — three compact tabs: **原子能力映射**, **资源清单**, and **导入信息**.
3. The existing top-right **导入 Extension** button opens a small ZIP-file picker dialog only. Choosing a valid package closes the dialog, selects the new release in history, and opens its conversion tab.

There is no separate configuration panel, second confirmation page, or standalone pipeline screen. The selected history row points visually to the detail panel. The mapping view uses only one relationship arrow per row: `source primitive → generated Multica resource`.

## 原子能力映射

The details view groups the imported document into these mappings:

- a Flow Command ending in `-e2e` → Squad Instructions;
- every other Command → a Generated Skill whose content preserves the Command content and whose config preserves Command metadata;
- an Extension Agent → an Agent resource;
- a source Skill → a Skill resource.

The UI must not show intermediate workflow labels such as “挂载”, “等待任务”, or “绑定” in the mapping rows.

Every mapping row has a leading confirmation check. A green check means the current mapping has been persisted and is effective. A neutral check means that row is pending confirmation:

- all rows of a newly imported release start neutral, including a release with the same Extension name;
- editing the Squad name makes only the Flow Command → Squad Instructions row neutral;
- editing an Agent runtime makes only that Agent → internal Agent row neutral;
- unrelated, already-persisted rows retain their green checks.

Clicking **确认导入** or **确认更新** confirms only the pending rows in row order and restores their green checks. The tab then shows a compact in-place completion status. It does not navigate away or replace the mapping view.

## 资源清单

The resource-inventory tab makes the selected Extension release → versioned Squad mapping explicit. It groups the Squad's internal Agents and internal Skills. Each Agent row also displays the independent workspace Runtime bound to that Agent, so the execution binding is visible without making Runtime a Squad-owned resource or rendering it as a standalone card. Agents and Skills in this inventory are effective only inside the Squad: they are not exposed in public Agent/Skills lists and cannot be selected through ordinary task-assignment entry points. Runtimes remain separately managed in the workspace Runtime area.

The conversion tab is also the only edit surface:

- The `-e2e` Command → Squad Instructions target identifies a version-free Squad name. An Extension release version is never appended to a Squad that is being updated in place.
- Each Agent → internal Agent target contains an editable fixed-runtime selector.
- All Command, Skill, and internal-resource names remain read-only.

Every Extension release creates its own versioned Squad template. The editable field contains only the Squad base name (for example `delegate`); the UI directly appends a read-only release suffix (for example `· v2.0.0`) to form the complete Squad name. No release overwrites another release's Squad, and users cannot edit the version suffix. Each internal Agent's fixed runtime remains editable, but the release → Squad mapping remains one-to-one and traceable.

The pending ZIP release shows **确认导入**. Previously imported releases retain green checks; editing their Squad name or runtime makes only the affected mapping neutral and reveals **保存更改**. These edits change the selected version's template configuration only. They never rewrite a newer or older release.

Using a versioned Squad creates a member-owned copy that records its source version. Importing a later Extension version never changes existing copies. Historical templates may be **归档** to remove them from new selection while retaining their mapping history and existing member copies. Re-importing the same version with identical content is idempotent; reusing a version with changed content is rejected and requires a version bump.

## Command classification

The canonical marker is defined once:

```go
const PlatformExtensionFlowCommandSuffix = "-e2e"
```

Flow classification uses `strings.HasSuffix(command.Name, PlatformExtensionFlowCommandSuffix)`. The new mock and UI use `delegate-e2e` with no dot suffix. Existing `.flow` documents remain accepted as a legacy compatibility format until their migration is complete; new documents should use `-e2e`.

## Import interaction

- The existing top-right Import button opens a ZIP picker dialog. It does not open a full import wizard.
- The picker validates that the package contains exactly one Command ending with `-e2e`; validation failure remains inside the picker.
- Import success inserts the selected release into the history list, selects it, and opens the conversion tab with the editable mapping rows.
- The primary action is rendered inside the conversion tab: **确认导入** for a new release and **确认更新** for an existing release.
- Re-importing the same key/version keeps the existing idempotent behavior.
- Import/runtime errors remain inline and do not replace the history list.
- The page remains responsive: history and detail panels collapse to a single column on narrow screens.

## Scope boundaries

- No change to Runtime Pool allocation behavior.
- ZIP extension packages are supported: `extension.json` declares the atomic resources and lists each Skill file, while `skills/<skill-key>/...` holds the text or binary assets. Binary files are stored base64-tagged and restored to their original bytes by the daemon.
- No new import endpoint; the current list/import/detail APIs remain the data source.
- The implementation may extend the canonical manifest/native mapping so Generated Skills are visible and persisted, but it must not change fixed-agent behavior.

## Acceptance criteria

- The Extensions page visibly contains a history list, selected-release detail panel, and current import status panel.
- Selecting a history row changes the detail content and selected arrow target.
- `delegate-e2e` is displayed as a Flow Command mapped to Squad Instructions.
- Non-`-e2e` Commands are displayed as Generated Skills.
- Agents and source Skills are displayed as direct resource mappings.
- Existing import success, idempotency, invalid JSON, and runtime-unavailable states remain covered by tests.
