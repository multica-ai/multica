# ObitaPlus Right History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persistent right-side chat history list to the ObitaPlus page while keeping the center chat readable and preserving the existing floating chat behavior.

**Architecture:** Reuse the existing chat session list, pending-task aggregate, active-session store, and session actions already implemented for the chat history popover. Split the history rendering into a reusable page/sidebar presentation so the floating window keeps its popover and the page variant renders a fixed-width right rail.

**Tech Stack:** Next.js App Router, React/TypeScript shared views in `packages/views`, Zustand chat store, TanStack Query, Vitest + Testing Library.

---

### Task 1: Add Failing Page-Mode Tests

**Files:**
- Modify: `packages/views/chat/components/chat-page.test.tsx`
- Modify: `packages/views/chat/components/chat-message-list.test.tsx`

- [ ] **Step 1: Test ObitaPlus page renders a right history rail**

Add a mocked `ChatWindow` assertion that page mode can expose a dedicated right history region. Expected before implementation: fail because `ChatPage` only renders the chat window.

- [ ] **Step 2: Test page message list keeps constrained content width**

Assert page-mode message list uses a constrained page width class, not `max-w-none`. Expected before implementation: fail because current page mode expands message rows full width.

### Task 2: Implement Reusable History Rail

**Files:**
- Modify: `packages/views/chat/components/chat-window.tsx`

- [ ] **Step 1: Extract history row rendering from `SessionDropdown` into reusable render path**

Keep existing state and actions: select session, cross-agent switch, rename, delete, stop running task, running/unread/completed affordances.

- [ ] **Step 2: Keep floating mode as a popover**

`SessionDropdown` continues to render the trigger and popover with no visual regression.

- [ ] **Step 3: Render page mode as a right-side rail**

In `ChatWindow variant="page"`, wrap chat content and the history list in a horizontal layout. The center chat uses `min-w-0 flex-1`; the right rail uses fixed width around 20rem and a left border.

### Task 3: Improve Chat Readability

**Files:**
- Modify: `packages/views/chat/components/chat-message-list.tsx`
- Modify: `packages/views/chat/components/chat-input.tsx`

- [ ] **Step 1: Constrain page-mode message content**

Use the same centered max-width treatment as floating mode so user and assistant messages no longer sit against the page edges.

- [ ] **Step 2: Keep input centered**

Retain the existing page-mode compact input shape and centered `max-w-4xl` shell.

### Task 4: Verify and Review

**Files:**
- Modify: `docs/system-design/frontend/obitaplus/design.md`

- [ ] **Step 1: Run focused tests**

Run `pnpm vitest packages/views/chat/components/chat-page.test.tsx packages/views/chat/components/chat-message-list.test.tsx packages/views/chat/components/chat-input-layout.test.tsx`.

- [ ] **Step 2: Update design doc**

Document the right-side history rail and page-mode message width behavior.

- [ ] **Step 3: Run code review and guidance checks**

Use `obita-code-review`, `obita-coding-guidance`, and versioning rules for the changed frontend/docs files.
