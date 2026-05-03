import { beforeEach, describe, expect, it, vi } from "vitest";

const maxOpenMock = vi.hoisted(() => vi.fn((content: unknown) => ({ content })));

vi.mock("@tiptap/core", () => ({
  Extension: {
    create: (config: unknown) => ({ config }),
  },
}));

vi.mock("@tiptap/pm/state", () => ({
  Plugin: class MockPlugin {
    props: Record<string, unknown>;

    constructor(spec: { props: Record<string, unknown> }) {
      this.props = spec.props;
    }
  },
  PluginKey: class MockPluginKey {
    constructor(_name: string) {}
  },
}));

vi.mock("@tiptap/pm/model", () => ({
  Slice: {
    maxOpen: maxOpenMock,
  },
}));

import { createMarkdownPasteExtension } from "./markdown-paste";

function createHandlePaste(editor: Record<string, unknown>) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const extension = createMarkdownPasteExtension() as any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const plugins = extension.config.addProseMirrorPlugins.call({ editor } as any) as Array<{
    props: { handlePaste: (...args: unknown[]) => boolean };
  }>;
  const plugin = plugins[0];
  if (!plugin) throw new Error("No plugin found");
  return plugin.props.handlePaste;
}

function createClipboardEvent(text: string, html = "") {
  return {
    clipboardData: {
      files: { length: 0 },
      getData: (type: string) => {
        if (type === "text/plain") return text;
        if (type === "text/html") return html;
        return "";
      },
    },
  };
}

describe("markdown paste", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("inserts literal text inside code blocks instead of parsing Markdown", () => {
    const insertContent = vi.fn();
    const parse = vi.fn();
    const replaceSelection = vi.fn();
    const dispatch = vi.fn();
    const handlePaste = createHandlePaste({
      markdown: { parse },
      schema: { nodeFromJSON: vi.fn() },
      commands: { insertContent },
      isActive: vi.fn((name: string) => name === "codeBlock"),
    });

    const handled = handlePaste(
      {
        state: { tr: { replaceSelection } },
        dispatch,
      },
      createClipboardEvent("const x = 1;\n\nconst y = 2;"),
    );

    expect(handled).toBe(true);
    expect(insertContent).toHaveBeenCalledWith("const x = 1;\n\nconst y = 2;");
    expect(parse).not.toHaveBeenCalled();
    expect(replaceSelection).not.toHaveBeenCalled();
    expect(dispatch).not.toHaveBeenCalled();
  });

  it("parses Markdown normally outside code blocks", () => {
    const parse = vi.fn(() => ({ type: "doc", content: [] }));
    const nodeFromJSON = vi.fn(() => ({ content: { size: 3 } }));
    const replaceSelection = vi.fn(() => "transaction");
    const dispatch = vi.fn();
    const handlePaste = createHandlePaste({
      markdown: { parse },
      schema: { nodeFromJSON },
      commands: { insertContent: vi.fn() },
      isActive: vi.fn(() => false),
    });

    const handled = handlePaste(
      {
        state: { tr: { replaceSelection } },
        dispatch,
      },
      createClipboardEvent("# Title"),
    );

    expect(handled).toBe(true);
    expect(parse).toHaveBeenCalledWith("# Title");
    expect(nodeFromJSON).toHaveBeenCalledWith({ type: "doc", content: [] });
    expect(maxOpenMock).toHaveBeenCalledWith({ size: 3 });
    expect(replaceSelection).toHaveBeenCalledWith({ content: { size: 3 } });
    expect(dispatch).toHaveBeenCalledWith("transaction");
  });
});
