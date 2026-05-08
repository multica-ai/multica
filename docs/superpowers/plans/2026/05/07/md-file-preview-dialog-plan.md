# Markdown File Preview Dialog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make markdown attachment previews draggable, resizable, and optionally full-screen without changing other dialogs.

**Architecture:** The existing markdown preview button continues to own fetching and modal open state. A markdown-preview-specific dialog content component wraps the rendered markdown with a drag/resize library, tracks temporary geometry and full-screen state, and leaves shared dialog primitives untouched. Tests stay near existing file-card preview coverage.

**Tech Stack:** React, TypeScript, `@base-ui/react` dialog wrapper, `react-rnd`, lucide-react icons, Vitest, Testing Library, pnpm.

---

## File Structure

- Modify `packages/views/package.json`: add the drag/resize dependency to `dependencies`.
- Modify `pnpm-lock.yaml`: update lockfile after installing the dependency.
- Modify `packages/views/editor/markdown-file-preview.tsx`: add a dedicated draggable/resizable markdown preview dialog content component with full-screen support.
- Modify `packages/views/editor/readonly-content.test.tsx`: extend existing readonly markdown file-card tests for the new dialog controls and layout hooks.
- Modify `packages/views/editor/extensions/file-card.test.tsx`: mirror the behavior assertions for editable file-card node previews if coverage there depends on the shared preview button.

## Task 1: Add Drag/Resize Dependency

**Files:**
- Modify: `packages/views/package.json`
- Modify: `pnpm-lock.yaml`

- [ ] **Step 1: Install the dependency**

Run from the repo root:

```bash
pnpm --filter @multica/views add react-rnd
```

Expected: `packages/views/package.json` includes `react-rnd` under `dependencies`, and `pnpm-lock.yaml` is updated.

- [ ] **Step 2: Verify dependency metadata**

Run:

```bash
pnpm --filter @multica/views typecheck
```

Expected: PASS.

## Task 2: Add Failing Readonly Preview Tests

**Files:**
- Modify: `packages/views/editor/readonly-content.test.tsx`

- [ ] **Step 1: Extend the existing preview test**

In the `ReadonlyContent file cards` test named `previews markdown file cards before the download action`, add these assertions after the dialog is found:

```ts
expect(screen.getByTestId("markdown-preview-shell")).toBeInTheDocument();
expect(screen.getByTestId("markdown-preview-drag-handle")).toHaveTextContent(
  "permission-config-design.md",
);
expect(screen.getByTestId("markdown-preview-scroll")).toHaveClass("overflow-y-auto");
expect(
  screen.getByRole("button", { name: "Enter full screen" }),
).toBeInTheDocument();
```

- [ ] **Step 2: Add a full-screen toggle test**

Add this test in the same `describe("ReadonlyContent file cards", ...)` block:

```ts
it("toggles markdown previews between windowed and full-screen modes", async () => {
  previewAttachmentMarkdown.mockResolvedValue("# Preview title");

  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ReadonlyContent content="!file[fullscreen.md](https://cdn.example.com/fullscreen.md)" />
    </I18nProvider>,
  );

  fireEvent.click(screen.getByRole("button", { name: "Preview fullscreen.md" }));

  await waitFor(() =>
    expect(previewAttachmentMarkdown).toHaveBeenCalledWith(
      "https://cdn.example.com/fullscreen.md",
    ),
  );

  const shell = await screen.findByTestId("markdown-preview-shell");
  expect(shell).toHaveAttribute("data-fullscreen", "false");

  fireEvent.click(screen.getByRole("button", { name: "Enter full screen" }));
  expect(shell).toHaveAttribute("data-fullscreen", "true");
  expect(
    screen.getByRole("button", { name: "Exit full screen" }),
  ).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Exit full screen" }));
  expect(shell).toHaveAttribute("data-fullscreen", "false");
  expect(
    screen.getByRole("button", { name: "Enter full screen" }),
  ).toBeInTheDocument();
});
```

- [ ] **Step 3: Run the tests to verify they fail**

Run:

```bash
pnpm --filter @multica/views test -- editor/readonly-content.test.tsx
```

Expected: FAIL because `markdown-preview-shell`, `markdown-preview-drag-handle`, and full-screen controls do not exist yet.

## Task 3: Implement Draggable, Resizable Preview Content

**Files:**
- Modify: `packages/views/editor/markdown-file-preview.tsx`

- [ ] **Step 1: Update imports**

Replace the current import block with the needed additions:

```ts
import { useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import { Eye, Maximize2, Minimize2, XIcon } from "lucide-react";
import { Rnd } from "react-rnd";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
```

- [ ] **Step 2: Add layout constants**

Add these constants below `isMarkdownFilename`:

```ts
const DEFAULT_PREVIEW_WIDTH = 896;
const DEFAULT_PREVIEW_HEIGHT = 720;
const MIN_PREVIEW_WIDTH = 420;
const MIN_PREVIEW_HEIGHT = 320;
const PREVIEW_VIEWPORT_MARGIN = 16;
```

- [ ] **Step 3: Add geometry types and viewport helper**

Add this type and helper below the constants:

```ts
type PreviewBounds = {
  x: number;
  y: number;
  width: number;
  height: number;
};

function getDefaultPreviewBounds(): PreviewBounds {
  if (typeof window === "undefined") {
    return {
      width: DEFAULT_PREVIEW_WIDTH,
      height: DEFAULT_PREVIEW_HEIGHT,
      x: 0,
      y: 0,
    };
  }

  const width = Math.min(
    DEFAULT_PREVIEW_WIDTH,
    window.innerWidth - PREVIEW_VIEWPORT_MARGIN * 2,
  );
  const height = Math.min(
    DEFAULT_PREVIEW_HEIGHT,
    window.innerHeight - PREVIEW_VIEWPORT_MARGIN * 2,
  );

  return {
    width: Math.max(MIN_PREVIEW_WIDTH, width),
    height: Math.max(MIN_PREVIEW_HEIGHT, height),
    x: Math.max(PREVIEW_VIEWPORT_MARGIN, (window.innerWidth - width) / 2),
    y: Math.max(PREVIEW_VIEWPORT_MARGIN, (window.innerHeight - height) / 2),
  };
}

function getFullscreenPreviewBounds(fallback: PreviewBounds): PreviewBounds {
  if (typeof window === "undefined") return fallback;

  return {
    x: PREVIEW_VIEWPORT_MARGIN,
    y: PREVIEW_VIEWPORT_MARGIN,
    width: Math.max(320, window.innerWidth - PREVIEW_VIEWPORT_MARGIN * 2),
    height: Math.max(240, window.innerHeight - PREVIEW_VIEWPORT_MARGIN * 2),
  };
}
```

- [ ] **Step 4: Add preview content component**

Add this component above `MarkdownFilePreviewButton`:

```tsx
function MarkdownPreviewDialogContent({
  filename,
  previewLoading,
  children,
}: {
  filename: string;
  previewLoading: boolean;
  children: ReactNode;
}) {
  const { t } = useT("editor");
  const [fullscreen, setFullscreen] = useState(false);
  const [windowBounds, setWindowBounds] = useState(getDefaultPreviewBounds);
  const bounds = fullscreen
    ? getFullscreenPreviewBounds(windowBounds)
    : windowBounds;

  return (
    <DialogContent
      showCloseButton={false}
      className="max-w-none border-0 bg-transparent p-0 shadow-none ring-0"
    >
      <Rnd
        bounds="window"
        default={windowBounds}
        size={{ width: bounds.width, height: bounds.height }}
        position={{ x: bounds.x, y: bounds.y }}
        minWidth={MIN_PREVIEW_WIDTH}
        minHeight={MIN_PREVIEW_HEIGHT}
        disableDragging={fullscreen}
        enableResizing={!fullscreen}
        dragHandleClassName="markdown-preview-drag-handle"
        onDragStop={(_event, data) => {
          setWindowBounds((current) => ({
            ...current,
            x: data.x,
            y: data.y,
          }));
        }}
        onResizeStop={(_event, _direction, ref, _delta, position) => {
          setWindowBounds({
            x: position.x,
            y: position.y,
            width: ref.offsetWidth,
            height: ref.offsetHeight,
          });
        }}
        data-testid="markdown-preview-shell"
        data-fullscreen={fullscreen ? "true" : "false"}
        className="overflow-hidden rounded-xl bg-popover text-sm text-popover-foreground shadow-lg ring-1 ring-foreground/10"
      >
        <div className="grid h-full grid-rows-[auto_minmax(0,1fr)] gap-3 p-4">
          <DialogHeader
            data-testid="markdown-preview-drag-handle"
            className="markdown-preview-drag-handle min-w-0 cursor-move pr-20"
          >
            <DialogTitle className="truncate">{filename}</DialogTitle>
          </DialogHeader>
          <div className="absolute top-2 right-2 flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={fullscreen ? "Exit full screen" : "Enter full screen"}
              title={fullscreen ? "Exit full screen" : "Enter full screen"}
              onClick={() => setFullscreen((value) => !value)}
            >
              {fullscreen ? <Minimize2 /> : <Maximize2 />}
            </Button>
            <DialogCloseButton />
          </div>
          <div
            data-testid="markdown-preview-scroll"
            className="min-h-0 overflow-y-auto rounded-md border border-border bg-background p-4"
          >
            {previewLoading ? (
              <p className="text-sm text-muted-foreground">
                {t(($) => $.file_card.preview_loading)}
              </p>
            ) : (
              children
            )}
          </div>
        </div>
      </Rnd>
    </DialogContent>
  );
}
```

- [ ] **Step 5: Add close button helper**

Add this helper above `MarkdownPreviewDialogContent`:

```tsx
function DialogCloseButton() {
  return (
    <DialogClose render={<Button variant="ghost" size="icon-sm" />}>
      <XIcon />
      <span className="sr-only">Close</span>
    </DialogClose>
  );
}
```

- [ ] **Step 6: Use the new content component**

Replace the current fixed `DialogContent` block inside `MarkdownFilePreviewButton` with:

```tsx
<Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
  <MarkdownPreviewDialogContent
    filename={filename}
    previewLoading={previewLoading}
  >
    {renderContent(previewContent ?? "")}
  </MarkdownPreviewDialogContent>
</Dialog>
```

- [ ] **Step 7: Run readonly preview tests**

Run:

```bash
pnpm --filter @multica/views test -- editor/readonly-content.test.tsx
```

Expected: PASS for the readonly markdown preview tests.

## Task 4: Mirror Editable File-Card Coverage

**Files:**
- Modify: `packages/views/editor/extensions/file-card.test.tsx`

- [ ] **Step 1: Add dialog shell assertions**

In the editable file-card test named `previews markdown cards before the download action`, add after the dialog assertion:

```ts
expect(screen.getByTestId("markdown-preview-shell")).toBeInTheDocument();
expect(screen.getByTestId("markdown-preview-drag-handle")).toHaveTextContent(
  "permission-config-design.md",
);
expect(
  screen.getByRole("button", { name: "Enter full screen" }),
).toBeInTheDocument();
```

- [ ] **Step 2: Run editable file-card tests**

Run:

```bash
pnpm --filter @multica/views test -- editor/extensions/file-card.test.tsx
```

Expected: PASS.

## Task 5: Verify Type Safety and Diff

**Files:**
- All files changed in Tasks 1-4.

- [ ] **Step 1: Run focused tests**

Run:

```bash
pnpm --filter @multica/views test -- editor/readonly-content.test.tsx
pnpm --filter @multica/views test -- editor/extensions/file-card.test.tsx
```

Expected: both test files pass.

- [ ] **Step 2: Run typecheck**

Run:

```bash
pnpm --filter @multica/views typecheck
```

Expected: PASS.

- [ ] **Step 3: Review diff quality**

Run:

```bash
git diff --check
git diff --stat
```

Expected: no whitespace errors, and only the planned files are changed.

## Task 6: Final Commit

**Files:**
- All files changed in Tasks 1-5.

- [ ] **Step 1: Stage only planned files when the user explicitly approves committing**

Run:

```bash
git add \
  docs/superpowers/specs/2026/05/07/md-file-preview-dialog-design.md \
  docs/superpowers/plans/2026/05/07/md-file-preview-dialog-plan.md \
  packages/views/package.json \
  pnpm-lock.yaml \
  packages/views/editor/markdown-file-preview.tsx \
  packages/views/editor/readonly-content.test.tsx \
  packages/views/editor/extensions/file-card.test.tsx
```

Expected: only planned files are staged.

- [ ] **Step 2: Commit only after explicit user approval**

Run:

```bash
git commit -m "docs: plan markdown preview dialog resizing"
```

Expected: commit succeeds with only the design and implementation plan if code has not yet been implemented. If implementation is included later, use a feature commit such as `feat: add resizable markdown preview dialog`.
