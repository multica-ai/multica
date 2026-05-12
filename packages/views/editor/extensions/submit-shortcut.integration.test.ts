/**
 * Integration tests for submit-shortcut.
 *
 * The sibling unit test (submit-shortcut.test.ts) only asserts which keys
 * are registered. These tests boot a real Tiptap editor with StarterKit
 * (which brings hardBreak) plus our submit extension, then dispatch the
 * actual keyboard events through ProseMirror. That's the only way to
 * confirm that JEH-1025's fix really lets Mod-Enter fall through to
 * hardBreak — keymap plugin ordering is a runtime concern that a
 * keys-only assertion can't catch.
 *
 * Platform note: prosemirror-keymap reads `navigator.platform` once at
 * module-load time to decide whether `Mod` means Cmd or Ctrl. We pin
 * jsdom to MacIntel BEFORE the tiptap imports run so `Mod` resolves to
 * Cmd (metaKey) for the whole file. Ctrl coverage lives in the unit
 * test — it's about keymap registration, not platform behavior.
 */
import { vi, describe, it, expect, beforeEach, afterEach } from "vitest";

// Pin platform BEFORE tiptap/prosemirror-keymap imports — `prosemirror-keymap`
// reads `navigator.platform` once at module load to decide whether `Mod`
// means Cmd or Ctrl. `vi.hoisted` runs before all imports.
vi.hoisted(() => {
  Object.defineProperty(navigator, "platform", {
    value: "MacIntel",
    configurable: true,
  });
});

import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { createSubmitExtension } from "./submit-shortcut";

interface JsonNode {
  type: string;
  text?: string;
  content?: JsonNode[];
}

function makeEditor(submitOnEnter: boolean, onSubmit: () => boolean) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ codeBlock: false }),
      createSubmitExtension(onSubmit, { submitOnEnter }),
    ],
    content: { type: "doc", content: [{ type: "paragraph" }] },
  });
}

function dispatchKey(
  editor: Editor,
  key: string,
  modifiers: { shift?: boolean; meta?: boolean; ctrl?: boolean } = {},
) {
  const event = new KeyboardEvent("keydown", {
    key,
    code: key === "Enter" ? "Enter" : key,
    shiftKey: modifiers.shift ?? false,
    metaKey: modifiers.meta ?? false,
    ctrlKey: modifiers.ctrl ?? false,
    bubbles: true,
    cancelable: true,
  });
  editor.view.dom.dispatchEvent(event);
  return event;
}

function hasHardBreak(json: JsonNode): boolean {
  if (json.type === "hardBreak") return true;
  return (json.content ?? []).some(hasHardBreak);
}

describe("submit-shortcut integration (real Tiptap + ProseMirror, Mac platform)", () => {
  let editor: Editor;

  beforeEach(() => {
    document.body.innerHTML = "";
  });

  afterEach(() => {
    editor?.destroy();
  });

  describe("submitOnEnter=true (chat-style mode)", () => {
    it("Enter fires onSubmit", () => {
      const onSubmit = vi.fn(() => true);
      editor = makeEditor(true, onSubmit);
      dispatchKey(editor, "Enter");
      expect(onSubmit).toHaveBeenCalledTimes(1);
    });

    it("Cmd+Enter inserts a hard break instead of submitting (JEH-1025)", () => {
      const onSubmit = vi.fn(() => true);
      editor = makeEditor(true, onSubmit);
      dispatchKey(editor, "Enter", { meta: true });
      expect(onSubmit).not.toHaveBeenCalled();
      expect(hasHardBreak(editor.getJSON() as JsonNode)).toBe(true);
    });

    it("Shift+Enter inserts a hard break", () => {
      const onSubmit = vi.fn(() => true);
      editor = makeEditor(true, onSubmit);
      dispatchKey(editor, "Enter", { shift: true });
      expect(onSubmit).not.toHaveBeenCalled();
      expect(hasHardBreak(editor.getJSON() as JsonNode)).toBe(true);
    });
  });

  describe("submitOnEnter=false (long-form composer)", () => {
    it("Cmd+Enter fires onSubmit", () => {
      const onSubmit = vi.fn(() => true);
      editor = makeEditor(false, onSubmit);
      dispatchKey(editor, "Enter", { meta: true });
      expect(onSubmit).toHaveBeenCalledTimes(1);
    });

    it("Shift+Enter still inserts a hard break (hardBreak's other binding)", () => {
      const onSubmit = vi.fn(() => true);
      editor = makeEditor(false, onSubmit);
      dispatchKey(editor, "Enter", { shift: true });
      expect(onSubmit).not.toHaveBeenCalled();
      expect(hasHardBreak(editor.getJSON() as JsonNode)).toBe(true);
    });

    it("bare Enter splits the paragraph (does not submit)", () => {
      const onSubmit = vi.fn(() => true);
      editor = makeEditor(false, onSubmit);
      dispatchKey(editor, "Enter");
      expect(onSubmit).not.toHaveBeenCalled();
      const doc = editor.getJSON() as JsonNode;
      const paragraphs = (doc.content ?? []).filter((n) => n.type === "paragraph");
      expect(paragraphs.length).toBe(2);
    });
  });
});
