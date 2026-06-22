# Workflow Stage Overview — Self-Verifying & Self-Healing Test Plan

**Feature:** Workflow 阶段可视化概览页（`/workflows/[id]` 默认视图）
**Design doc:** `docs/superpowers/specs/2026-06-19-workflow-stage-overview-design.md`
**Generated:** 2026-06-19
**Updated:** 2026-06-19 (概览作为默认视图 + 视图切换)
**Tool:** playwright-cli (spec-driven testing: plan → generate → heal)

## Application Overview

The Workflow Stage Overview page is the **default view** for `/workflows/[id]`. It introduces a three-layer drill-down view: a horizontally scrollable stage card strip → a read-only ReactFlow DAG for the selected stage → a slide-out drawer showing node configuration details. A DropdownMenu view toggle in the header allows switching between overview and editor views. View preference is persisted per workspace via Zustand. Changes sync between views via the shared TanStack Query cache key.

## Self-Healing Strategy

All tests follow these resilience principles to minimize locator drift:

| Principle | Implementation |
|-----------|---------------|
| **Semantic locators** | Prefer `getByRole()`, `getByLabel()`, `getByTestId()` over CSS class selectors |
| **Text pattern matching** | Use regex (`/Stage \\d+/`) over exact string matches for dynamic content |
| **ARIA snapshots** | Use `toMatchAriaSnapshot()` for structural assertions — survives layout refactors |
| **data-testid anchors** | Recommended `data-testid` attributes documented per component; if missing, tests fall back to role+name |
| **Heal workflow** | Each scenario's `// heal:` comments document likely drift patterns and recovery actions |

### Recommended data-testid attributes

For maximum test stability, the implementation should include these `data-testid` attributes:

| Component | data-testid | Purpose |
|-----------|------------|---------|
| `StageCanvas` | `stage-canvas` | Container anchor |
| `StageCard` | `stage-card-{stageId}` | Individual card targeting |
| `StageCard` (add button) | `add-stage-button` | Create stage trigger |
| `StageNodeDag` | `stage-node-dag` | DAG container anchor |
| `NodeDetailPanel` | `node-detail-panel` | Drawer container |
| `NodeDetailPanel` (close) | `node-detail-close` | Close button |
| `StageCreateDialog` | `stage-create-dialog` | Dialog container |
| `StageCanvasSkeleton` | `stage-canvas-skeleton` | Loading state |
| `EmptyStageState` | `empty-stage-state` | Empty state CTA |
| `EmptyNodesState` | `empty-nodes-state` | Empty DAG placeholder |
| `UnassignedCard` | `stage-card-unassigned` | Virtual "未分组" card |

## Test Scenarios

### 1. Navigation & Page Shell

**Seed:** `e2e/seed-workflow-overview.spec.ts`

#### 1.1. navigate-to-overview-from-workflow-list

**File:** `e2e/workflow-overview/navigate-to-overview.spec.ts`

**Steps:**
1. From the workspace dashboard, navigate to the workflow list
   - expect: URL matches `/{slug}/workflows`
   - expect: at least one workflow card or list item is visible
2. Click on a workflow to open its default page
   - expect: URL matches `/{slug}/workflows/{id}`
   - expect: the stage canvas area is visible (overview is the default view)
   - expect: page heading contains the workflow name
   - expect: a view toggle button is visible in the header (DropdownMenu)

**Heal hints:**
- Overview is now the default — no need to click an "Overview" tab
- If view toggle button is renamed, check for button with `title="View"` or Layers/Pen icon

#### 1.2. overview-page-shell-structure

**File:** `e2e/workflow-overview/overview-page-shell-structure.spec.ts`

**Steps:**
1. Navigate directly to a workflow overview page with stages
   - expect: three main zones visible — top stage card strip, middle DAG area, no detail panel
2. Verify the stage card strip is at the top
   - expect: horizontal scrollable area with stage cards
3. Verify the DAG area occupies the main content space below
   - expect: ReactFlow container visible (pan/zoom controls present)
4. Verify no detail panel is shown initially
   - expect: detail panel drawer is absent or hidden

**Heal hints:**
- Use `toMatchAriaSnapshot` on the page root to verify three-zone structure
- If ReactFlow controls change, check for `.react-flow__controls` class

### 2. Stage Canvas Rendering

**Seed:** `e2e/seed-workflow-overview.spec.ts`

#### 2.1. stage-cards-render-with-correct-data

**File:** `e2e/workflow-overview/stage-cards-render.spec.ts`

**Steps:**
1. Seed a workflow with 3 stages ("需求" with 2 nodes, "设计" with 3 nodes, "编码" with 1 node) via API
2. Navigate to the overview page
   - expect: exactly 3 stage cards visible in the strip
3. Verify first card content
   - expect: card shows "需求" or "Stage 1/3" label
   - expect: card shows node count "2 nodes" or similar
4. Verify cards are ordered by `sort_order`
   - expect: first card has lower sort_order than second

**Heal hints:**
- Node count text may use singular/plural — match with regex `/\d+ node/`
- Stage order can be verified by card position in DOM

#### 2.2. loading-skeleton-displayed

**File:** `e2e/workflow-overview/loading-skeleton.spec.ts`

**Steps:**
1. Intercept the workflow GET API request with a 2-second delay
2. Navigate to the overview page
   - expect: skeleton placeholder cards visible (gray pulsing rectangles)
   - expect: at least 3 skeleton cards rendered
3. Wait for API response
   - expect: skeleton disappears, real stage cards appear
   - expect: no error state shown

**Heal hints:**
- Use `route` to add latency: `page.route('**/api/workflows/*', async route => { setTimeout(() => route.continue(), 2000); })`
- Skeleton selector: `[data-testid="stage-canvas-skeleton"]` or `.animate-pulse`

#### 2.3. empty-state-when-no-stages

**File:** `e2e/workflow-overview/empty-state-no-stages.spec.ts`

**Steps:**
1. Seed a workflow with zero stages (nodes may exist with `stage_id = NULL`)
2. Navigate to the overview page
   - expect: empty state message visible — "No stages defined yet" or Chinese equivalent
   - expect: CTA button "Create first stage" visible and clickable
3. Click the CTA button
   - expect: stage creation dialog opens

**Heal hints:**
- Match text with regex: `/No stages|尚未定义阶段|创建第一个阶段/`
- If CTA text changes, fall back to button role near empty state region

#### 2.4. unassigned-nodes-virtual-card

**File:** `e2e/workflow-overview/unassigned-nodes-card.spec.ts`

**Steps:**
1. Seed a workflow with stages AND some nodes that have `stage_id = null`
2. Navigate to the overview page
   - expect: an "Unassigned" or "未分组" virtual card appears in the strip
   - expect: the unassigned card shows the count of nodes with `stage_id = null`
3. Click the unassigned card
   - expect: DAG renders showing the unassigned nodes

**Heal hints:**
- "Unassigned" text may be localized — match with regex `/Unassigned|未分组/`
- Virtual card should always be last in the strip

#### 2.5. many-stages-horizontal-scroll

**File:** `e2e/workflow-overview/many-stages-scroll.spec.ts`

**Steps:**
1. Seed a workflow with 12 stages
2. Navigate to the overview page
   - expect: not all stage cards are visible in viewport at once
   - expect: horizontal scroll is possible (scrollable container)
   - expect: fade/gradient masks at left/right edges when scrolled to middle
3. Scroll to the rightmost card
   - expect: last stage card is fully visible
   - expect: fade mask on left edge, no mask on right edge

**Heal hints:**
- Check container has `overflow-x: auto` or `scroll`
- Fade masks: check for elements with gradient background or `mask-image` CSS
- Use `page.evaluate()` to check `scrollWidth > clientWidth`

### 3. Stage CRUD Operations

**Seed:** `e2e/seed-workflow-overview.spec.ts`

#### 3.1. create-stage-via-dialog

**File:** `e2e/workflow-overview/create-stage.spec.ts`

**Steps:**
1. Navigate to overview page of a workflow with stages
2. Record current number of stage cards
3. Click the "+" add stage button at the end of the strip
   - expect: a dialog/modal opens with title "Create Stage" or "创建阶段"
   - expect: dialog contains a name input field
   - expect: dialog contains an optional description field
4. Type "测试阶段" into the name field
5. Click confirm/save button
   - expect: dialog closes
   - expect: a new stage card with name "测试阶段" appears in the strip
   - expect: total card count increases by 1
   - expect: success notification or optimistic update visible

**Heal hints:**
- Dialog title: match with regex `/Create Stage|创建阶段|新建阶段/`
- If optimistic update, card appears before API response
- Verify via network: POST to `/api/workflows/{id}/stages` returns 201

#### 3.2. edit-stage-name

**File:** `e2e/workflow-overview/edit-stage.spec.ts`

**Steps:**
1. Navigate to overview page with stages
2. Open the context menu (┇) on the first stage card
   - expect: menu shows "Edit" or "编辑" option
3. Click "Edit"
   - expect: dialog opens pre-filled with current stage name
4. Change name to "需求分析" and save
   - expect: dialog closes
   - expect: stage card now shows "需求分析"

**Heal hints:**
- Context menu trigger: three-dot button or right-click on card
- If no context menu exists, check for inline edit on double-click

#### 3.3. delete-stage-with-confirmation

**File:** `e2e/workflow-overview/delete-stage.spec.ts`

**Steps:**
1. Navigate to overview page with stages
2. Open the context menu on a stage that has nodes
3. Click "Delete" or "删除"
   - expect: confirmation dialog appears
   - expect: dialog mentions how many nodes will be moved to "Unassigned"
4. Confirm deletion
   - expect: stage card disappears from the strip
   - expect: nodes that belonged to this stage now appear under "Unassigned" card

**Heal hints:**
- Confirmation dialog text: `/This stage contains \d+ node|此阶段包含 \d+ 个节点/`
- Verify API: DELETE `/api/workflows/{id}/stages/{stageId}` returns 200

#### 3.4. delete-stage-without-nodes

**File:** `e2e/workflow-overview/delete-empty-stage.spec.ts`

**Steps:**
1. Navigate to overview page with an empty stage (no nodes)
2. Open context menu on the empty stage
3. Click "Delete"
   - expect: confirmation dialog does NOT mention moving nodes
   - or: stage is deleted immediately without confirmation
4. Confirm if needed
   - expect: stage card removed from strip

#### 3.5. reorder-stages-via-drag

**File:** `e2e/workflow-overview/reorder-stages.spec.ts`

**Steps:**
1. Navigate to overview page with 3+ stages
2. Record the order of stage names
3. Drag the first stage card to the right of the second stage card
   - expect: card order visually updates (first and second swap positions)
4. Reload the page
   - expect: the new order persists (stages are in the swapped order)

**Heal hints:**
- This is P1 in the design — may use dialog-based reorder instead of drag
- If drag not implemented: verify dialog reorder works
- Check API: PUT `/api/workflows/{id}/stages/reorder` is called

### 4. Node DAG Interaction

**Seed:** `e2e/seed-workflow-overview.spec.ts`

#### 4.1. dag-renders-on-stage-selection

**File:** `e2e/workflow-overview/dag-on-stage-select.spec.ts`

**Steps:**
1. Navigate to overview page with multiple stages, each having nodes
2. Verify initial state: DAG area shows placeholder or first stage's nodes
3. Click the second stage card
   - expect: stage card gets selected visual state (border/accent highlight)
   - expect: DAG area updates to show nodes belonging to the second stage
   - expect: DAG auto-fits view (`fitView` animation visible or complete)
4. Click the first stage card again
   - expect: DAG updates to show first stage's nodes
   - expect: second card loses selected state, first card gains it

**Heal hints:**
- Selected state: `aria-selected="true"` or `.selected` class
- DAG transition: wait for `max-height` animation or use 300ms wait
- Verify nodes: count ReactFlow node elements matches expected node count

#### 4.2. dag-is-read-only

**File:** `e2e/workflow-overview/dag-read-only.spec.ts`

**Steps:**
1. Select a stage with nodes
2. Verify the DAG is in read-only mode
   - expect: nodes are visible but cannot be dragged (attempt drag → node stays in place)
   - expect: no edge creation handles visible
   - expect: no delete keybinding works on nodes (press Delete → nothing happens)
3. Verify nodes are clickable (selection for detail panel)
   - expect: clicking a node does not enter edit mode
   - expect: detail panel opens instead

**Heal hints:**
- Read-only check: `page.evaluate(() => document.querySelector('.react-flow__node.selected') === null)` after drag attempt
- ReactFlow in read-only mode has `nodesDraggable={false}` etc.

#### 4.3. empty-stage-dag-placeholder

**File:** `e2e/workflow-overview/empty-stage-dag.spec.ts`

**Steps:**
1. Seed a stage with zero nodes
2. Navigate to overview page and select the empty stage
   - expect: DAG area shows empty state message
   - expect: text says "No nodes in this stage" or "此阶段暂无节点"
   - expect: message suggests adding nodes in the editor
3. Select a non-empty stage
   - expect: DAG shows nodes normally

**Heal hints:**
- Empty text: `/No nodes|暂无节点|在编辑器中添加/`

#### 4.4. many-nodes-fit-view

**File:** `e2e/workflow-overview/many-nodes-fit-view.spec.ts`

**Steps:**
1. Seed a stage with 15+ nodes and complex edges
2. Select that stage
   - expect: all nodes are visible within the viewport (fitView applied)
   - expect: zoom/pan controls functional
3. Zoom in via controls
   - expect: viewport zooms, scroll bars or pan available
4. Click fit-view button
   - expect: view returns to showing all nodes

**Heal hints:**
- ReactFlow controls: `.react-flow__controls-fitview`
- Verify all nodes visible: use bounding box comparison

### 5. Node Detail Panel

**Seed:** `e2e/seed-workflow-overview.spec.ts`

#### 5.1. detail-panel-opens-on-node-click

**File:** `e2e/workflow-overview/detail-panel-open.spec.ts`

**Steps:**
1. Select a stage that has nodes with worker/critic configured
2. Click on a node in the DAG
   - expect: a slide-out drawer panel appears on the right side
   - expect: panel shows the node name as title
   - expect: DAG node gets selected visual state
3. Verify key sections are present
   - expect: "Worker" section visible
   - expect: "Critic" section visible
   - expect: "Format Schema" section visible
   - expect: "Relations" section visible

**Heal hints:**
- Panel selector: `[data-testid="node-detail-panel"]` or `[role="dialog"]` or `[role="complementary"]`
- Section headings: match with regex `/Worker|Critic|Format Schema|Relations/`
- Drawer position: check CSS `right: 0` or `translateX`

#### 5.2. detail-panel-shows-configured-values

**File:** `e2e/workflow-overview/detail-panel-configured.spec.ts`

**Steps:**
1. Seed a node with: worker type "agent", assigned to "TestAgent", critic type "human", format_schema with a JSON schema
2. Navigate to overview, select the stage, click the node
   - expect: Worker section shows "agent" type and "TestAgent" name
   - expect: Critic section shows "human" type and reviewer name
   - expect: Format Schema section shows formatted JSON (not raw string)
   - expect: Relations section shows upstream and downstream connections

**Heal hints:**
- JSON display may be syntax-highlighted or pretty-printed — check content exists
- Agent name may link to agent detail page — verify href contains agent ID

#### 5.3. detail-panel-shows-unconfigured-state

**File:** `e2e/workflow-overview/detail-panel-unconfigured.spec.ts`

**Steps:**
1. Seed a node with no worker, no critic, no format_schema
2. Select its stage and click the node
   - expect: Worker section shows "Not configured" or "未配置" in muted style
   - expect: Critic section shows "Not configured" or "未配置"
   - expect: Format Schema section shows "No format constraints" or "无格式约束" or is collapsed

**Heal hints:**
- "未配置" text style: check for `text-muted-foreground` class or reduced opacity

#### 5.4. detail-panel-close-methods

**File:** `e2e/workflow-overview/detail-panel-close.spec.ts`

**Steps:**
1. Open the detail panel by clicking a node
2. Click the × close button
   - expect: panel closes/disappears
   - expect: node deselects in DAG
3. Click another node to reopen panel
4. Click on the DAG background (not on a node)
   - expect: panel closes
5. Click a node to open panel, then press Escape
   - expect: panel closes

**Heal hints:**
- Close button: `[data-testid="node-detail-close"]` or `[aria-label="Close"]`
- Escape key: `page.keyboard.press('Escape')`

#### 5.5. switching-nodes-updates-panel

**File:** `e2e/workflow-overview/detail-panel-switch-node.spec.ts`

**Steps:**
1. Open detail panel for node A
2. Record the displayed node name and worker config
3. Click on node B in the DAG (same stage or different stage)
   - expect: panel content updates to show node B's details
   - expect: panel does NOT close and reopen (seamless content swap)
   - expect: node A deselects, node B selects in DAG
4. Switch to a different stage and click a node there
   - expect: panel still shows the correct node details

**Heal hints:**
- Verify seamless update: panel container element persists, child content changes
- Transition: content may fade/crossfade

### 6. Error & Edge Cases

**Seed:** `e2e/seed-workflow-overview.spec.ts`

#### 6.1. workflow-not-found-404

**File:** `e2e/workflow-overview/not-found.spec.ts`

**Steps:**
1. Navigate to `/some-slug/workflows/non-existent-id/overview`
   - expect: 404 page or "not found" message displayed
   - expect: page does NOT show loading skeleton indefinitely
   - expect: page does NOT white-screen

**Heal hints:**
- 404 check: look for `/not.?found|404/` text or dedicated 404 component

#### 6.2. api-error-with-retry

**File:** `e2e/workflow-overview/api-error-retry.spec.ts`

**Steps:**
1. Intercept the workflow GET API to return 500
2. Navigate to the overview page
   - expect: error alert/message visible
   - expect: "Retry" button present
3. Remove the API interception
4. Click "Retry"
   - expect: data loads successfully
   - expect: stage cards appear

**Heal hints:**
- Error alert: `[role="alert"]` or `.destructive` variant
- Retry button: `getByRole('button', { name: /retry|重试/i })`

#### 6.3. no-workspace-access

**File:** `e2e/workflow-overview/no-access.spec.ts`

**Steps:**
1. Log in as a user who is NOT a member of the target workspace
2. Navigate to `/{slug}/workflows/{id}/overview`
   - expect: access denied / "No access" page displayed
   - expect: no workflow data leaked in the DOM

**Heal hints:**
- `NoAccessPage` component: check for specific text or data-testid

#### 6.4. delete-stage-with-nodes-confirmation-content

**File:** `e2e/workflow-overview/delete-stage-confirm-content.spec.ts`

**Steps:**
1. Seed a stage with exactly 3 nodes
2. Attempt to delete the stage
   - expect: confirmation dialog explicitly says "3" nodes
   - expect: dialog mentions nodes will go to "Unassigned" or "未分组"
3. Cancel the deletion
   - expect: stage still exists in strip
   - expect: nodes still belong to this stage

**Heal hints:**
- Node count in dialog: extract number with `/\d+/` and verify it matches
- Cancel: `getByRole('button', { name: /cancel|取消/i })`

#### 6.5. delete-last-stage

**File:** `e2e/workflow-overview/delete-last-stage.spec.ts`

**Steps:**
1. Seed a workflow with exactly 1 stage
2. Delete that stage
   - expect: stage card disappears
   - expect: workflow returns to empty state ("No stages defined yet")

### 7. Cross-Page Integration

**Seed:** `e2e/seed-workflow-overview.spec.ts`

#### 7.1. open-in-editor-link

**File:** `e2e/workflow-overview/open-in-editor.spec.ts`

**Steps:**
1. Open the detail panel for a node
2. Click the "Open in editor" button in the panel footer
   - expect: view switches to editor view **in-place** (no URL change from `/workflows/{id}`)
   - expect: the editor's ReactFlow DAG is displayed
   - expect: the same workflow's nodes are visible in the editor
   - expect: the view toggle button now shows the editor icon

**Heal hints:**
- Button text: `/Open in editor|在编辑器中打开/`
- No navigation occurs — URL stays at `/workflows/{id}`
- URL change: `page.waitForURL()` for the editor route

#### 7.2. editor-changes-sync-to-overview

**File:** `e2e/workflow-overview/editor-sync-to-overview.spec.ts`

**Steps:**
1. Open the overview page and note the stage/node count
2. Open the editor page in a new tab (or same tab)
3. Add a new node to a stage in the editor and save
4. Switch back to the overview page (or reload)
   - expect: the new node appears in the correct stage's DAG
   - expect: node count on the stage card updates

**Heal hints:**
- TanStack Query: overview and editor share the same cache key
- If stale data: wait for background refetch or reload

#### 7.3. overview-stage-changes-sync-to-editor

**File:** `e2e/workflow-overview/overview-sync-to-editor.spec.ts`

**Steps:**
1. Open the overview page and create a new stage
2. Navigate to the editor page
   - expect: the new stage's nodes are visible in the editor's DAG
   - expect: nodes are grouped/colored by stage (if editor supports stage grouping)

### 8. Responsive & Accessibility

**Seed:** `e2e/seed-workflow-overview.spec.ts`

#### 8.1. responsive-layout-below-breakpoint

**File:** `e2e/workflow-overview/responsive-mobile.spec.ts`

**Steps:**
1. Resize the viewport to 800×600 (below 1024px breakpoint)
2. Navigate to the overview page
   - expect: stage cards are stacked vertically (accordion or list layout)
   - expect: no horizontal scroll on stage strip
3. Click a stage
   - expect: DAG renders below or within the expanded stage section
4. Open the detail panel
   - expect: drawer opens from bottom (full-screen on mobile) instead of right side

**Heal hints:**
- Resize: `page.setViewportSize({ width: 800, height: 600 })`
- Layout check: compare `getBoundingClientRect()` of stage cards (vertical = same x, different y)

#### 8.2. keyboard-navigation

**File:** `e2e/workflow-overview/keyboard-navigation.spec.ts`

**Steps:**
1. Navigate to overview page with 3+ stages
2. Press Tab to focus the first stage card
   - expect: focus ring visible on a stage card
3. Press ArrowRight
   - expect: focus moves to the next stage card
   - expect: next card's stage is now selected (DAG updates)
4. Press ArrowLeft
   - expect: focus returns to previous card
5. Press Escape
   - expect: current stage deselects (if any)
   - expect: if detail panel open, it closes

**Heal hints:**
- Focus ring: `:focus-visible` pseudo-class
- Arrow key: `page.keyboard.press('ArrowRight')`
- Tab order: verify `tabindex` or natural DOM order

#### 8.3. loading-aria-announcements

**File:** `e2e/workflow-overview/aria-live-regions.spec.ts`

**Steps:**
1. Navigate to overview page
2. Verify loading state has appropriate ARIA
   - expect: loading region has `aria-busy="true"` or `role="status"`
3. After data loads, create a new stage
   - expect: success announcement in a live region (`role="status"` or `aria-live="polite"`)

**Heal hints:**
- `aria-busy`: on the canvas container during loading
- Live region: check for elements with `aria-live` attribute

## Seed Test

Create `e2e/seed-workflow-overview.spec.ts` before generating scenario tests:

```typescript
// e2e/seed-workflow-overview.spec.ts
// Seed test for workflow stage overview feature.
// All overview scenarios assume a logged-in user on a workspace-scoped
// workflow overview page with pre-seeded data.
//
// Individual scenarios may extend this with additional API seeding.

import { test as baseTest, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";

const test = baseTest.extend({
  page: async ({ page }, use) => {
    const api = await createTestApi();
    const slug = await loginAsDefault(page);

    // Seed a workflow with stages and nodes for the overview page
    // (Exact seeding TBD once workflow API client methods are available)
    // For now, navigate to the first available workflow's overview page.

    await page.goto(`/${slug}/workflows`);
    await use(page);
  },
});

export { test, expect };
```

## Generation & Heal Workflow

### Initial generation

```bash
# 1. Verify Playwright workspace
npx --no-install playwright --version

# 2. Start the app (backend + frontend must be running)
# make start  (in another terminal)

# 3. Generate seed test first
PLAYWRIGHT_HTML_OPEN=never npx playwright test e2e/seed-workflow-overview.spec.ts --debug=cli
playwright-cli attach tw-XXXX
playwright-cli resume
# Explore the app, verify seed works
playwright-cli close

# 4. Generate each scenario one at a time
PLAYWRIGHT_HTML_OPEN=never npx playwright test e2e/seed-workflow-overview.spec.ts --debug=cli
playwright-cli attach tw-XXXX
playwright-cli resume
# Walk through scenario steps per the spec
# Copy generated TypeScript into the target .spec.ts file
playwright-cli close

# 5. Run generated tests
npx playwright test e2e/workflow-overview/
```

### Healing failing tests

```bash
# 1. Run all overview tests, capture failures
npx playwright test e2e/workflow-overview/

# 2. For each failing test, debug:
PLAYWRIGHT_HTML_OPEN=never npx playwright test <failing-file>:<line> --debug=cli
playwright-cli attach tw-XXXX

# 3. Diagnose with snapshots, console, network
playwright-cli snapshot
playwright-cli console
playwright-cli requests

# 4. Rehearse corrected interaction
playwright-cli click <corrected-ref>
# Copy the corrected TypeScript from output

# 5. Edit the test file with the fix
# 6. Rerun to confirm green
npx playwright test <failing-file>

# 7. Reconcile with spec:
#    - Pure locator drift → fix test only
#    - App behavior changed → update this spec file
#    - Unclear if regression → ask user before changing
```

### Common drift patterns & recovery

| Drift symptom | Likely cause | Recovery action |
|--------------|-------------|-----------------|
| Stage card text not found | i18n key renamed | Use regex fallback, check `locales/*/workflows.json` |
| Node detail panel not opening | Drawer component changed | Check for `[role="dialog"]` or `[role="complementary"]` |
| DAG empty after stage select | ReactFlow render timing | Add `waitFor` on `.react-flow__node` count |
| Create stage dialog not found | Dialog library changed | Check for `[role="dialog"]` or portal container |
| "Unassigned" card missing | Text changed or logic changed | Check `stage_id = null` handling in API response |
| Responsive breakpoint mismatch | CSS breakpoint changed | Use `page.evaluate(() => window.innerWidth)` to confirm |
