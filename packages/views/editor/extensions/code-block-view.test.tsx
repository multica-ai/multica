import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { mockNativeDialog } from "../test-utils/native-dialog";

// Tiptap's NodeView primitives are hard to instantiate in jsdom without a
// full editor. Stub them so the test can render <CodeBlockView /> as a plain
// React component and inspect the resulting DOM shape.
vi.mock("@tiptap/react", () => {
  const NodeViewWrapper = ({ children, ...rest }: any) => (
    <div data-testid="nvw" {...rest}>
      {children}
    </div>
  );
  // The real NodeViewContent renders an element managed by ProseMirror. For
  // the test it's enough to surface a sentinel element so we can assert it
  // remains mounted while CSS-hidden.
  const NodeViewContent = ({ as = "div", ...rest }: any) => {
    const Tag = as;
    return <Tag data-testid="nvc" {...rest} />;
  };
  return { NodeViewWrapper, NodeViewContent };
});

vi.mock("../mermaid-diagram", () => ({
  MermaidDiagram: () => null,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        code_block: {
          html_preview: "Localized HTML preview",
          copy_code: "Copy code",
          show_preview: "Show preview",
          show_source: "Show source",
          fullscreen: "Fullscreen",
          close_preview: "Close",
        },
        attachment: { reload: "Reload" },
      }),
  }),
}));

import { CodeBlockView } from "./code-block-view";

function makeProps(language: string, text: string) {
  return {
    node: {
      attrs: { language },
      textContent: text,
    },
  } as unknown as Parameters<typeof CodeBlockView>[0];
}

describe("CodeBlockView — html language toggle", () => {
  let restoreDialog: () => void;
  // Inner async timers in useDebouncedValue make the iframe srcDoc lag by
  // ~200ms; use fake timers so the test stays deterministic.
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    restoreDialog = mockNativeDialog();
  });
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    restoreDialog();
  });

  it("defaults to preview view: renders an iframe with sandbox='allow-scripts' and keeps the <pre> mounted (hidden)", () => {
    render(<CodeBlockView {...makeProps("html", "<p>hello</p>")} />);
    act(() => {
      vi.advanceTimersByTime(250);
    });
    const frame = document.querySelector("iframe");
    expect(frame).toBeTruthy();
    expect(frame?.getAttribute("title")).toBe("Localized HTML preview");
    expect(frame?.getAttribute("sandbox")).toBe("allow-scripts");
    // NodeViewContent (and its enclosing <pre>) MUST remain mounted —
    // unmounting would break Tiptap's bindings and prevent editing.
    const nvc = screen.getByTestId("nvc");
    expect(nvc).toBeTruthy();
    const pre = nvc.closest("pre");
    expect(pre).toBeTruthy();
    expect(pre?.className).toContain("sr-only");
  });

  it("toggles to source view without unloading the preview or editor content", () => {
    render(<CodeBlockView {...makeProps("html", "<p>hello</p>")} />);
    act(() => {
      vi.advanceTimersByTime(250);
    });
    const frame = document.querySelector("iframe");
    expect(frame).toBeTruthy();
    const toggle = screen.getByRole("button", { name: "Show source" });
    fireEvent.click(toggle);
    expect(document.querySelector("iframe")).toBe(frame);
    expect(frame?.closest("[hidden]")).toBeTruthy();
    const nvc = screen.getByTestId("nvc");
    const pre = nvc.closest("pre")!;
    expect(pre.className).not.toContain("sr-only");
  });

  it("opens the current HTML in a larger sandboxed preview without replacing editor content", () => {
    const { rerender } = render(
      <CodeBlockView {...makeProps("html", "<p>first</p>")} />,
    );
    act(() => {
      vi.advanceTimersByTime(250);
    });
    const editorContent = screen.getByTestId("nvc");
    rerender(<CodeBlockView {...makeProps("html", "<p>latest</p>")} />);
    act(() => {
      vi.advanceTimersByTime(250);
    });
    const original = document.querySelector("iframe");
    const opener = screen.getByRole("button", { name: "Fullscreen" });
    fireEvent.click(opener);
    const dialog = screen.getByRole("dialog", {
      name: "Localized HTML preview",
    });
    const frame = dialog.querySelector("iframe");
    expect(frame).toBe(original);
    expect(document.querySelectorAll("iframe")).toHaveLength(1);
    expect(frame?.getAttribute("srcdoc")).toBe("<p>latest</p>");
    expect(frame?.getAttribute("sandbox")).toBe("allow-scripts");
    expect(screen.getByTestId("nvc")).toBe(editorContent);
    expect(document.querySelector("iframe")).toBe(original);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    act(() => {
      vi.advanceTimersByTime(250);
    });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByTestId("nvc")).toBe(editorContent);
    fireEvent.click(opener);
    expect(screen.getByRole("dialog").querySelector("iframe")).toBe(original);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    fireEvent.click(screen.getByRole("button", { name: "Show source" }));
    expect(editorContent.closest("pre")?.className).not.toContain("sr-only");
    fireEvent.click(opener);
    expect(screen.getByRole("dialog").querySelector("iframe")).toBe(original);
    expect(original?.closest("[hidden]")).toBeNull();
  });

  it("does not show the toggle or an iframe for a non-html language", () => {
    render(<CodeBlockView {...makeProps("typescript", "const x = 1;")} />);
    expect(screen.queryByRole("button", { name: "Show source" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Show preview" })).toBeNull();
    expect(document.querySelector("iframe")).toBeNull();
    expect(screen.queryByRole("button", { name: "Reload" })).toBeNull();
  });

  it("reloads inline and enlarged HTML while preserving the editor and surface", () => {
    render(<CodeBlockView {...makeProps("html", "<p>hello</p>")} />);
    const original = document.querySelector("iframe");
    const content = screen.getByTestId("nvc");
    fireEvent.click(screen.getByRole("button", { name: "Reload" }));
    const inline = document.querySelector("iframe");
    expect(inline).not.toBe(original);
    expect(inline?.getAttribute("srcdoc")).toBe("<p>hello</p>");
    expect(screen.getByTestId("nvc")).toBe(content);

    fireEvent.click(screen.getByRole("button", { name: "Show source" }));
    expect(screen.getByRole("button", { name: "Reload" }).hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Fullscreen" }));
    const dialog = screen.getByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Reload" }));
    const enlarged = dialog.querySelector("iframe");
    expect(enlarged).not.toBe(inline);
    expect(enlarged?.getAttribute("sandbox")).toBe("allow-scripts");
    expect(screen.getByRole("dialog")).toBe(dialog);
    expect(document.querySelectorAll("iframe")).toHaveLength(1);
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    expect(document.querySelector("iframe")).toBe(enlarged);
    expect(screen.getByTestId("nvc")).toBe(content);
    expect(content.closest("pre")?.className).not.toContain("sr-only");
  });

  it("defers source edits until the retained preview is visible", () => {
    const { rerender } = render(
      <CodeBlockView {...makeProps("html", "<p>initial</p>")} />,
    );
    const frame = document.querySelector("iframe")!;
    const editorContent = screen.getByTestId("nvc");

    rerender(<CodeBlockView {...makeProps("html", "<p>pending</p>")} />);
    fireEvent.click(screen.getByRole("button", { name: "Show source" }));
    act(() => vi.advanceTimersByTime(250));
    expect(frame.getAttribute("srcdoc")).toBe("<p>initial</p>");
    rerender(<CodeBlockView {...makeProps("html", "<p>edited</p>")} />);
    act(() => vi.advanceTimersByTime(250));
    expect(frame.getAttribute("srcdoc")).toBe("<p>initial</p>");

    fireEvent.click(screen.getByRole("button", { name: "Fullscreen" }));
    act(() => vi.advanceTimersByTime(250));
    expect(frame.getAttribute("srcdoc")).toBe("<p>edited</p>");
    expect(screen.getByRole("dialog").querySelector("iframe")).toBe(frame);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    rerender(<CodeBlockView {...makeProps("html", "<p>latest</p>")} />);
    act(() => vi.advanceTimersByTime(250));
    expect(frame.getAttribute("srcdoc")).toBe("<p>edited</p>");
    fireEvent.click(screen.getByRole("button", { name: "Show preview" }));
    act(() => vi.advanceTimersByTime(250));
    expect(frame.getAttribute("srcdoc")).toBe("<p>latest</p>");
    expect(document.querySelector("iframe")).toBe(frame);
    expect(screen.getByTestId("nvc")).toBe(editorContent);
  });
});
