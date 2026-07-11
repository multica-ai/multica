import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { useRef, useState, type RefObject } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enEditor from "../locales/en/editor.json";
import { HtmlPreviewFindBar, type FindResult } from "./html-preview-find-bar";
import { FIND_CMD } from "./utils/iframe-find";

const resources = { en: { editor: enEditor } };

function Harness({
  post,
  result,
  onClose = () => {},
}: {
  post: ReturnType<typeof vi.fn>;
  result: FindResult | null;
  onClose?: () => void;
}) {
  // Real query state so the controlled `query` prop updates on type — the count
  // label depends on query.length > 0.
  const [query, setQuery] = useState("");
  const iframeRef = useRef<HTMLIFrameElement | null>(
    { contentWindow: { postMessage: post } } as unknown as HTMLIFrameElement,
  );
  return (
    <I18nProvider locale="en" resources={resources}>
      <HtmlPreviewFindBar
        iframeRef={iframeRef as RefObject<HTMLIFrameElement | null>}
        result={result}
        query={query}
        onQueryChange={setQuery}
        onClose={onClose}
      />
    </I18nProvider>
  );
}

describe("HtmlPreviewFindBar", () => {
  let post: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    post = vi.fn();
  });

  it("sends a search command to the iframe as the user types", () => {
    render(<Harness post={post} result={null} />);
    fireEvent.change(screen.getByPlaceholderText("Find in page"), {
      target: { value: "hello" },
    });
    expect(post).toHaveBeenCalledWith(
      { source: FIND_CMD, action: "search", query: "hello", caseSensitive: false },
      "*",
    );
  });

  it("renders the current/total count from result once a query is entered", () => {
    render(<Harness post={post} result={{ found: true, total: 5, current: 2 }} />);
    fireEvent.change(screen.getByPlaceholderText("Find in page"), { target: { value: "x" } });
    expect(screen.getByText("2/5")).toBeInTheDocument();
  });

  it("shows a no-results label when total is 0", () => {
    render(<Harness post={post} result={{ found: false, total: 0, current: 0 }} />);
    fireEvent.change(screen.getByPlaceholderText("Find in page"), { target: { value: "zzz" } });
    expect(screen.getByText("No results")).toBeInTheDocument();
  });

  it("Enter steps next, Shift+Enter steps prev", () => {
    render(<Harness post={post} result={{ found: true, total: 3, current: 1 }} />);
    const input = screen.getByPlaceholderText("Find in page");
    fireEvent.change(input, { target: { value: "a" } });
    post.mockClear();
    fireEvent.keyDown(input, { key: "Enter" });
    expect(post).toHaveBeenLastCalledWith(
      expect.objectContaining({ action: "next", query: "a" }),
      "*",
    );
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    expect(post).toHaveBeenLastCalledWith(
      expect.objectContaining({ action: "prev", query: "a" }),
      "*",
    );
  });

  it("clicking the next/prev buttons steps in that direction", () => {
    render(<Harness post={post} result={{ found: true, total: 3, current: 1 }} />);
    fireEvent.change(screen.getByPlaceholderText("Find in page"), { target: { value: "a" } });
    post.mockClear();
    fireEvent.click(screen.getByLabelText("Next match"));
    expect(post).toHaveBeenLastCalledWith(expect.objectContaining({ action: "next" }), "*");
    fireEvent.click(screen.getByLabelText("Previous match"));
    expect(post).toHaveBeenLastCalledWith(expect.objectContaining({ action: "prev" }), "*");
  });

  it("close clears the selection in the iframe and calls onClose", () => {
    const onClose = vi.fn();
    render(<Harness post={post} result={null} onClose={onClose} />);
    post.mockClear();
    fireEvent.click(screen.getByLabelText("Close find"));
    expect(post).toHaveBeenCalledWith(expect.objectContaining({ action: "clear" }), "*");
    expect(onClose).toHaveBeenCalled();
  });
});
