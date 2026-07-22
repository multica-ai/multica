import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { readFileSync } from "node:fs";

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({ t: (select: (bundle: typeof editor) => string) => select(editor) }),
  };
});

vi.mock("./code-block-static", () => ({
  CodeBlockStatic: ({ body }: { body: string }) => (
    <pre data-testid="code-block-static">{body}</pre>
  ),
}));

const copyTextMock = vi.hoisted(() => vi.fn().mockResolvedValue(true));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: copyTextMock }));

const mermaidRenderMock = vi.hoisted(() => vi.fn());
const mermaidInitializeMock = vi.hoisted(() => vi.fn());
vi.mock("mermaid", () => ({
  default: { initialize: mermaidInitializeMock, render: mermaidRenderMock },
}));

const MOCK_SVG = '<svg viewBox="0 0 1000 500"><g><text>mock diagram</text></g></svg>';

import { MermaidDiagram } from "./mermaid-diagram";

const CHART = "graph LR\n  A[Start] --> B[Done]";
const VIEWPORT = { width: 800, height: 400 };

// jsdom reports 0x0 for every rect; the canvas needs a real viewport to fit against.
function stubViewportSize() {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    bottom: VIEWPORT.height,
    height: VIEWPORT.height,
    left: 0,
    right: VIEWPORT.width,
    top: 0,
    width: VIEWPORT.width,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  });
}

beforeEach(() => {
  stubViewportSize();
  // Full reset, not mockClear: `mock.calls` would otherwise carry over and make
  // a "re-render happened" waitFor pass instantly on a previous test's calls,
  // and an unconsumed *Once implementation would leak into the next test.
  mermaidRenderMock.mockReset();
  mermaidRenderMock.mockResolvedValue({ svg: MOCK_SVG });
  mermaidInitializeMock.mockClear();
  copyTextMock.mockClear();
  Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
    configurable: true,
    value: () => ({
      fillStyle: "#000",
      fillRect: vi.fn(),
      getImageData: () => ({ data: new Uint8ClampedArray([12, 34, 56, 255]) }),
    }),
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  document.documentElement.className = "";
});

function currentScale(): number {
  const element = document.querySelector<HTMLElement>(".mermaid-viewer-content")!;
  return Number.parseFloat(/scale\(([\d.]+)\)/.exec(element.style.transform)![1]!);
}

async function openViewer() {
  const expand = await screen.findByRole("button", { name: "Open diagram viewer" });
  fireEvent.click(expand);
  await screen.findByRole("application");
}

async function findScroller(): Promise<HTMLElement> {
  return waitFor(() => {
    const found = document.querySelector<HTMLElement>(".mermaid-diagram-scroll");
    expect(found).not.toBeNull();
    return found!;
  });
}

/**
 * A press and release with no movement — the gesture that should open the viewer.
 *
 * Fires the trailing `click` too. jsdom does not synthesize it from pointer
 * events, but a real browser always does, and that click is exactly what used
 * to reopen the viewer at the end of a drag — so a gesture helper that omits it
 * cannot see the bug at all.
 */
function tap(element: HTMLElement, { x, y }: { x: number; y: number }) {
  fireEvent.pointerDown(element, { pointerId: 1, clientX: x, clientY: y });
  fireEvent.pointerUp(element, { pointerId: 1, clientX: x, clientY: y });
  fireEvent.click(element, { clientX: x, clientY: y });
}

/** A press, drag and release, in the event order a real browser produces. */
function drag(
  element: HTMLElement,
  from: { x: number; y: number },
  to: { x: number; y: number },
  { pointerType = "mouse", button = 0 }: { pointerType?: string; button?: number } = {},
) {
  const id = { pointerId: 1, pointerType, button };
  fireEvent.pointerDown(element, { ...id, clientX: from.x, clientY: from.y });
  fireEvent.pointerMove(element, { ...id, clientX: to.x, clientY: to.y });
  fireEvent.pointerUp(element, { ...id, clientX: to.x, clientY: to.y });
  fireEvent.click(element, { clientX: to.x, clientY: to.y });
}

async function expectViewerStaysClosed() {
  // The viewer mounts asynchronously through the Dialog's portal, so assert
  // after a flush — a synchronous check would pass even if it were opening.
  await waitFor(() => {
    expect(mermaidRenderMock).toHaveBeenCalled();
  });
  expect(screen.queryByRole("application")).toBeNull();
}

describe("MermaidDiagram theme changes", () => {
  it("keeps the viewer open and preserves zoom when the theme flips", async () => {
    render(<MermaidDiagram chart={CHART} />);
    await openViewer();

    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    const zoomed = currentScale();
    expect(zoomed).toBeGreaterThan(0.8);

    // Flip the theme the way the app does; the component observes documentElement
    // and re-renders the diagram with new theme colors.
    await act(async () => {
      document.documentElement.classList.add("dark");
      // Let the MutationObserver callback and the async re-render settle.
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(mermaidRenderMock.mock.calls.length).toBeGreaterThan(1);
    });

    // The viewer previously unmounted here: the re-render cleared the rendered
    // document to null before the new one arrived, closing the dialog and
    // throwing away the user's zoom and position mid-read.
    expect(screen.getByRole("application")).toBeInTheDocument();
    expect(currentScale()).toBeCloseTo(zoomed, 5);
  });

  it("never blanks the diagram while the themed re-render is still in flight", async () => {
    render(<MermaidDiagram chart={CHART} />);
    await waitFor(() => {
      expect(document.querySelector(".mermaid-diagram-frame")).not.toBeNull();
    });

    // Hold the themed re-render open so the intermediate state is observable.
    // Without this the replacement lands in the same tick and a blanking bug
    // would slip through unseen.
    let releaseRender!: (value: { svg: string }) => void;
    mermaidRenderMock.mockImplementationOnce(
      () =>
        new Promise<{ svg: string }>((resolve) => {
          releaseRender = resolve;
        }),
    );

    await act(async () => {
      document.documentElement.classList.add("dark");
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(mermaidRenderMock.mock.calls.length).toBeGreaterThan(1);
    });

    // Mid-flight: the previous diagram must still be on screen rather than
    // collapsing to the loading skeleton on every theme toggle.
    expect(document.querySelector(".mermaid-diagram-frame")).not.toBeNull();
    expect(screen.queryByText("Rendering diagram…")).toBeNull();

    await act(async () => {
      releaseRender({ svg: '<svg viewBox="0 0 1000 500"><text>themed</text></svg>' });
    });
    expect(document.querySelector(".mermaid-diagram-frame")).not.toBeNull();
  });
});

// Verified in Chromium: dragging a diagram starts a native text selection that
// paints the whole iframe box with the selection highlight (it is a replaced
// element) and runs on into the surrounding comment text. Asserted against the
// stylesheet because jsdom has no layout and cannot reproduce a real selection.
// Deliberately NOT solved by preventDefault-ing pointerdown: that also drops
// the default focus, which silently kills the viewer's keyboard controls.
describe("Mermaid selection suppression", () => {
  const mermaidCss = readFileSync("editor/styles/mermaid.css", "utf8");

  function blockFor(selector: string): string {
    const start = mermaidCss.indexOf(selector);
    expect(start, `${selector} missing from mermaid.css`).toBeGreaterThan(-1);
    return mermaidCss.slice(start, mermaidCss.indexOf("}", start));
  }

  it("stops a drag on the inline diagram from selecting text", () => {
    expect(blockFor(".mermaid-diagram-scroll {")).toContain("user-select: none");
  });

  it("stops a pan that leaves the viewer canvas from selecting text", () => {
    expect(blockFor(".mermaid-viewer-canvas {")).toContain("user-select: none");
  });
});

describe("MermaidDiagram rendering config", () => {
  it("renders labels as SVG text, without which PNG export silently produces nothing", async () => {
    render(<MermaidDiagram chart={CHART} />);

    await waitFor(() => {
      expect(mermaidInitializeMock).toHaveBeenCalled();
    });

    // Verified in Chromium against the real Mermaid build: with Mermaid's
    // default (htmlLabels: true) the labels land in a <foreignObject>, which a
    // browser refuses to rasterize through <img> — it paints zero pixels and
    // taints the canvas, so toBlob throws and "Download PNG" does nothing at
    // all. Keep this false or export is broken.
    expect(mermaidInitializeMock).toHaveBeenCalledWith(
      expect.objectContaining({ htmlLabels: false }),
    );
    // The sandbox iframe is the isolation boundary; strict must stay strict.
    expect(mermaidInitializeMock).toHaveBeenCalledWith(
      expect.objectContaining({ securityLevel: "strict" }),
    );
  });
});

describe("MermaidDiagram inline presentation", () => {
  it("renders the diagram in an empty sandbox at its natural size", async () => {
    render(<MermaidDiagram chart={CHART} />);

    const frame = await waitFor(() => {
      const found = document.querySelector<HTMLIFrameElement>(".mermaid-diagram-frame");
      expect(found).not.toBeNull();
      return found!;
    });

    expect(frame.getAttribute("sandbox")).toBe("");
    expect(frame.style.width).toBe("1000px");
    expect(frame.style.height).toBe("500px");
  });

  it("copies the source straight from the inline toolbar", async () => {
    render(<MermaidDiagram chart={CHART} />);

    fireEvent.click(await screen.findByRole("button", { name: "Copy diagram source" }));

    await waitFor(() => {
      expect(copyTextMock).toHaveBeenCalledWith(CHART);
    });
  });

  it("opens the viewer when the diagram itself is tapped, not just the button", async () => {
    render(<MermaidDiagram chart={CHART} />);
    const scroll = await findScroller();

    tap(scroll, { x: 100, y: 100 });

    expect(await screen.findByRole("application")).toBeInTheDocument();
  });
});

// Kim's acceptance (MUL-4908): a still click opens, a horizontal drag moves a
// wide diagram, and no gesture past the threshold may open the viewer on
// release. Before this, every attempt to drag a wide diagram ended in a click
// and the viewer opened on top of the user.
describe("MermaidDiagram inline tap vs drag", () => {
  async function setupScroller({ scrollable = true }: { scrollable?: boolean } = {}) {
    render(<MermaidDiagram chart={CHART} />);
    const scroll = await findScroller();
    // jsdom has no layout: scrollLeft would clamp to 0 and scrollWidth is
    // always 0, so back them with real values to observe the pan.
    let scrollLeft = 0;
    Object.defineProperty(scroll, "scrollLeft", {
      configurable: true,
      get: () => scrollLeft,
      set: (value: number) => {
        // Mirror the browser's clamping, which is what makes an unscrollable
        // diagram stay put while still counting as a drag.
        const max = scrollable ? 2000 : 0;
        scrollLeft = Math.min(Math.max(value, 0), max);
      },
    });
    return scroll;
  }

  it("opens the viewer on a still click", async () => {
    const scroll = await setupScroller();

    tap(scroll, { x: 100, y: 100 });

    expect(await screen.findByRole("application")).toBeInTheDocument();
  });

  it("still opens the viewer when a click jitters below the threshold", async () => {
    const scroll = await setupScroller();

    // ~3px of shake is a click with an unsteady hand, not a drag. The threshold
    // has to sit above this or trackpad users could never open the viewer.
    drag(scroll, { x: 100, y: 100 }, { x: 102, y: 102 });

    expect(await screen.findByRole("application")).toBeInTheDocument();
  });

  it("does not open the viewer after a horizontal drag", async () => {
    const scroll = await setupScroller();

    drag(scroll, { x: 300, y: 100 }, { x: 200, y: 100 });

    await expectViewerStaysClosed();
  });

  it("pans the scroll container while dragging horizontally", async () => {
    const scroll = await setupScroller();

    fireEvent.pointerDown(scroll, { pointerId: 1, clientX: 300, clientY: 100 });
    fireEvent.pointerMove(scroll, { pointerId: 1, clientX: 200, clientY: 100 });

    // Dragging left by 100px reveals 100px further right.
    expect(scroll.scrollLeft).toBe(100);

    fireEvent.pointerMove(scroll, { pointerId: 1, clientX: 260, clientY: 100 });
    // Tracks the pointer from the gesture's origin, not incrementally.
    expect(scroll.scrollLeft).toBe(40);
  });

  it("does not open the viewer after dragging a diagram that cannot scroll", async () => {
    // Nothing to pan, so the drag has no visible effect — releasing must still
    // not open the viewer, or the gesture reads as an accidental jump.
    const scroll = await setupScroller({ scrollable: false });

    drag(scroll, { x: 300, y: 100 }, { x: 200, y: 100 });

    expect(scroll.scrollLeft).toBe(0);
    await expectViewerStaysClosed();
  });

  it("does not open the viewer when the browser takes over the gesture", async () => {
    // Touch scrolling (horizontal pan or vertical page scroll) is handed to the
    // browser, which signals the takeover with pointercancel.
    const scroll = await setupScroller();

    fireEvent.pointerDown(scroll, { pointerId: 1, pointerType: "touch", clientX: 100, clientY: 300 });
    fireEvent.pointerCancel(scroll, { pointerId: 1, pointerType: "touch" });

    await expectViewerStaysClosed();
  });

  it("leaves touch panning to the browser instead of driving scrollLeft itself", async () => {
    // The container is `overflow-x: auto`, so touch already pans natively and
    // vertical drags belong to the page. Driving scrollLeft here as well would
    // move the diagram at double speed.
    const scroll = await setupScroller();

    fireEvent.pointerDown(scroll, { pointerId: 1, pointerType: "touch", clientX: 300, clientY: 100 });
    fireEvent.pointerMove(scroll, { pointerId: 1, pointerType: "touch", clientX: 200, clientY: 100 });

    expect(scroll.scrollLeft).toBe(0);
  });

  it("pans for a pen drag, which has no native drag-to-scroll either", async () => {
    // Only touch is left to the browser. Keying the pan on `=== "mouse"` would
    // silently leave pen users unable to pan at all.
    const scroll = await setupScroller();

    fireEvent.pointerDown(scroll, { pointerId: 1, pointerType: "pen", clientX: 300, clientY: 100 });
    fireEvent.pointerMove(scroll, { pointerId: 1, pointerType: "pen", clientX: 200, clientY: 100 });

    expect(scroll.scrollLeft).toBe(100);
  });

  it("ignores right-button drags", async () => {
    const scroll = await setupScroller();

    drag(scroll, { x: 300, y: 100 }, { x: 200, y: 100 }, { button: 2 });

    expect(scroll.scrollLeft).toBe(0);
    await expectViewerStaysClosed();
  });

  it("keeps the expand button opening the viewer regardless of the gesture rule", async () => {
    await setupScroller();

    fireEvent.click(screen.getByRole("button", { name: "Open diagram viewer" }));

    expect(await screen.findByRole("application")).toBeInTheDocument();
  });
});

describe("MermaidDiagram error state", () => {
  it("surfaces the parser message and a copy affordance alongside the source fallback", async () => {
    mermaidRenderMock.mockRejectedValueOnce(new Error("Parse error on line 3"));

    render(<MermaidDiagram chart={CHART} />);

    await waitFor(() => {
      expect(document.querySelector(".mermaid-diagram-error")).not.toBeNull();
    });
    // Without the parser message the fallback is an unexplained code block.
    expect(screen.getByText("Parse error on line 3")).toBeInTheDocument();
    expect(screen.getByText("Unable to render Mermaid diagram.")).toBeInTheDocument();
    expect(document.querySelector(".mermaid-diagram-error code")?.textContent).toBe(CHART);

    fireEvent.click(screen.getByRole("button", { name: "Copy diagram source" }));
    await waitFor(() => {
      expect(copyTextMock).toHaveBeenCalledWith(CHART);
    });
  });
});
