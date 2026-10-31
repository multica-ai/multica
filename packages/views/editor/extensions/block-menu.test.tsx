import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { createRef } from "react";
import { act, render, screen } from "@testing-library/react";
import { Editor } from "@tiptap/core";

vi.mock("../../i18n", async () => {
  const editor: Record<string, unknown> = (await import("../../locales/en/editor.json")).default;
  const t = (select: (bundle: unknown) => unknown) => {
    const path: string[] = [];
    const recorder: unknown = new Proxy({}, { get: (_, key: string) => (path.push(key), recorder) });
    select(recorder);
    return String(path.reduce<unknown>((node, key) => (node as Record<string, unknown>)?.[key], editor) ?? "");
  };
  return { useT: () => ({ t }) };
});

import { createEditorExtensions } from ".";
import { BLOCK_MENU_ITEMS, BlockMenuList, runBlockMenuItem, type BlockMenuItem, type BlockMenuListRef } from "./block-menu";

let editor: Editor | null = null;

beforeAll(() => {
  Range.prototype.getClientRects = () => [] as unknown as DOMRectList;
  Range.prototype.getBoundingClientRect = () => new DOMRect();
});

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

function makeEditor(markdown: string): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  editor = new Editor({
    element,
    extensions: createEditorExtensions({
      placeholder: "",
      disableMentions: true,
      enableSlashCommands: true,
      slashCommandMode: "block",
      onUploadFileRef: { current: undefined },
    }),
    content: markdown,
    contentType: "markdown",
  });
  return editor;
}

/** The range of a typed `/query` at the end of the document's last block. */
function slashRangeAtEnd(ed: Editor, query: string) {
  const to = ed.state.doc.content.size - 1;
  return { from: to - query.length, to };
}

describe("block menu commands", () => {
  it("replaces the typed /query with a 3×3 table", () => {
    const ed = makeEditor("Intro\n\n/tab");
    runBlockMenuItem(ed, "table", slashRangeAtEnd(ed, "/tab"));
    const markdown = ed.getMarkdown();
    expect(markdown).not.toContain("/tab");
    expect(markdown.match(/^\|.*\|$/gm)).toHaveLength(4);
  });

  it("turns the current paragraph into a heading", () => {
    const ed = makeEditor("Plan /h2");
    runBlockMenuItem(ed, "heading2", slashRangeAtEnd(ed, " /h2"));
    expect(ed.getMarkdown().trim()).toBe("## Plan");
  });

  it("inserts a mermaid code block", () => {
    const ed = makeEditor("/mer");
    runBlockMenuItem(ed, "mermaid", slashRangeAtEnd(ed, "/mer"));
    expect(ed.getJSON().content?.[0]).toMatchObject({ type: "codeBlock", attrs: { language: "mermaid" } });
  });

  it("inserts a collapsible block with the caret in its summary", () => {
    const ed = makeEditor("/det");
    runBlockMenuItem(ed, "details", slashRangeAtEnd(ed, "/det"));
    ed.commands.insertContent("Full log");
    expect(ed.getMarkdown()).toContain("<summary>Full log</summary>");
  });
});

describe("BlockMenuList", () => {
  function renderList(query: string, items: BlockMenuItem[] = BLOCK_MENU_ITEMS) {
    const command = vi.fn();
    const ref = createRef<BlockMenuListRef>();
    render(<BlockMenuList ref={ref} items={items} query={query} command={command} />);
    const press = (key: string) => {
      let handled = false;
      act(() => {
        handled = ref.current!.onKeyDown({ event: new KeyboardEvent("keydown", { key }) });
      });
      return handled;
    };
    return { command, press };
  }

  it("shows block types as an icon row and selects the first insert", () => {
    const { command, press } = renderList("");
    expect(screen.getByText("Turn into")).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Heading 1" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /Divider/ })).toHaveAttribute("aria-selected", "true");
    press("Enter");
    expect(command).toHaveBeenCalledWith(expect.objectContaining({ id: "divider" }));
  });

  it("moves along the icon row with left and right", () => {
    const { command, press } = renderList("");
    press("ArrowUp");
    expect(screen.getByRole("option", { name: "Normal Text" })).toHaveAttribute("aria-selected", "true");
    expect(press("ArrowRight")).toBe(true);
    press("Enter");
    expect(command).toHaveBeenCalledWith(expect.objectContaining({ id: "heading1" }));
  });

  it("filters by label and keyword into one list", () => {
    renderList("h2");
    expect(screen.queryByText("Turn into")).toBeNull();
    expect(screen.getAllByRole("option").map((option) => option.textContent)).toEqual(["Heading 2"]);
  });

  it("renders nothing and lets Enter through when nothing matches", () => {
    const { press } = renderList("zzz");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(press("Enter")).toBe(false);
  });
});
