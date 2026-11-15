import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { VirtualThreadRow } from "./virtual-thread-row";

let notify: IntersectionObserverCallback;
let observed: Element;
beforeEach(() => {
  vi.stubGlobal("IntersectionObserver", class {
    constructor(callback: IntersectionObserverCallback) { notify = callback; }
    observe(node: Element) { observed = node; }
    disconnect() {}
  });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });
const intersect = (visible: boolean) => act(() => notify([
  { target: observed, isIntersecting: visible } as IntersectionObserverEntry,
], {} as IntersectionObserver));

describe("VirtualThreadRow", () => {
  it("keeps an anchor and measured height while unmounting offscreen content", () => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 400, 640));
    const body = vi.fn(() => <p>Expensive Markdown</p>);
    const { container } = render(<VirtualThreadRow id="comment-target" scrollParent={document.body}>{body}</VirtualThreadRow>);
    expect(body).not.toHaveBeenCalled();
    expect(container.querySelector("#comment-target")).toHaveStyle({ height: "320px" });
    intersect(true);
    expect(screen.getByText("Expensive Markdown")).toBeInTheDocument();
    intersect(false);
    expect(screen.queryByText("Expensive Markdown")).toBeNull();
    expect(container.querySelector("#comment-target")).toHaveStyle({ height: "640px" });
    intersect(true);
    expect(screen.getByText("Expensive Markdown")).toBeInTheDocument();
  });

  it("retains an offscreen edit instead of unmounting its draft", () => {
    render(<VirtualThreadRow id="comment-edit" scrollParent={document.body}>
      {() => <div contentEditable suppressContentEditableWarning>Unsaved draft</div>}
    </VirtualThreadRow>);
    intersect(true);
    intersect(false);
    expect(screen.getByText("Unsaved draft")).toBeInTheDocument();
  });

  it("renders all content without a scroll parent for in-page find", () => {
    render(<VirtualThreadRow id="comment-find">{() => <p>Findable history</p>}</VirtualThreadRow>);
    expect(screen.getByText("Findable history")).toBeInTheDocument();
  });
});
