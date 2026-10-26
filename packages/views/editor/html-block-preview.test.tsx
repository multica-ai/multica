import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { mockNativeDialog } from "./test-utils/native-dialog";

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string, values?: Record<string, unknown>) =>
        select(editor).replace(/\{\{(\w+)\}\}/g, (_, key: string) => String(values?.[key] ?? "")),
    }),
  };
});

// CodeBlockStatic depends on lowlight which has a heavy import surface and a
// jsdom-incompatible code path. Stub to keep the source-view test focused on
// the toggle wiring rather than highlighting.
vi.mock("./code-block-static", () => ({
  CodeBlockStatic: ({ body }: { body: string }) => (
    <pre data-testid="code-block-static">{body}</pre>
  ),
}));

import { HtmlBlockPreview } from "./html-block-preview";
import { HTML_BLOCK_MESSAGE_KEY } from "./utils/html-block-document";

let restoreDialog: () => void;
beforeEach(() => {
  window.sessionStorage.clear();
  restoreDialog = mockNativeDialog();
});
afterEach(() => {
  cleanup();
  restoreDialog();
  vi.restoreAllMocks();
});

function inlineFrame(): HTMLIFrameElement {
  const frame = document.querySelector<HTMLIFrameElement>('[data-dynamic-block="html"] iframe');
  expect(frame).not.toBeNull();
  return frame!;
}

/** What the bridge inside the sandbox posts to the page. */
function post(frame: HTMLIFrameElement, data: Record<string, unknown>) {
  act(() => {
    window.dispatchEvent(
      new MessageEvent("message", {
        data: { [HTML_BLOCK_MESSAGE_KEY]: 1, ...data },
        source: frame.contentWindow,
      }),
    );
  });
}

describe("HtmlBlockPreview — frame", () => {
  it("shows an always-visible title bar with the fence title and kind", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" title="Weekly p95" />);
    expect(screen.getByText("Weekly p95")).toBeTruthy();
    expect(screen.getByText("HTML")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Preview" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Fullscreen" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy source" })).toBeTruthy();
  });

  it("names the block after its kind when the fence has no title", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    expect(screen.getAllByText("HTML")).toHaveLength(1);
  });

  it("renders the HTML in a scripts-only sandbox with the theme, bridge and fragment shim", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    const frame = inlineFrame();
    expect(frame.getAttribute("title")).toBe("HTML preview");
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    const srcdoc = frame.getAttribute("srcdoc") ?? "";
    expect(srcdoc.startsWith("<style>:root{")).toBe(true);
    expect(srcdoc).toContain(HTML_BLOCK_MESSAGE_KEY);
    expect(srcdoc).toContain("<p>hi</p>");
    expect(srcdoc).toContain("scrollIntoView");
  });

  it("switches to source in the same frame and keeps the preview mounted", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    const frame = inlineFrame();

    fireEvent.click(screen.getByRole("tab", { name: "Source" }));
    expect(screen.getByTestId("code-block-static").textContent).toBe("<p>hi</p>");
    // Hidden, not torn down: going back must not rerun the document.
    expect(frame.isConnected).toBe(true);

    fireEvent.click(screen.getByRole("tab", { name: "Preview" }));
    expect(inlineFrame()).toBe(frame);
    expect(screen.queryByTestId("code-block-static")).toBeNull();
  });
});

describe("HtmlBlockPreview — height", () => {
  const body = (frame: HTMLIFrameElement) => frame.parentElement as HTMLElement;

  it("shows a loading skeleton until the document reports, then takes its height", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    const frame = inlineFrame();
    expect(screen.getByText("Rendering...")).toBeTruthy();
    expect(frame.className).toContain("invisible");

    post(frame, { type: "height", height: 264 });
    expect(body(frame).style.height).toBe("264px");
    expect(screen.queryByText("Rendering...")).toBeNull();
    expect(frame.className).not.toContain("invisible");
  });

  it("never goes below the minimum body height", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    const frame = inlineFrame();
    post(frame, { type: "height", height: 30 });
    expect(body(frame).style.height).toBe("120px");
  });

  it("ignores messages that do not come from its own frame", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    const frame = inlineFrame();
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { [HTML_BLOCK_MESSAGE_KEY]: 1, type: "height", height: 400 },
          source: window,
        }),
      );
    });
    expect(screen.getByText("Rendering...")).toBeTruthy();
    expect(body(frame).style.height).toBe("120px");
  });

  it("starts from the height this HTML had earlier in the session", () => {
    const first = render(<HtmlBlockPreview html="<p>chart</p>" />);
    post(inlineFrame(), { type: "height", height: 300 });
    first.unmount();

    render(<HtmlBlockPreview html="<p>chart</p>" />);
    expect(body(inlineFrame()).style.height).toBe("300px");
  });
});

describe("HtmlBlockPreview — errors", () => {
  it("explains a script error in place and offers the source", () => {
    render(<HtmlBlockPreview html="<script>boom()</script>" />);
    post(inlineFrame(), { type: "error", message: "ReferenceError: boom is not defined", line: 1 });

    const alert = screen.getByRole("alert");
    expect(alert.textContent).toContain("The preview hit a script error");
    expect(alert.textContent).toContain("ReferenceError: boom is not defined (line 1)");

    fireEvent.click(screen.getByRole("button", { name: "View source" }));
    expect(screen.getByTestId("code-block-static").textContent).toBe("<script>boom()</script>");
  });
});

describe("HtmlBlockPreview — reload", () => {
  it("reloads only the requested frame and restarts height measurement after a script error", () => {
    render(
      <>
        <HtmlBlockPreview html="<p>first</p>" title="First" />
        <HtmlBlockPreview html="<p>second</p>" title="Second" />
      </>,
    );
    const frames = document.querySelectorAll("iframe");
    const first = frames[0]!;
    const second = frames[1]!;
    const block = first.closest('[data-dynamic-block="html"]') as HTMLElement;
    for (const height of [200, 300, 400, 500]) post(first, { type: "height", height });
    post(first, { type: "error", message: "Network unavailable" });
    expect(within(block).getByRole("alert").textContent).toContain("Network unavailable");
    const staleWindow = first.contentWindow;
    const srcDoc = first.getAttribute("srcdoc");

    fireEvent.click(within(block).getByRole("button", { name: "Reload" }));

    const fresh = block.querySelector("iframe")!;
    expect(fresh).not.toBe(first);
    expect(first.isConnected).toBe(false);
    expect(second.isConnected).toBe(true);
    expect(fresh.getAttribute("srcdoc")).toBe(srcDoc);
    expect(fresh.getAttribute("sandbox")).toBe("allow-scripts");
    expect(within(block).queryByRole("alert")).toBeNull();
    expect(within(block).getByText("Rendering...")).toBeTruthy();
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        source: staleWindow,
        data: { [HTML_BLOCK_MESSAGE_KEY]: 1, type: "error", message: "Stale error" },
      }));
    });
    expect(within(block).queryByRole("alert")).toBeNull();
    post(fresh, { type: "height", height: 600 });
    expect(fresh.parentElement?.style.height).toBe("600px");
    expect(within(block).queryByText("Rendering...")).toBeNull();
  });

  it("waits for the reloaded document before restarting the missing-bridge timeout", () => {
    vi.useFakeTimers();
    try {
      render(<HtmlBlockPreview html="<p>no bridge reports</p>" />);
      fireEvent.load(inlineFrame());
      act(() => vi.advanceTimersByTime(1000));
      expect(screen.queryByText("Rendering...")).toBeNull();

      fireEvent.click(screen.getByRole("button", { name: "Reload" }));
      act(() => vi.advanceTimersByTime(1000));
      expect(screen.getByText("Rendering...")).toBeTruthy();
      fireEvent.load(inlineFrame());
      act(() => vi.advanceTimersByTime(1000));
      expect(screen.queryByText("Rendering...")).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("reloads inside the same enlarged surface and retains source selection on close", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    const original = inlineFrame();
    fireEvent.click(screen.getByRole("tab", { name: "Source" }));
    const source = screen.getByTestId("code-block-static");
    expect(screen.getByRole("button", { name: "Reload" }).hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Fullscreen" }));
    const dialog = screen.getByRole("dialog");

    fireEvent.click(within(dialog).getByRole("button", { name: "Reload" }));

    expect(screen.getByRole("dialog")).toBe(dialog);
    const fresh = dialog.querySelector("iframe");
    expect(fresh).not.toBe(original);
    expect(document.querySelectorAll("iframe")).toHaveLength(1);
    expect(source.isConnected).toBe(true);
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    expect(screen.getByRole("tab", { name: "Source" }).getAttribute("aria-selected")).toBe("true");
    fireEvent.click(screen.getByRole("tab", { name: "Preview" }));
    expect(inlineFrame()).toBe(fresh);
  });
});

describe("HtmlBlockPreview — fullscreen", () => {
  it("enlarges the loaded iframe without creating another document", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    const original = inlineFrame();
    expect(document.querySelectorAll("iframe")).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "Fullscreen" }));

    const frames = document.querySelectorAll("iframe");
    expect(frames).toHaveLength(1);
    expect(screen.getByRole("dialog").querySelector("iframe")).toBe(original);
    expect(original.getAttribute("sandbox")).toBe("allow-scripts");
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Fullscreen" }));
    expect(screen.getByRole("dialog").querySelector("iframe")).toBe(original);
  });

  it("ignores enlarged viewport measurements and restores the inline height", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    const frame = inlineFrame();
    post(frame, { type: "height", height: 264 });
    fireEvent.click(screen.getByRole("button", { name: "Fullscreen" }));
    post(frame, { type: "height", height: 972 });
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(frame.parentElement?.style.height).toBe("264px");
  });

  it("reveals the loaded preview from source and restores source on close", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    const frame = inlineFrame();
    fireEvent.click(screen.getByRole("tab", { name: "Source" }));
    fireEvent.click(screen.getByRole("button", { name: "Fullscreen" }));
    expect(screen.getByRole("dialog").querySelector("iframe")).toBe(frame);
    expect(frame.closest("[hidden]")).toBeNull();
    expect(frame.closest("[inert]")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(
      screen.getByRole("tab", { name: "Source" }).getAttribute("aria-selected"),
    ).toBe("true");
  });

  it("keeps expanded source mounted and selected while enlarging the preview", () => {
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(900);
    render(<HtmlBlockPreview html={"<p>long source</p>\n".repeat(100)} />);
    fireEvent.click(screen.getByRole("tab", { name: "Source" }));
    const source = screen.getByTestId("code-block-static");
    fireEvent.click(screen.getByRole("button", { name: "Show all" }));

    fireEvent.click(screen.getByRole("button", { name: "Fullscreen" }));
    expect(source.isConnected).toBe(true);
    expect(screen.getByRole("tab", { name: "Source" }).getAttribute("aria-selected")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    expect(screen.getByTestId("code-block-static")).toBe(source);
    expect(source.closest("[data-collapsed]")).toBeNull();
    expect(screen.queryByRole("button", { name: "Show all" })).toBeNull();
  });
});
