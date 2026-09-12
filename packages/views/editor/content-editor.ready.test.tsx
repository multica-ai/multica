import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRef, Profiler, StrictMode, type ReactNode } from "react";
import type { Editor } from "@tiptap/react";
import { MarkdownManager } from "@tiptap/markdown";
import { ContentEditor, type ContentEditorRef } from "./content-editor";
import { MARKDOWN_CHUNK_THRESHOLD } from "./utils/parse-markdown-chunked";

vi.mock("../i18n", () => ({ useT: () => ({ t: () => "" }) }));

const long = Array.from({ length: 500 }, (_, i) => `Paragraph ${i}. Long cached description.`).join("\n\n");
const cases = [
  { name: "empty", markdown: "", selector: "p", count: 1, first: "", last: "" },
  { name: "short", markdown: "Short description.", selector: "p", count: 1, first: "Short description.", last: "Short description." },
  { name: "chunked", markdown: long, selector: "p", count: 500, first: "Paragraph 0.", last: "Paragraph 499." },
  { name: "mixed", markdown: "# Heading\n\nParagraph.\n\n- First\n- Second\n\n```ts\nconst ready = true;\n```", selector: "h1, li, pre", count: 4, first: "Heading", last: "const ready = true;" },
  { name: "image", markdown: "Before image.\n\n![Image](https://example.test/image.png)\n\nAfter image.", selector: "img", count: 1, first: "Before image.", last: "After image." },
];

function host(children: ReactNode) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>;
}

function mountedEditor(): Editor {
  return (document.querySelector(".ProseMirror") as HTMLElement & { editor: Editor }).editor;
}

afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

describe("ContentEditor initial document (real Tiptap)", () => {
  // The content matrix belongs here. Paint/geometry are covered by Playwright,
  // not JSDOM; IssueDetail only tests eager wiring and issue identity.
  it.each(cases)("commits $name content before create and reports connected, usable DOM once", async ({ markdown, selector, count, first, last }) => {
    vi.useFakeTimers();
    const parse = vi.spyOn(MarkdownManager.prototype, "parse");
    const snapshots: string[] = [];
    const ref = createRef<ContentEditorRef>();
    const onReady = vi.fn(() => {
      const dom = document.querySelector(".ProseMirror")!;
      expect(dom.isConnected).toBe(true);
      expect(dom).toHaveAttribute("contenteditable", "true");
      snapshots.push(dom.innerHTML);
      expect(ref.current).not.toBeNull();
    });
    render(host(<StrictMode><ContentEditor ref={ref} value={markdown} onReady={onReady} showBubbleMenu={false} eagerClientRender /></StrictMode>));
    await act(async () => {});
    const dom = document.querySelector(".ProseMirror")!;
    expect(dom.textContent).toContain(first);
    expect(dom.textContent).toContain(last);
    expect(dom.querySelectorAll(selector)).toHaveLength(count);
    expect(onReady).not.toHaveBeenCalled();
    const beforeCreate = dom.innerHTML;
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(onReady).toHaveBeenCalledTimes(1);
    expect(snapshots).toEqual([beforeCreate]);
    expect(mountedEditor().isInitialized).toBe(true);
    expect(long.length).toBeGreaterThan(MARKDOWN_CHUNK_THRESHOLD);
    if (markdown === long) {
      expect(parse).toHaveBeenCalled();
      expect(parse.mock.calls.every(([chunk]) => chunk.length < MARKDOWN_CHUNK_THRESHOLD)).toBe(true);
    }
  });

  it("retains and flushes the first edit made before create", async () => {
    vi.useFakeTimers();
    const onUpdate = vi.fn();
    const view = render(host(<ContentEditor value={long} onUpdate={onUpdate} flushPendingOnUnmount debounceMs={1500} showBubbleMenu={false} eagerClientRender />));
    expect(mountedEditor().isInitialized).toBe(false);
    act(() => { mountedEditor().commands.insertContent("FIRSTEDIT "); });
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(mountedEditor().getMarkdown()).toContain("FIRSTEDIT");
    view.unmount();
    expect(onUpdate).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("FIRSTEDIT"), long);
  });

  it("retains the first upload inserted before create", async () => {
    vi.useFakeTimers();
    const ref = createRef<ContentEditorRef>();
    const file = new File(["first"], "first-drop.txt", { type: "text/plain" });
    const upload = vi.fn(async () => ({
      id: "upload-1", workspace_id: "ws-1", issue_id: null, comment_id: null,
      chat_session_id: null, chat_message_id: null, uploader_type: "member", uploader_id: "user-1",
      filename: file.name, content_type: file.type, size_bytes: file.size, created_at: "2026-09-01T00:00:00Z",
      url: "/file.txt", download_url: "/file.txt", markdown_url: "/file.txt", link: "/file.txt", markdownLink: "/file.txt",
    }));
    render(host(<ContentEditor ref={ref} value={long} onUploadFile={upload} showBubbleMenu={false} eagerClientRender />));
    expect(mountedEditor().isInitialized).toBe(false);
    await act(async () => { ref.current!.uploadFile(file); });
    await act(() => vi.advanceTimersByTimeAsync(20));
    expect(upload).toHaveBeenCalledTimes(1);
    expect(mountedEditor().getMarkdown()).toContain("first-drop.txt");
    expect(mountedEditor().getMarkdown()).toContain("Paragraph 499.");
  });

  it("does not add a readiness render for a consumer without onReady", async () => {
    vi.useFakeTimers();
    const commit = vi.fn();
    render(host(<Profiler id="editor" onRender={commit}><ContentEditor value="Short." showBubbleMenu={false} eagerClientRender /></Profiler>));
    commit.mockClear();
    await act(() => vi.advanceTimersByTimeAsync(1));
    expect(mountedEditor().isInitialized).toBe(true);
    expect(commit).not.toHaveBeenCalled();
  });

  it("adopts the client editor when hydrating a null server snapshot", async () => {
    const { hydrateRoot } = await import("react-dom/client");
    const container = document.createElement("div");
    document.body.append(container);
    const onReady = vi.fn();
    const recoverable = vi.fn();
    let root: ReturnType<typeof hydrateRoot>;
    await act(async () => {
      root = hydrateRoot(container, host(<ContentEditor value={long} onReady={onReady} showBubbleMenu={false} eagerClientRender />), { onRecoverableError: recoverable });
    });
    await waitFor(() => expect(onReady).toHaveBeenCalledTimes(1));
    expect(container.querySelector(".ProseMirror")?.textContent).toContain("Paragraph 499.");
    expect(recoverable).not.toHaveBeenCalled();
    act(() => root!.unmount());
    container.remove();
  });
});
