import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";
import type { JSONContent } from "@tiptap/core";
import { NodeSelection, TextSelection } from "@tiptap/pm/state";
import { useEffect } from "react";
import { mockNativeDialog } from "../test-utils/native-dialog";
import { HTML_BLOCK_MESSAGE_KEY } from "../utils/html-block-document";

vi.mock("../../i18n", async () => {
  const editor = (await import("../../locales/en/editor.json")).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string) => select(editor),
    }),
  };
});

// jsdom resolves no theme, so the embed theme reports a scheme the test sets.
const embedScheme = vi.hoisted(() => ({ current: "light" as "light" | "dark" }));
vi.mock("../../common/embed-theme", () => ({
  readEmbedTheme: () => ({
    tokens: { "--color-scheme": embedScheme.current },
    colorScheme: embedScheme.current,
  }),
}));

vi.mock("../mermaid-diagram", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../mermaid-diagram")>()),
  MermaidDiagram: ({ chart }: { chart: string }) => <div data-testid="diagram">{chart}</div>,
}));

import { createEditorExtensions } from ".";

function TestEditor({ content, onEditor }: { content: JSONContent[]; onEditor: (editor: Editor) => void }) {
  // The editor's real extension set, so other keymaps get their turn first.
  const editor = useEditor({
    extensions: createEditorExtensions({ disableMentions: true }),
    content: { type: "doc", content },
  });
  useEffect(() => {
    if (editor) onEditor(editor);
  }, [editor, onEditor]);
  return <EditorContent editor={editor} />;
}

const paragraph = (text: string): JSONContent => ({ type: "paragraph", content: [{ type: "text", text }] });
const codeBlock = (language: string, text: string, meta: string | null = null): JSONContent => ({
  type: "codeBlock",
  attrs: { language, meta },
  content: text ? [{ type: "text", text }] : [],
});

/** "before" paragraph (8) puts the block at 8; its text starts at 9. */
const BLOCK_POS = 8;

async function mount(block: JSONContent, around: { before?: JSONContent[]; after?: JSONContent[] } = {}) {
  let editor: Editor | undefined;
  const content = [...(around.before ?? [paragraph("before")]), block, ...(around.after ?? [paragraph("after")])];
  render(<TestEditor content={content} onEditor={(e) => (editor = e)} />);
  await waitFor(() => expect(document.querySelector("[data-dynamic-code-block], pre")).not.toBeNull());
  return editor!;
}

function frame(): HTMLIFrameElement {
  const iframe = document.querySelector<HTMLIFrameElement>('[data-dynamic-block="html"] iframe');
  expect(iframe).not.toBeNull();
  return iframe!;
}

function sourceRegion(): HTMLElement {
  return document.querySelector<HTMLElement>("[data-dynamic-block-source]")!;
}

function postHeight(height: number) {
  act(() => {
    window.dispatchEvent(
      new MessageEvent("message", {
        data: { [HTML_BLOCK_MESSAGE_KEY]: 1, type: "height", height },
        source: frame().contentWindow,
      }),
    );
  });
}

function press(editor: Editor, key: string) {
  return act(async () => {
    fireEvent.keyDown(editor.view.dom, { key });
  });
}

function select(editor: Editor, pos: number) {
  return act(async () => {
    editor.commands.setTextSelection(pos);
  });
}

/** Top-level node texts, so a join or a lost paragraph shows. */
function blocks(editor: Editor): string[] {
  const out: string[] = [];
  editor.state.doc.forEach((node) => out.push(`${node.type.name}:${node.textContent}`));
  return out;
}

function blockMeta(editor: Editor): string | null {
  return editor.state.doc.nodeAt(BLOCK_POS)?.attrs.meta ?? null;
}

const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 300)));

let restoreDialog: () => void;
beforeEach(() => {
  window.sessionStorage.clear();
  restoreDialog = mockNativeDialog();
});
afterEach(() => {
  cleanup();
  restoreDialog();
  embedScheme.current = "light";
  document.documentElement.classList.remove("dark");
  vi.restoreAllMocks();
});

describe("CodeBlockView — ```html in the editor", () => {
  it("draws the dynamic block: a title bar with the fence title over the preview", async () => {
    await mount(codeBlock("html", "<p>hi</p>", 'title="Weekly p95"'));
    expect(screen.getByRole("button", { name: "Weekly p95" })).toBeTruthy();
    expect(screen.getByText("HTML")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Preview" }).getAttribute("aria-selected")).toBe("true");
    expect(frame().getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame().className).not.toContain("h-[480px]");
    // The editable source stays mounted under the preview, only hidden.
    const code = sourceRegion().querySelector("pre code");
    expect(code?.textContent).toBe("<p>hi</p>");
    expect(sourceRegion().hidden).toBe(true);
  });

  it("names the block after its kind when the fence has no title", async () => {
    await mount(codeBlock("html", "<p>hi</p>"));
    expect(screen.getByRole("button", { name: "HTML" })).toBeTruthy();
  });

  it("takes its content's height and collapses a tall document", async () => {
    await mount(codeBlock("html", "<p>hi</p>"));
    postHeight(200);
    expect(frame().parentElement!.style.height).toBe("200px");
    expect(screen.queryByRole("button", { name: "Show all" })).toBeNull();

    postHeight(900);
    expect(frame().parentElement!.style.height).toBe("900px");
    expect(document.querySelector<HTMLElement>("[data-collapsed]")!.style.maxHeight).toBe("480px");
    fireEvent.click(screen.getByRole("button", { name: "Show all" }));
    expect(document.querySelector("[data-collapsed]")).toBeNull();
  });

  it("edits in the source view; the hidden preview catches up when shown", async () => {
    const editor = await mount(codeBlock("html", "<p>first</p>"));
    const original = frame().getAttribute("srcdoc");
    fireEvent.click(screen.getByRole("tab", { name: "Source" }));
    expect(sourceRegion().hidden).toBe(false);
    expect(frame().closest("[hidden]")).not.toBeNull();

    act(() => {
      editor.view.dispatch(editor.state.tr.insertText("<b>new</b>", BLOCK_POS + 1));
    });
    await settle();
    // Not rebuilt while nobody can see it.
    expect(frame().getAttribute("srcdoc")).toBe(original);

    fireEvent.click(screen.getByRole("tab", { name: "Preview" }));
    await waitFor(() => expect(frame().getAttribute("srcdoc")).toContain("<b>new</b><p>first</p>"));
  });

  it("enlarges the latest source from the source view", async () => {
    const editor = await mount(codeBlock("html", "<p>first</p>"));
    fireEvent.click(screen.getByRole("tab", { name: "Source" }));
    act(() => {
      editor.view.dispatch(editor.state.tr.insertText("<i>x</i>", BLOCK_POS + 1));
    });
    await settle();
    fireEvent.click(screen.getByRole("button", { name: "Fullscreen" }));
    await waitFor(() =>
      expect(screen.getByRole("dialog").querySelector("iframe")?.getAttribute("srcdoc")).toContain("<i>x</i>"),
    );
  });

  it("opens an empty block in the source view", async () => {
    await mount(codeBlock("html", ""));
    expect(screen.getByRole("tab", { name: "Source" }).getAttribute("aria-selected")).toBe("true");
    expect(sourceRegion().hidden).toBe(false);
  });

  it("renames from the title bar and keeps the rest of the info string", async () => {
    const editor = await mount(codeBlock("html", "<p>hi</p>", 'title="Weekly p95" theme=dark'));
    fireEvent.click(screen.getByRole("button", { name: "Weekly p95" }));
    const input = screen.getByRole("textbox", { name: "Title" }) as HTMLInputElement;
    expect(input.value).toBe("Weekly p95");
    fireEvent.change(input, { target: { value: "Daily p95" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(blockMeta(editor)).toBe('title="Daily p95" theme=dark');
    expect(await screen.findByRole("button", { name: "Daily p95" })).toBeTruthy();
  });

  it("adds a title to an untitled block, and Escape discards a rename", async () => {
    const editor = await mount(codeBlock("html", "<p>hi</p>"));
    fireEvent.click(screen.getByRole("button", { name: "HTML" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Title" }), { target: { value: "Funnel" } });
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Title" }), { key: "Enter" });
    expect(blockMeta(editor)).toBe('title="Funnel"');

    fireEvent.click(await screen.findByRole("button", { name: "Funnel" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Title" }), { target: { value: "Other" } });
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Title" }), { key: "Escape" });
    expect(blockMeta(editor)).toBe('title="Funnel"');
    expect(screen.getByRole("button", { name: "Funnel" })).toBeTruthy();
  });

  it("selects a previewed block whole instead of putting the caret in its hidden source", async () => {
    const editor = await mount(codeBlock("html", "<p>hi</p>"));
    await act(async () => {
      editor.commands.setTextSelection(BLOCK_POS + 3);
    });
    const { selection } = editor.state;
    expect(selection).toBeInstanceOf(NodeSelection);
    expect(selection.from).toBe(BLOCK_POS);

    // Enter opens the source with the caret at its start.
    await act(async () => {
      fireEvent.keyDown(editor.view.dom, { key: "Enter" });
    });
    expect(screen.getByRole("tab", { name: "Source" }).getAttribute("aria-selected")).toBe("true");
    expect(editor.state.selection).toBeInstanceOf(TextSelection);
    expect(editor.state.selection.from).toBe(BLOCK_POS + 1);
    expect(editor.state.doc.nodeAt(BLOCK_POS)?.textContent).toBe("<p>hi</p>");
  });

  it("selects a previewed block whole when the arrow keys reach it", async () => {
    const editor = await mount(codeBlock("html", "<p>hi</p>"));
    // jsdom has no layout; every caret here sits on its textblock's edge.
    vi.spyOn(editor.view, "endOfTextblock").mockReturnValue(true);

    await act(async () => {
      editor.commands.setTextSelection(3);
    });
    await press(editor, "ArrowDown");
    expect(editor.state.selection).toBeInstanceOf(NodeSelection);
    expect(editor.state.selection.from).toBe(BLOCK_POS);

    const after = BLOCK_POS + editor.state.doc.nodeAt(BLOCK_POS)!.nodeSize + 1;
    await act(async () => {
      editor.commands.setTextSelection(after);
    });
    await press(editor, "ArrowUp");
    expect(editor.state.selection).toBeInstanceOf(NodeSelection);
    expect(editor.state.selection.from).toBe(BLOCK_POS);

    // A block showing its source is ordinary text to move through.
    fireEvent.click(screen.getByRole("tab", { name: "Source" }));
    await act(async () => {
      editor.commands.setTextSelection(3);
    });
    await press(editor, "ArrowDown");
    expect(editor.state.selection).toBeInstanceOf(TextSelection);
  });

  it("selects a previewed block on Backspace or Delete instead of joining its hidden source", async () => {
    const editor = await mount(codeBlock("html", "<p>hi</p>"));
    const original = blocks(editor);
    const afterStart = BLOCK_POS + editor.state.doc.nodeAt(BLOCK_POS)!.nodeSize + 1;

    // Backspace at the start of the next paragraph would append it to the source.
    await select(editor, afterStart);
    await press(editor, "Backspace");
    expect(editor.state.selection).toBeInstanceOf(NodeSelection);
    expect(editor.state.selection.from).toBe(BLOCK_POS);
    expect(blocks(editor)).toEqual(original);

    // Delete at the end of the previous paragraph would pull the source into it.
    await select(editor, BLOCK_POS - 1);
    await press(editor, "Delete");
    expect(editor.state.selection).toBeInstanceOf(NodeSelection);
    expect(editor.state.selection.from).toBe(BLOCK_POS);
    expect(blocks(editor)).toEqual(original);
  });

  it("removes an empty paragraph beside a previewed block and selects the block", async () => {
    const empty: JSONContent = { type: "paragraph" };
    const editor = await mount(codeBlock("html", "<p>hi</p>"), {
      before: [paragraph("before"), empty],
      after: [empty, paragraph("after")],
    });
    const blockPos = BLOCK_POS + 2;
    const blockSize = editor.state.doc.nodeAt(blockPos)!.nodeSize;

    await select(editor, blockPos + blockSize + 1);
    await press(editor, "Backspace");
    expect(blocks(editor)).toEqual(["paragraph:before", "paragraph:", "codeBlock:<p>hi</p>", "paragraph:after"]);
    expect(editor.state.selection).toBeInstanceOf(NodeSelection);
    expect(editor.state.selection.from).toBe(blockPos);

    await select(editor, BLOCK_POS + 1);
    await press(editor, "Delete");
    expect(blocks(editor)).toEqual(["paragraph:before", "codeBlock:<p>hi</p>", "paragraph:after"]);
    expect(editor.state.selection).toBeInstanceOf(NodeSelection);
    expect(editor.state.selection.from).toBe(BLOCK_POS);
  });

  it("moves the caret out of the source when the preview comes back", async () => {
    const editor = await mount(codeBlock("html", "<p>hi</p>"));
    fireEvent.click(screen.getByRole("tab", { name: "Source" }));
    await act(async () => {
      editor.commands.setTextSelection(BLOCK_POS + 3);
    });
    expect(editor.state.selection).toBeInstanceOf(TextSelection);

    await act(async () => {
      fireEvent.click(screen.getByRole("tab", { name: "Preview" }));
    });
    expect(editor.state.selection).toBeInstanceOf(NodeSelection);
  });

  it("keeps the document of HTML that reads no token across a theme switch", async () => {
    await mount(codeBlock("html", '<input value="initial">'));
    const iframe = frame();
    const before = iframe.getAttribute("srcdoc");
    expect(before).toMatch(/^<style>:root\{\}<\/style>/);

    embedScheme.current = "dark";
    await act(async () => {
      document.documentElement.classList.add("dark");
    });
    expect(frame()).toBe(iframe);
    expect(iframe.getAttribute("srcdoc")).toBe(before);
  });

  it("rebuilds the document of HTML that reads a token when the theme switches", async () => {
    await mount(codeBlock("html", '<p style="color: var(--foreground)">hi</p>'));
    expect(frame().getAttribute("srcdoc")).toContain("color-scheme:light");

    embedScheme.current = "dark";
    await act(async () => {
      document.documentElement.classList.add("dark");
    });
    await waitFor(() => expect(frame().getAttribute("srcdoc")).toContain("color-scheme:dark"));
  });
});

describe("CodeBlockView — other languages", () => {
  it("draws ```mermaid in the same frame", async () => {
    await mount(codeBlock("mermaid", "graph TD; A-->B", 'title="Request flow"'));
    expect(document.querySelector('[data-dynamic-block="mermaid"]')).not.toBeNull();
    expect(screen.getByRole("button", { name: "Request flow" })).toBeTruthy();
    expect(screen.getByText("Mermaid")).toBeTruthy();
    expect(screen.getByTestId("diagram").textContent).toBe("graph TD; A-->B");
  });

  it("keeps plain code as an editable block with a copy button", async () => {
    await mount(codeBlock("typescript", "const x = 1;"));
    expect(document.querySelector("[data-dynamic-block]")).toBeNull();
    expect(screen.getByRole("button", { name: "Copy code" })).toBeTruthy();
    expect(document.querySelector("pre code")?.textContent).toBe("const x = 1;");
  });
});
