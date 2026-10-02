// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import type { Editor } from "@tiptap/react";
import type { PluginComposerCommandTarget } from "../plugins/plugin-composer-commands";
import { collectComposerCommands } from "../plugins/plugin-composer-commands";
import type { PluginInstallation } from "@multica/core/types";
import {
  capturePluginComposerInvocation,
  insertPluginComposerMarkdown,
  isPluginComposerInvocationInstalled,
} from "./plugin-composer-insertion";

function editorWithSlashText(text = "/sample") {
  const insertContentAt = vi.fn(() => ({ run: () => true }));
  const focus = vi.fn(() => ({ insertContentAt }));
  const doc = {
    content: { size: text.length },
    textBetween: (from: number, to: number) => text.slice(from, to),
  };
  const editor = {
    isDestroyed: false,
    state: { doc },
    chain: () => ({ focus }),
  } as unknown as Editor;
  return { editor, insertContentAt };
}

const target = {} as PluginComposerCommandTarget;

describe("plugin composer insertion", () => {
  it("refuses a disabled, removed, or upgraded installation after the modal opened", () => {
    const installation = {
      id: "install-1",
      plugin_key: "com.example.snippet",
      package_version_id: "version-1",
      enabled: true,
      composer_commands: [{ key: "sample", label: "sample", contexts: ["chat"], surface: "picker" }],
      surfaces: [{ key: "picker", type: "modal", name: "Picker", entry: "ui/main.js" }],
    } as PluginInstallation;
    const currentTarget = collectComposerCommands([installation], "chat", "web")[0]!;
    const invocation = capturePluginComposerInvocation(currentTarget, "ws-1", editorWithSlashText().editor, { from: 0, to: 7 });
    const isActive = (plugins: PluginInstallation[]) =>
      isPluginComposerInvocationInstalled(invocation, { plugins }, "chat", "web");

    expect(isActive([installation])).toBe(true);
    expect(isActive([{ ...installation, enabled: false }])).toBe(false);
    expect(isActive([{ ...installation, package_version_id: "version-2" }])).toBe(false);
    expect(isActive([])).toBe(false);
  });

  it("inserts Markdown only into the same unchanged editor and range", () => {
    const { editor, insertContentAt } = editorWithSlashText();
    const invocation = capturePluginComposerInvocation(target, "ws-1", editor, { from: 0, to: 7 });

    expect(insertPluginComposerMarkdown(invocation, editor, "## Notes\n\n- ")).toBe(true);
    expect(insertContentAt).toHaveBeenCalledWith(
      { from: 0, to: 7 },
      "## Notes\n\n- ",
      { contentType: "markdown" },
    );
  });

  it("refuses a different editor, destroyed editor, or any intervening document edit", () => {
    const { editor, insertContentAt } = editorWithSlashText();
    const invocation = capturePluginComposerInvocation(target, "ws-1", editor, { from: 0, to: 7 });

    expect(insertPluginComposerMarkdown(invocation, editorWithSlashText().editor, "text")).toBe(false);
    editor.state.doc = editorWithSlashText("/changed").editor.state.doc;
    expect(insertPluginComposerMarkdown(invocation, editor, "text")).toBe(false);
    editor.state.doc = invocation.doc;
    Object.defineProperty(editor, "isDestroyed", { value: true, configurable: true });
    expect(insertPluginComposerMarkdown(invocation, editor, "text")).toBe(false);
    expect(insertContentAt).not.toHaveBeenCalled();
  });
});
