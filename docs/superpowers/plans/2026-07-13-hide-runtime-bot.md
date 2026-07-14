# Hide Runtime Bot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Temporarily hide the Workspace settings Runtime Bot entry without removing its backend capability or stored installations.

**Architecture:** Add a local frontend feature flag in `LarkTab`. Keep the Runtime Bot implementation in place for easy restoration, but disable its runtime query, section, and install dialog while the flag is false.

**Tech Stack:** React, TanStack Query, Vitest, Testing Library, TypeScript.

---

### Task 1: Hide the Runtime Bot settings surface

**Files:**
- Modify: `packages/views/settings/components/lark-tab.tsx`
- Test: `packages/views/settings/components/lark-tab.test.tsx`

- [x] Add a test that renders `LarkTab` and asserts the Runtime Bot heading and runtime selector are absent.
- [x] Run `pnpm test settings/components/lark-tab.test.tsx` from `packages/views` and confirm the new test fails.
- [x] Add `RUNTIME_BOT_CONNECT_ENABLED = false`, disable the runtime query, and conditionally omit the Runtime Bot section and dialog.
- [x] Run the targeted test and `pnpm typecheck` from `packages/views`.
- [x] Verify the local Workspace integration page no longer displays Runtime Bot while Notification Bot remains visible.
