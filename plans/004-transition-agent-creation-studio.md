# 004 — Give agent creation screens a directional transition

- **Status**: DONE
- **Commit**: 002ea0d87
- **Severity**: MEDIUM
- **Category**: Missed opportunities
- **Estimated scope**: 2 files, roughly 100–160 lines including tests

## Problem

The agent creation studio is a rare multi-screen flow, but its chooser, template picker, configuration form, AI setup, and AI builder are mounted through separate conditionals. Choosing a path or going back replaces nearly the entire page below the persistent header in one frame.

```tsx
// packages/views/agents/components/agent-creation-studio.tsx:686 — current
{mode === "choose" && (
  <ModeChooser
    onBlank={chooseBlank}
    onAI={() => setMode("ai")}
    agentBuilderEnabled={agentBuilderEnabled}
  />
)}

{mode === "templates" && (
  <TemplateChooser
    templates={filteredTemplates}
    loading={templatesLoading}
    search={templateSearch}
    onSearch={setTemplateSearch}
    selected={selectedTemplate}
    onSelect={setSelectedTemplate}
    detail={templateDetailQuery.data}
    detailLoading={templateDetailQuery.isLoading}
    onUse={applyTemplate}
  />
)}

{(mode === "blank" || mode === "template") && (
  <div className="min-h-0 flex-1 overflow-y-auto">
    {/* configuration */}
  </div>
)}

{mode === "ai" && !builderSessionId && <BuilderSetup /* ... */ />}
{mode === "ai" && builderSessionId && (
  <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(0,1.1fr)_minmax(420px,0.9fr)]">
    {/* builder conversation and live draft */}
  </div>
)}
```

Back navigation is already explicit at `packages/views/agents/components/agent-creation-studio.tsx:423-439`, but the render path does not retain whether a screen change is forward or backward. A directional transition therefore requires a tiny amount of client-only navigation state; it must not alter creation state or server behavior.

## Target

Render exactly one active screen under the header and transition it according to navigation direction.

- Import `AnimatePresence`, `motion`, and `useReducedMotion` from `motion/react`.
- Import `UI_EASE_OUT` and `UI_MOTION_DURATION` from `@multica/ui/lib/motion` as created by plan 001.
- Add local visual state `transitionDirection: 1 | -1`, initialized to `1`. This state is motion plumbing only and must not enter Zustand or persisted drafts.
- Define a stable screen key:
  - `choose`
  - `templates`
  - `configure` for both `blank` and `template`
  - `ai-setup` when `mode === "ai" && !builderSessionId`
  - `ai-builder` when `mode === "ai" && builderSessionId`
- Set direction `1` immediately before every existing forward transition: choose blank, choose AI, apply a template if the retained template screen is reached, and successfully create the builder session. The current file contains no `setMode("templates")` entry point; preserve that fact and do not invent one.
- Set direction `-1` immediately before every local back transition in `goBack` or `resetCreationMode`.
- Wrap the active screen in `<AnimatePresence mode="wait" initial={false} custom={transitionDirection}>`.
- Normal-motion targets:
  - forward enter: `{ opacity: 0, transform: "translateX(8px)" }`
  - backward enter: `{ opacity: 0, transform: "translateX(-8px)" }`
  - settled: `{ opacity: 1, transform: "translateX(0)" }`
  - forward exit: `{ opacity: 0, transform: "translateX(-8px)" }`
  - backward exit: `{ opacity: 0, transform: "translateX(8px)" }`
  - enter: `{ duration: UI_MOTION_DURATION.standard, ease: UI_EASE_OUT }`
  - exit: `{ duration: UI_MOTION_DURATION.fast, ease: UI_EASE_OUT }`
- Reduced-motion targets:
  - every transform is `"translateX(0)"`
  - opacity enter and exit each use `UI_MOTION_DURATION.fast`.
- Use complete transform strings. Do not use Motion `x` shorthand.
- The persistent header at lines 656-684 must not animate. Only the screen below it changes.

## Repo conventions to follow

- `apps/desktop/src/renderer/src/components/tab-bar.tsx:73,325-330` is the current 8px directional entrance reference. Preserve its restrained distance, but use the complete `transform` string and the exact strong ease-out from `AUDIT.md`.
- `packages/views/layout/animated-right-sidebar.tsx:36-81` keeps visual transition state local to the component instead of writing it into business stores; follow that ownership boundary.
- `packages/views/agents/components/agent-creation-studio.tsx:423-462` already centralizes back, blank, and template transitions. Add direction assignments there rather than scattering animation logic through child components.
- Plan 001 provides the exact easing `[0.23, 1, 0.32, 1]` and 150/200ms duration constants. Do not redefine them locally.

## Steps

1. Complete plan 001 first and verify `@multica/ui/lib/motion` resolves.
2. Add Motion/shared-constant imports, `shouldReduceMotion`, and local `transitionDirection` state to `AgentCreationStudio`.
3. Add a small local helper that sets direction before changing `mode`; use it for the existing chooser actions. Keep support for the retained `templates` render branch, but do not add a new way to enter it. Do not replace the existing domain mode union.
4. Update `goBack`, `resetCreationMode`, `chooseBlank`, `applyTemplate`, and the successful `startBuilder` path so each local screen change records the correct direction immediately before the state that changes the screen key.
5. Derive `screenKey` exactly as specified. Build one `screen` React node from the current conditional bodies without changing their props, handlers, or internal markup.
6. Replace the five sibling render conditionals below the header with one `AnimatePresence` and keyed `motion.div`. The wrapper must be `className="flex min-h-0 flex-1 flex-col"`; keep each existing screen root intact inside it.
7. Define variants that accept direction and use the exact full-transform targets and durations above. The exit variant must use the direction captured by the exiting screen, not whatever a later render happens to hold.
8. Extend `packages/views/agents/components/agent-creation-studio.test.ts`. Prefer testing a small exported pure helper for the screen key/direction mapping if the existing component harness is too expensive; do not export raw Motion variants solely for snapshot testing. Assert at least that `blank` and `template` share `configure`, while AI setup and AI builder have distinct keys.

## Boundaries

- Do NOT change agent draft shape, builder protocol encoding, API calls, cache updates, navigation destinations, validation, toasts, runtime/model selection, or session cleanup.
- Do NOT introduce a template-mode entry point; there is no `setMode("templates")` call at commit `002ea0d87`.
- Do NOT animate the persistent header or the long configuration form's internal sections.
- Do NOT animate width, height, grid columns, scroll position, or layout.
- Do NOT use `layout`, `layoutId`, Motion `x`, `y`, or `scale` shorthand properties.
- Do NOT persist `transitionDirection` or put it in Zustand/React Query.
- Do NOT add dependencies.
- If the screen conditionals or navigation helpers have drifted since commit `002ea0d87`, STOP and report instead of improvising.

## Verification

- **Mechanical**:
  - `pnpm --filter @multica/views typecheck`
  - `pnpm --filter @multica/views test -- agents/components/agent-creation-studio.test.ts`
  - Both must exit 0; all builder protocol, duplicate-access, and draft-merge tests must remain green.
- **Feel check**: exercise chooser → blank → back and chooser → AI setup → AI builder → back. If an existing external test harness can place the component in retained `templates` mode without changing production reachability, also verify templates → configure → back; otherwise do not add a UI entry point just for the feel check.
  - Forward screens must enter from the right by exactly 8px; backward screens must enter from the left by exactly 8px.
  - The header must remain perfectly stationary.
  - There must be no double scrollbar, clipped footer, or blank flex region during the transition.
  - Rapid back actions must retarget without replaying from zero or leaving an outgoing screen mounted.
  - In DevTools Animations at 10% speed, confirm the screen wrapper uses only opacity and a full `translateX(...)` transform.
  - Toggle `prefers-reduced-motion: reduce` and confirm direction movement disappears while the 150ms crossfade remains.
- **Done when**: every current screen and navigation path behaves identically, forward/back direction is visually coherent, no layout property animates, and the entire transition stays below 300ms.
