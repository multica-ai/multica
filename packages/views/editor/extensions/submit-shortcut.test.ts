import { describe, it, expect, vi } from "vitest";
import { createSubmitExtension } from "./submit-shortcut";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

interface FakeEditor {
  view: { composing: boolean };
  isActive: (name: string) => boolean;
}

/**
 * Tiptap extensions expose `addKeyboardShortcuts` as a method on the
 * extension's options-bound config. We sidestep the framework here by
 * inspecting the config blob directly and invoking the handler with a
 * mocked editor — the unit under test is the keymap, not Tiptap itself.
 */
function getShortcuts(
  ext: ReturnType<typeof createSubmitExtension>,
  editor: FakeEditor,
): Record<string, () => boolean> {
  // Tiptap's Extension.create stores the user-supplied config under `config`.
  const cfg = (ext as unknown as { config: { addKeyboardShortcuts: () => Record<string, () => boolean> } }).config;
  return cfg.addKeyboardShortcuts.call({ editor } as unknown as ThisParameterType<typeof cfg.addKeyboardShortcuts>);
}

const idleEditor: FakeEditor = {
  view: { composing: false },
  isActive: () => false,
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("createSubmitExtension", () => {
  describe("submitOnEnter=false (default Cmd/Ctrl+Enter mode)", () => {
    it("registers Mod-Enter only — bare Enter keeps its native newline behaviour", () => {
      const ext = createSubmitExtension(() => true, { submitOnEnter: false });
      const shortcuts = getShortcuts(ext, idleEditor);
      expect(Object.keys(shortcuts).sort()).toEqual(["Mod-Enter"]);
    });

    it("Mod-Enter forwards the result of onSubmit so editor falls through on no-op", () => {
      const onSubmit = vi.fn(() => false);
      const ext = createSubmitExtension(onSubmit, { submitOnEnter: false });
      const shortcuts = getShortcuts(ext, idleEditor);
      expect(shortcuts["Mod-Enter"]!()).toBe(false);
      expect(onSubmit).toHaveBeenCalledTimes(1);
    });
  });

  describe("submitOnEnter=true (chat-style mode)", () => {
    it("registers bare Enter only — Mod-Enter and Shift-Enter fall through to hardBreak for a newline", () => {
      const ext = createSubmitExtension(() => true, { submitOnEnter: true });
      const shortcuts = getShortcuts(ext, idleEditor);
      expect(Object.keys(shortcuts).sort()).toEqual(["Enter"]);
    });

    it("Enter calls onSubmit and forwards its return value", () => {
      const onSubmit = vi.fn(() => true);
      const ext = createSubmitExtension(onSubmit, { submitOnEnter: true });
      const shortcuts = getShortcuts(ext, idleEditor);
      expect(shortcuts.Enter!()).toBe(true);
      expect(onSubmit).toHaveBeenCalledTimes(1);
    });

    it("Enter is a no-op while an IME composition session is active", () => {
      const onSubmit = vi.fn(() => true);
      const ext = createSubmitExtension(onSubmit, { submitOnEnter: true });
      const shortcuts = getShortcuts(ext, {
        view: { composing: true },
        isActive: () => false,
      });
      expect(shortcuts.Enter!()).toBe(false);
      expect(onSubmit).not.toHaveBeenCalled();
    });

    it("Enter is a no-op inside a code block (lets the editor insert a newline)", () => {
      const onSubmit = vi.fn(() => true);
      const ext = createSubmitExtension(onSubmit, { submitOnEnter: true });
      const shortcuts = getShortcuts(ext, {
        view: { composing: false },
        isActive: (name: string) => name === "codeBlock",
      });
      expect(shortcuts.Enter!()).toBe(false);
      expect(onSubmit).not.toHaveBeenCalled();
    });

    it("Enter still submits when isActive matches an unrelated mark like bold", () => {
      const onSubmit = vi.fn(() => true);
      const ext = createSubmitExtension(onSubmit, { submitOnEnter: true });
      const shortcuts = getShortcuts(ext, {
        view: { composing: false },
        isActive: (name: string) => name === "bold",
      });
      expect(shortcuts.Enter!()).toBe(true);
      expect(onSubmit).toHaveBeenCalledTimes(1);
    });
  });
});
