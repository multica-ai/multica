import { describe, it, expect, vi } from "vitest";
import { getExtensionField } from "@tiptap/core";
import type { Editor } from "@tiptap/core";
import { createSubmitExtension } from "./submit-shortcut";

function getShortcuts(
  ext: ReturnType<typeof createSubmitExtension>,
  editor: Partial<Editor>,
): Record<string, () => boolean> {
  const fn = getExtensionField<
    () => Record<string, () => boolean>
  >(ext, "addKeyboardShortcuts", {
    name: "submitShortcut",
    options: {},
    storage: {},
    editor: editor as Editor,
    type: null,
  });
  return fn?.() ?? {};
}

describe("createSubmitExtension", () => {
  const baseEditor = {
    view: { composing: false } as unknown as Editor["view"],
    isActive: () => false,
  } as Partial<Editor>;

  it("Mod-Enter always submits", () => {
    const onSubmit = vi.fn(() => true);
    const shortcuts = getShortcuts(
      createSubmitExtension(onSubmit, { submitOnEnterRef: { current: false } }),
      baseEditor,
    );

    expect(shortcuts["Mod-Enter"]).toBeDefined();
    shortcuts["Mod-Enter"]!();
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("bare Enter falls through when submitOnEnter is false", () => {
    const onSubmit = vi.fn(() => true);
    const shortcuts = getShortcuts(
      createSubmitExtension(onSubmit, { submitOnEnterRef: { current: false } }),
      baseEditor,
    );

    expect(shortcuts.Enter).toBeDefined();
    expect(shortcuts.Enter!()).toBe(false);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("bare Enter submits when submitOnEnter is true", () => {
    const onSubmit = vi.fn(() => true);
    const shortcuts = getShortcuts(
      createSubmitExtension(onSubmit, { submitOnEnterRef: { current: true } }),
      baseEditor,
    );

    expect(shortcuts.Enter).toBeDefined();
    expect(shortcuts.Enter!()).toBe(true);
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("reads submitOnEnter from the ref at keypress time", () => {
    const onSubmit = vi.fn(() => true);
    const submitOnEnterRef = { current: false };
    const shortcuts = getShortcuts(
      createSubmitExtension(onSubmit, { submitOnEnterRef }),
      baseEditor,
    );

    expect(shortcuts.Enter!()).toBe(false);
    expect(onSubmit).not.toHaveBeenCalled();

    submitOnEnterRef.current = true;

    expect(shortcuts.Enter!()).toBe(true);
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("Enter is suppressed during IME composition", () => {
    const onSubmit = vi.fn(() => true);
    const shortcuts = getShortcuts(
      createSubmitExtension(onSubmit, { submitOnEnterRef: { current: true } }),
      {
        view: { composing: true } as unknown as Editor["view"],
        isActive: () => false,
      },
    );

    expect(shortcuts.Enter!()).toBe(false);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("Enter is suppressed inside a code block", () => {
    const onSubmit = vi.fn(() => true);
    const shortcuts = getShortcuts(
      createSubmitExtension(onSubmit, { submitOnEnterRef: { current: true } }),
      {
        view: { composing: false } as unknown as Editor["view"],
        isActive: (name: string) => name === "codeBlock",
      },
    );

    expect(shortcuts.Enter!()).toBe(false);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("Enter is suppressed inside a list item", () => {
    const onSubmit = vi.fn(() => true);
    const shortcuts = getShortcuts(
      createSubmitExtension(onSubmit, { submitOnEnterRef: { current: true } }),
      {
        view: { composing: false } as unknown as Editor["view"],
        isActive: (name: string) => name === "listItem",
      },
    );

    expect(shortcuts.Enter!()).toBe(false);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("Enter is suppressed inside a blockquote", () => {
    const onSubmit = vi.fn(() => true);
    const shortcuts = getShortcuts(
      createSubmitExtension(onSubmit, { submitOnEnterRef: { current: true } }),
      {
        view: { composing: false } as unknown as Editor["view"],
        isActive: (name: string) => name === "blockquote",
      },
    );

    expect(shortcuts.Enter!()).toBe(false);
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
