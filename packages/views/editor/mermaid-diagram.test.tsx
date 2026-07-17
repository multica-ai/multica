import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";

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

  it("opens the viewer when the diagram itself is clicked, not just the button", async () => {
    render(<MermaidDiagram chart={CHART} />);
    await waitFor(() => {
      expect(document.querySelector(".mermaid-diagram-scroll")).not.toBeNull();
    });

    fireEvent.click(document.querySelector(".mermaid-diagram-scroll")!);

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
