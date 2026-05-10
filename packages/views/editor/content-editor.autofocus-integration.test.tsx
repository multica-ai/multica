import { describe, it, expect, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";

// Integration test for JEH-756. Earlier autofocus regressions slipped past
// because every chat-/comment-input test mocked `@tiptap/react` with a stub
// that exposed `focus()` synchronously. The real `useEditor({
// immediatelyRender: false })` is async — it returns `null` on first render
// and creates the editor in a deferred frame — so autofocus must run from
// ContentEditor after the real editor instance exists.
//
// This test does NOT mock `@tiptap/react`. It mounts ContentEditor with the
// real Tiptap stack and asserts `document.activeElement` lands inside the
// ProseMirror node when `autoFocus` is set.

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => "test-ws",
}));

// Strip the heavy production extension graph (mentions, file uploads,
// markdown paste, etc). The autofocus boundary doesn't depend on which
// extensions are loaded; StarterKit + Markdown is enough — the latter
// because `content-editor.tsx`'s `onCreate` calls `ed.getMarkdown()`,
// which is contributed by `@tiptap/markdown`.
vi.mock("./extensions", async () => {
  const StarterKit = (await import("@tiptap/starter-kit")).default;
  const { Markdown } = await import("@tiptap/markdown");
  return {
    createEditorExtensions: () => [StarterKit, Markdown],
  };
});

vi.mock("./extensions/file-upload", () => ({
  uploadAndInsertFile: vi.fn(),
}));

vi.mock("./utils/preprocess", () => ({
  preprocessMarkdown: (value: string) => value,
}));

vi.mock("./bubble-menu", () => ({
  EditorBubbleMenu: () => null,
}));

import { ContentEditor } from "./content-editor";

function createFocusedDialog() {
  const dialog = document.createElement("div");
  dialog.setAttribute("role", "dialog");
  const dialogInput = document.createElement("input");
  dialog.appendChild(dialogInput);
  document.body.appendChild(dialog);
  dialogInput.focus();
  return { dialog, dialogInput };
}

describe("ContentEditor — JEH-756 autofocus integration", () => {
  it("focus lands on the ProseMirror node when autoFocus is set", async () => {
    const { container } = render(<ContentEditor autoFocus placeholder="…" />);

    // Tiptap with `immediatelyRender: false` defers editor creation past
    // mount, so the ProseMirror node appears in a follow-up render. Wait
    // for it before asserting on activeElement.
    const proseMirror = await waitFor(() => {
      const node = container.querySelector(".ProseMirror");
      if (!node) throw new Error("ProseMirror not yet mounted");
      return node as HTMLElement;
    });

    await waitFor(() => {
      // `commands.focus("end")` lands focus on the editable contenteditable
      // node — that's what `activeElement` reports.
      expect(proseMirror.contains(document.activeElement)).toBe(true);
    });
  });

  it("does NOT focus the ProseMirror node when autoFocus is omitted", async () => {
    const { container } = render(<ContentEditor placeholder="…" />);

    const proseMirror = await waitFor(() => {
      const node = container.querySelector(".ProseMirror");
      if (!node) throw new Error("ProseMirror not yet mounted");
      return node as HTMLElement;
    });

    // Give the deferred autofocus effect room to run if it were enabled.
    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(proseMirror.contains(document.activeElement)).toBe(false);
  });

  it("focuses after a dismissing dialog closes before deferred focus", async () => {
    const { dialog } = createFocusedDialog();
    const { container } = render(<ContentEditor autoFocus placeholder="…" />);

    const proseMirror = await waitFor(() => {
      const node = container.querySelector(".ProseMirror");
      if (!node) throw new Error("ProseMirror not yet mounted");
      return node as HTMLElement;
    });

    dialog.remove();

    await waitFor(() => {
      expect(proseMirror.contains(document.activeElement)).toBe(true);
    });
  });

  it("focuses after a dismissing dialog closes during the retry window", async () => {
    const { dialog } = createFocusedDialog();
    const { container } = render(<ContentEditor autoFocus placeholder="…" />);

    const proseMirror = await waitFor(() => {
      const node = container.querySelector(".ProseMirror");
      if (!node) throw new Error("ProseMirror not yet mounted");
      return node as HTMLElement;
    });

    window.setTimeout(() => dialog.remove(), 100);

    await waitFor(() => {
      expect(proseMirror.contains(document.activeElement)).toBe(true);
    });
  });

  it("does not focus while an open dialog still owns focus", async () => {
    const { dialog, dialogInput } = createFocusedDialog();
    const { container } = render(<ContentEditor autoFocus placeholder="…" />);

    const proseMirror = await waitFor(() => {
      const node = container.querySelector(".ProseMirror");
      if (!node) throw new Error("ProseMirror not yet mounted");
      return node as HTMLElement;
    });

    await new Promise((resolve) => setTimeout(resolve, 850));

    expect(proseMirror.contains(document.activeElement)).toBe(false);
    expect(document.activeElement).toBe(dialogInput);

    dialog.remove();
  });
});
