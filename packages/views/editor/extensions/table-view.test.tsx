import { afterEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";

const copyText = vi.hoisted(() => vi.fn(async () => true));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText }));

vi.mock("../../issues/hooks", () => ({
  useResolveIssueIdentifier: () => null,
}));

vi.mock("../../i18n", async () => {
  const editor: Record<string, Record<string, unknown>> = (await import("../../locales/en/editor.json")).default;
  const t = (select: (bundle: unknown) => unknown, values: Record<string, unknown> = {}) => {
    const path: string[] = [];
    const recorder: unknown = new Proxy({}, { get: (_, key: string) => (path.push(key), recorder) });
    select(recorder);
    const [section = "", key = ""] = path;
    const strings = editor[section] ?? {};
    const plural = values.count === 1 ? `${key}_one` : `${key}_other`;
    const template = String(strings[key] ?? strings[plural] ?? "");
    return template.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(values[name] ?? ""));
  };
  return { useT: () => ({ t }), useTimeAgo: () => "just now" };
});

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: vi.fn() },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
  useWorkspaceSlug: () => "acme",
}));

vi.mock("../../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId, fallbackLabel }: { issueId: string; fallbackLabel?: string }) => (
    <span>{fallbackLabel ?? issueId}</span>
  ),
}));

vi.mock("../../projects/components/project-mention-card", () => ({
  ProjectMentionCard: () => <span />,
}));

vi.mock("../link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

import { createEditorExtensions } from ".";

const TABLE = [
  "| GitHub | Relation | Count |",
  "| --- | --- | --- |",
  "| harness switch | tracks it | 10 |",
  "| agent registry | see `code` | 9 |",
].join("\n");

let current: Editor | null = null;

function TestEditor({ content = TABLE }: { content?: string }) {
  const editor = useEditor({
    extensions: createEditorExtensions({
      placeholder: "",
      disableMentions: true,
      onUploadFileRef: { current: undefined },
    }),
    content,
    contentType: "markdown",
    immediatelyRender: true,
  });
  current = editor;
  return <EditorContent editor={editor} className="rich-text-editor" />;
}

async function renderEditor(content?: string, column = "Relation") {
  const view = render(<TestEditor content={content} />);
  const handle = await screen.findByRole("separator", { name: `Resize ${column} column` });
  const table = view.container.querySelector<HTMLTableElement>(".rich-table table");
  if (!table || !current) throw new Error("table not rendered");
  return { ...view, table, handle, editor: current };
}

afterEach(() => {
  current?.destroy();
  current = null;
  window.localStorage.clear();
  copyText.mockClear();
});

describe("editor TableView", () => {
  it("puts ProseMirror's rows inside the shared table frame", async () => {
    const { table } = await renderEditor();
    expect(table.firstElementChild?.tagName).toBe("COLGROUP");
    expect(table.querySelector<HTMLTableSectionElement>(":scope > tbody")?.rows).toHaveLength(3);
    expect(table.rows[0]?.cells[1]?.tagName).toBe("TH");
  });

  it("resizes columns without editing the document, and keeps widths while typing", async () => {
    const { table, handle, editor } = await renderEditor();
    const before = editor.getMarkdown();

    fireEvent.keyDown(handle, { key: "ArrowRight", shiftKey: true });
    await waitFor(() => expect(table).toHaveAttribute("data-column-widths"));
    expect(table.querySelectorAll("colgroup col")).toHaveLength(3);
    expect(editor.getMarkdown()).toBe(before);

    // Typing changes the table's text, and with it the key its widths are
    // stored under; the widths stay.
    let cellPos = -1;
    editor.state.doc.descendants((node, pos) => {
      if (cellPos < 0 && node.type.name === "paragraph" && node.textContent === "tracks it") cellPos = pos + 1;
    });
    act(() => {
      editor.chain().insertContentAt(cellPos, "now ").run();
    });
    expect(editor.getMarkdown()).toContain("now tracks it");
    expect(table).toHaveAttribute("data-column-widths");
    expect(Object.keys(JSON.parse(window.localStorage.getItem("multica:rich-table-widths") ?? "{}"))).toHaveLength(1);
  });

  it("copies the table's Markdown", async () => {
    await renderEditor();
    await userEvent.click(screen.getByRole("button", { name: "Copy as Markdown" }));
    expect(copyText).toHaveBeenCalledWith(expect.stringContaining("| GitHub"));
    expect(copyText).toHaveBeenCalledWith(expect.stringContaining("`code`"));
  });

  it("filters and exports mentions as they display", async () => {
    await renderEditor(
      [
        "| Owner | Issue |",
        "| --- | --- |",
        "| [@Alice](mention://member/u1) | [DEV-1](mention://issue/i1) |",
        "| [@Bob](mention://member/u2) | none |",
      ].join("\n"),
      "Owner",
    );
    await userEvent.click(screen.getByRole("button", { name: "Expand table" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Copy as CSV" }));
    expect(copyText).toHaveBeenCalledWith("Owner,Issue\n@Alice,DEV-1\n@Bob,none");

    await userEvent.type(within(dialog).getByRole("searchbox"), "Alice");
    expect(within(dialog).getAllByRole("row")).toHaveLength(2);
  });

  it("expands into the dialog with the cells rendered", async () => {
    await renderEditor();
    await userEvent.click(screen.getByRole("button", { name: "Expand table" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.querySelector("code")?.textContent).toBe("code");
    expect(dialog).toHaveTextContent("harness switch");
  });
});
