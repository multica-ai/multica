import { describe, it, expect, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";

// Integration test for JEH-756. Earlier autofocus regressions slipped past
// because every chat-/comment-input test mocked `@tiptap/react` with a stub
// that exposed `focus()` synchronously. The real `useEditor({
// immediatelyRender: false })` is async — it returns `null` on first render
// and creates the editor in a deferred frame — so post-mount RAF effects
// calling `editor.commands.focus()` no-op'd silently in production.
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

    // Give Tiptap room to autofocus if it were going to.
    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(proseMirror.contains(document.activeElement)).toBe(false);
  });
});
