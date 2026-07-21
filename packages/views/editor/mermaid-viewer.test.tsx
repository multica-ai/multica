import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";

// Resolve against the real EN bundle rather than a hand-written stub: the
// accessible names below are then the strings that actually ship, and a
// missing/renamed key fails here instead of silently rendering "".
vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({ t: (select: (bundle: typeof editor) => string) => select(editor) }),
  };
});

// CodeBlockStatic pulls in lowlight, which has a heavy import surface and a
// jsdom-incompatible path. The source view's wiring is what matters here.
vi.mock("./code-block-static", () => ({
  CodeBlockStatic: ({ body }: { body: string }) => (
    <pre data-testid="code-block-static">{body}</pre>
  ),
}));

const copyTextMock = vi.hoisted(() => vi.fn().mockResolvedValue(true));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: copyTextMock }));

import { MermaidViewer } from "./mermaid-viewer";

const CHART = "graph LR\n  A[Start] --> B[Done]";
const SVG = '<svg viewBox="0 0 1000 500"><text>mock diagram</text></svg>';
const VIEWER_DOC = "<!doctype html><html><body>mock</body></html>";
const LAYOUT = { width: 1000, height: 500 };

// The canvas measures itself with getBoundingClientRect, which jsdom always
// reports as 0x0. Without a real viewport the transform math has nothing to
// fit against and every gesture is a no-op, so pin a deterministic size.
const VIEWPORT = { width: 800, height: 400 };

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

function Harness({ initialOpen = true }: { initialOpen?: boolean }) {
  const [open, setOpen] = useState(initialOpen);
  return (
    <>
      <button type="button" data-testid="opener" onClick={() => setOpen(true)}>
        open
      </button>
      <MermaidViewer
        open={open}
        onOpenChange={setOpen}
        chart={CHART}
        svg={SVG}
        viewerDocument={VIEWER_DOC}
        layout={LAYOUT}
        exportBackground="rgb(240, 240, 240)"
        exportFontFamily="Inter, sans-serif"
      />
    </>
  );
}

function canvas(): HTMLElement {
  return screen.getByRole("application");
}

function content(): HTMLElement {
  return document.querySelector<HTMLElement>(".mermaid-viewer-content")!;
}

/** Reads the applied scale back out of the inline transform. */
function currentScale(): number {
  const match = /scale\(([\d.]+)\)/.exec(content().style.transform);
  return Number.parseFloat(match![1]!);
}

function currentTranslate(): { x: number; y: number } {
  const match = /translate\(([-\d.]+)px, ([-\d.]+)px\)/.exec(content().style.transform);
  return { x: Number.parseFloat(match![1]!), y: Number.parseFloat(match![2]!) };
}

beforeEach(() => {
  stubViewportSize();
  copyTextMock.mockClear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("MermaidViewer closing", () => {
  it("closes via the explicit X button", async () => {
    render(<Harness />);
    expect(canvas()).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    await waitFor(() => {
      expect(screen.queryByRole("application")).toBeNull();
    });
  });

  it("closes on Escape after the canvas has been clicked and focused", async () => {
    // The regression this pins: the old lightbox put the diagram in an iframe
    // that took focus on click, stranding keydown inside a document the host
    // could not hear — so Escape silently stopped working and the overlay had
    // no visible close button. Interaction now lives in the host document.
    render(<Harness />);

    fireEvent.pointerDown(canvas(), { pointerId: 1, clientX: 100, clientY: 100 });
    fireEvent.pointerUp(canvas(), { pointerId: 1, clientX: 100, clientY: 100 });
    canvas().focus();
    expect(document.activeElement).toBe(canvas());

    fireEvent.keyDown(document.activeElement!, { key: "Escape" });

    await waitFor(() => {
      expect(screen.queryByRole("application")).toBeNull();
    });
  });

  it("keeps the diagram inside an empty sandbox, so it can never steal focus or run scripts", () => {
    render(<Harness />);

    const frame = document.querySelector<HTMLIFrameElement>(".mermaid-viewer-frame");
    expect(frame).not.toBeNull();
    // An empty sandbox is what makes host-side pan/zoom necessary; if this ever
    // loosens, the isolation the whole design protects is gone.
    expect(frame?.getAttribute("sandbox")).toBe("");
    expect(frame?.srcdoc).toBe(VIEWER_DOC);
  });

  it("returns focus to the element that opened it", async () => {
    function FocusHarness() {
      const [open, setOpen] = useState(false);
      const ref = { current: null as HTMLButtonElement | null };
      return (
        <>
          <button
            type="button"
            data-testid="opener"
            ref={(node) => {
              ref.current = node;
            }}
            onClick={() => setOpen(true)}
          >
            open
          </button>
          <MermaidViewer
            open={open}
            onOpenChange={setOpen}
            chart={CHART}
            svg={SVG}
            viewerDocument={VIEWER_DOC}
            layout={LAYOUT}
            exportBackground="rgb(240, 240, 240)"
            exportFontFamily="Inter, sans-serif"
            finalFocusRef={ref}
          />
        </>
      );
    }

    render(<FocusHarness />);
    const opener = screen.getByTestId("opener");
    opener.focus();
    fireEvent.click(opener);

    await screen.findByRole("application");
    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    await waitFor(() => {
      expect(document.activeElement).toBe(opener);
    });
  });

  it("locks background scroll while open and restores it on close", async () => {
    render(<Harness />);

    // Base UI marks the scroll-locked root; without the lock the page behind
    // drifts and reopening lands the reader somewhere else.
    await waitFor(() => {
      expect(document.documentElement).toHaveAttribute("data-base-ui-scroll-locked");
    });

    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    await waitFor(() => {
      expect(document.documentElement).not.toHaveAttribute("data-base-ui-scroll-locked");
    });
  });
});

describe("MermaidViewer zoom controls", () => {
  it("opens fitted to the viewport rather than squashed to a fixed size", () => {
    render(<Harness />);

    // 1000x500 content in an 800x400 viewport → limited by width: 0.8.
    expect(currentScale()).toBeCloseTo(0.8, 5);
    expect(screen.getByText("80%")).toBeInTheDocument();
  });

  it("zooms in and out from the toolbar", () => {
    render(<Harness />);
    const initial = currentScale();

    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(currentScale()).toBeGreaterThan(initial);

    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(currentScale()).toBeCloseTo(initial, 5);
  });

  it("jumps to natural size with Actual size", () => {
    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: "Actual size" }));

    expect(currentScale()).toBe(1);
    expect(screen.getByText("100%")).toBeInTheDocument();
  });

  it("restores the fitted view with Reset after panning away", () => {
    render(<Harness />);
    const fitted = currentTranslate();

    fireEvent.pointerDown(canvas(), { pointerId: 1, clientX: 400, clientY: 200 });
    fireEvent.pointerMove(canvas(), { pointerId: 1, clientX: 200, clientY: 120 });
    fireEvent.pointerUp(canvas(), { pointerId: 1, clientX: 200, clientY: 120 });
    expect(currentTranslate()).not.toEqual(fitted);

    fireEvent.click(screen.getByRole("button", { name: "Reset view" }));

    expect(currentTranslate().x).toBeCloseTo(fitted.x, 5);
    expect(currentTranslate().y).toBeCloseTo(fitted.y, 5);
  });

  it("disables Reset when the view is already fitted, and enables it once moved", () => {
    render(<Harness />);
    expect(screen.getByRole("button", { name: "Reset view" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));

    expect(screen.getByRole("button", { name: "Reset view" })).toBeEnabled();
  });

  it("stops at 400% and disables Zoom in at the ceiling", () => {
    render(<Harness />);

    for (let i = 0; i < 20; i++) {
      fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    }

    expect(currentScale()).toBe(4);
    expect(screen.getByText("400%")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Zoom in" })).toBeDisabled();
  });

  it("re-fits on reopen instead of inheriting the previous session's zoom", async () => {
    render(<Harness initialOpen={false} />);
    fireEvent.click(screen.getByTestId("opener"));
    await screen.findByRole("application");
    const fitted = currentScale();

    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(currentScale()).toBeGreaterThan(fitted);

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("application")).toBeNull());

    fireEvent.click(screen.getByTestId("opener"));
    await screen.findByRole("application");

    // Reopening is a fresh read; carrying a stale zoom over would drop the
    // reader somewhere in the middle of a diagram they just reopened.
    expect(currentScale()).toBeCloseTo(fitted, 5);
  });

  it("reopens showing the diagram even if it was left on the source view", async () => {
    render(<Harness initialOpen={false} />);
    fireEvent.click(screen.getByTestId("opener"));
    await screen.findByRole("application");

    fireEvent.click(screen.getByRole("button", { name: "Show source" }));
    expect(screen.queryByRole("application")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByTestId("code-block-static")).toBeNull());

    fireEvent.click(screen.getByTestId("opener"));

    expect(await screen.findByRole("application")).toBeInTheDocument();
  });

  it("stops at 25% and disables Zoom out at the floor", () => {
    render(<Harness />);

    for (let i = 0; i < 20; i++) {
      fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    }

    expect(currentScale()).toBe(0.25);
    expect(screen.getByText("25%")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Zoom out" })).toBeDisabled();
  });
});

describe("MermaidViewer canvas interaction", () => {
  it("pans with a mouse drag", () => {
    render(<Harness />);
    const before = currentTranslate();

    fireEvent.pointerDown(canvas(), { pointerId: 1, clientX: 300, clientY: 200 });
    fireEvent.pointerMove(canvas(), { pointerId: 1, clientX: 350, clientY: 230 });

    const after = currentTranslate();
    expect(after.x).toBeCloseTo(before.x + 50, 5);
    expect(after.y).toBeCloseTo(before.y + 30, 5);
  });

  it("ignores right-button drags so context-menu clicks do not shift the diagram", () => {
    render(<Harness />);
    const before = currentTranslate();

    fireEvent.pointerDown(canvas(), {
      pointerId: 1,
      button: 2,
      pointerType: "mouse",
      clientX: 300,
      clientY: 200,
    });
    fireEvent.pointerMove(canvas(), { pointerId: 1, clientX: 350, clientY: 230 });

    expect(currentTranslate()).toEqual(before);
  });

  it("zooms on wheel, anchored so the point under the cursor stays put", () => {
    render(<Harness />);
    const before = { scale: currentScale(), ...currentTranslate() };
    const anchor = { x: 200, y: 150 };

    fireEvent.wheel(canvas(), { deltaY: -100, clientX: anchor.x, clientY: anchor.y });

    const after = { scale: currentScale(), ...currentTranslate() };
    expect(after.scale).toBeGreaterThan(before.scale);
    // Same content coordinate under the cursor before and after.
    expect((anchor.x - after.x) / after.scale).toBeCloseTo(
      (anchor.x - before.x) / before.scale,
      3,
    );
  });

  it("zooms out on the opposite wheel direction", () => {
    render(<Harness />);
    const before = currentScale();

    fireEvent.wheel(canvas(), { deltaY: 100, clientX: 200, clientY: 150 });

    expect(currentScale()).toBeLessThan(before);
  });

  it("pinch-zooms with two touch pointers", () => {
    render(<Harness />);
    const before = currentScale();

    fireEvent.pointerDown(canvas(), {
      pointerId: 1,
      pointerType: "touch",
      clientX: 300,
      clientY: 200,
    });
    fireEvent.pointerDown(canvas(), {
      pointerId: 2,
      pointerType: "touch",
      clientX: 400,
      clientY: 200,
    });
    // Fingers spread from 100px apart to 200px apart → ~2x.
    fireEvent.pointerMove(canvas(), {
      pointerId: 2,
      pointerType: "touch",
      clientX: 500,
      clientY: 200,
    });

    expect(currentScale()).toBeCloseTo(before * 2, 2);
  });

  it("zooms with the +/- keys and refits with 0", () => {
    render(<Harness />);
    const fitted = currentScale();

    fireEvent.keyDown(canvas(), { key: "+" });
    expect(currentScale()).toBeGreaterThan(fitted);

    fireEvent.keyDown(canvas(), { key: "-" });
    expect(currentScale()).toBeCloseTo(fitted, 5);

    fireEvent.keyDown(canvas(), { key: "+" });
    fireEvent.keyDown(canvas(), { key: "0" });
    expect(currentScale()).toBeCloseTo(fitted, 5);
  });

  it("pans with the arrow keys", () => {
    render(<Harness />);
    const before = currentTranslate();

    fireEvent.keyDown(canvas(), { key: "ArrowRight" });

    // ArrowRight moves the viewport right, i.e. the content left.
    expect(currentTranslate().x).toBeLessThan(before.x);
  });

  it("never lets the diagram be panned entirely out of view", () => {
    render(<Harness />);

    fireEvent.pointerDown(canvas(), { pointerId: 1, clientX: 400, clientY: 200 });
    fireEvent.pointerMove(canvas(), { pointerId: 1, clientX: -100000, clientY: -100000 });

    const { x, y } = currentTranslate();
    const scale = currentScale();
    // At least a sliver of content still overlaps the viewport.
    expect(x + LAYOUT.width * scale).toBeGreaterThan(0);
    expect(y + LAYOUT.height * scale).toBeGreaterThan(0);
  });
});

describe("MermaidViewer source and export", () => {
  it("copies the Mermaid source", async () => {
    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: "Copy diagram source" }));

    await waitFor(() => {
      expect(copyTextMock).toHaveBeenCalledWith(CHART);
    });
  });

  it("toggles to the source view and back, because Issue/Comment is where agent output gets audited", async () => {
    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: "Show source" }));

    expect(screen.queryByRole("application")).toBeNull();
    expect(screen.getByText(/graph LR/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show diagram" }));
    expect(screen.getByRole("application")).toBeInTheDocument();
  });

  it("offers PNG, SVG and .mmd exports", async () => {
    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: "Export" }));

    expect(await screen.findByText("Download PNG")).toBeInTheDocument();
    expect(screen.getByText("Download SVG")).toBeInTheDocument();
    expect(screen.getByText("Download .mmd")).toBeInTheDocument();
  });

  it("downloads an .mmd file containing the original source", async () => {
    const createObjectURL = vi.fn((_blob: Blob) => "blob:mock");
    const revokeObjectURL = vi.fn((_url: string) => {});
    vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL });
    const clickSpy = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});

    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Export" }));
    fireEvent.click(await screen.findByText("Download .mmd"));

    await waitFor(() => {
      expect(clickSpy).toHaveBeenCalled();
    });
    const blob = createObjectURL.mock.calls[0]![0];
    await expect(blob.text()).resolves.toBe(CHART);

    vi.unstubAllGlobals();
  });
});
