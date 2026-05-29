/**
 * Integration test for the / skill picker vs the chat composer's
 * submitShortcut extension. In a chat-style composer (submitOnEnter=true),
 * pressing Enter while the `/` popup is open MUST insert the highlighted
 * skill — NOT submit the chat. The earlier priority-only fix (PR #629)
 * was insufficient because the suggestion plugin delegated Enter to a
 * React imperative ref, which is null until the popup component has been
 * mounted; in the test environment (no EditorContent) and in fast
 * keystroke timing in production, the ref never bound. This test boots a
 * real Tiptap editor with both extensions and verifies the behavior
 * end-to-end, not just the config value.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  Object.defineProperty(navigator, "platform", {
    value: "MacIntel",
    configurable: true,
  });
});

import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Mention from "@tiptap/extension-mention";
import { Extension } from "@tiptap/core";
import { QueryClient } from "@tanstack/react-query";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { createSkillMentionExtension } from "./extension";

vi.mock("@multica/core/platform", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/platform")>()),
  getCurrentWsId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("@multica/cerebro-feature-flags", async (importOriginal) => {
  const real =
    await importOriginal<typeof import("@multica/cerebro-feature-flags")>();
  return {
    ...real,
    useCerebroFeatureFlagsStore: Object.assign(vi.fn(), {
      getState: () => ({
        overrides: { cerebro_skill_mention: true },
      }),
    }),
  };
});

// Recreate the local submitShortcut extension. The real one lives in
// `@multica/views`, which depends on this package — depending back would be
// a cycle. The behavior we care about is identical: bind Enter at default
// priority to call onSubmit and return true.
function createTestSubmitExtension(onSubmit: () => boolean) {
  return Extension.create({
    name: "submitShortcut",
    addKeyboardShortcuts() {
      return {
        Enter: () => onSubmit(),
      };
    },
  });
}

function makeEditor(
  onSubmit: () => boolean,
  skills: { id: string; name: string; description: string }[] = [
    { id: "skill-1", name: "Release notes", description: "Draft notes" },
    { id: "skill-2", name: "Systematic debugging", description: "Trace causes" },
  ],
) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  qc.setQueryData(
    workspaceKeys.skills("ws-1"),
    skills.map((s) => ({
      ...s,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      created_by: null,
      owner_id: null,
      approver_ids: [],
      current_version: "1",
      workspace_id: "ws-1",
      config: {},
    })),
  );
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ codeBlock: false }),
      Mention,
      createSkillMentionExtension(qc),
      createTestSubmitExtension(onSubmit),
    ],
    content: { type: "doc", content: [{ type: "paragraph" }] },
  });
}

function dispatchKey(editor: Editor, key: string) {
  const event = new KeyboardEvent("keydown", {
    key,
    code: key === "Enter" ? "Enter" : key,
    bubbles: true,
    cancelable: true,
  });
  editor.view.dom.dispatchEvent(event);
}

describe("Enter while / popup is open", () => {
  let editor: Editor;

  beforeEach(() => {
    document.body.innerHTML = "";
  });

  afterEach(() => {
    editor?.destroy();
  });

  it("Enter without / does submit (baseline)", () => {
    const onSubmit = vi.fn(() => true);
    editor = makeEditor(onSubmit);
    editor.commands.focus();
    dispatchKey(editor, "Enter");
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("Enter while / popup is open does NOT submit", async () => {
    const onSubmit = vi.fn(() => true);
    editor = makeEditor(onSubmit);
    editor.commands.focus();
    editor.commands.insertContent("/");
    // Give the Suggestion plugin's view().update() a tick to fire onStart
    // and populate the closure-managed item list. No React mount required —
    // that's the whole point of the fix.
    await new Promise((r) => setTimeout(r, 10));
    dispatchKey(editor, "Enter");
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("Enter inserts the highlighted skill as a mention node", async () => {
    const onSubmit = vi.fn(() => true);
    editor = makeEditor(onSubmit);
    editor.commands.focus();
    editor.commands.insertContent("/");
    await new Promise((r) => setTimeout(r, 10));
    dispatchKey(editor, "Enter");

    const json = editor.getJSON();
    const paragraph = json.content?.[0];
    const mention: any = paragraph?.content?.find(
      (n: any) => n.type === "mention",
    );
    expect(mention).toBeDefined();
    // First item alphabetically is "Release notes". (The `type: "skill"`
    // attr is added by BaseMentionExtension in production; this test uses
    // raw @tiptap/extension-mention which doesn't declare it.)
    expect(mention?.attrs?.label).toBe("Release notes");
  });

  it("ArrowDown moves the highlight before Enter selects", async () => {
    const onSubmit = vi.fn(() => true);
    editor = makeEditor(onSubmit);
    editor.commands.focus();
    editor.commands.insertContent("/");
    await new Promise((r) => setTimeout(r, 10));
    dispatchKey(editor, "ArrowDown");
    dispatchKey(editor, "Enter");

    const json = editor.getJSON();
    const mention: any = json.content?.[0]?.content?.find(
      (n: any) => n.type === "mention",
    );
    // Second item alphabetically is "Systematic debugging".
    expect(mention?.attrs?.label).toBe("Systematic debugging");
  });
});
