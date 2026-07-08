import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        code_block: {
          copy_code: "Copy code",
          show_preview: "Show preview",
          show_source: "Show source",
          fullscreen: "Fullscreen",
        },
      }),
  }),
}));

// CodeBlockStatic depends on lowlight which has a heavy import surface and a
// jsdom-incompatible code path. Stub to keep the source-view test focused on
// the toggle wiring rather than highlighting.
vi.mock("./code-block-static", () => ({
  CodeBlockStatic: ({ body }: { body: string }) => (
    <pre data-testid="code-block-static">{body}</pre>
  ),
}));

import { HtmlBlockPreview } from "./html-block-preview";

afterEach(() => vi.restoreAllMocks());

describe("HtmlBlockPreview — preview / source toggle", () => {
  it("renders the iframe with sandbox and the fragment-nav shim in srcdoc", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    // Two iframes exist after mount — the inline 480px one and the
    // (hidden) Dialog one. Both carry the same srcdoc.
    const frames = document.querySelectorAll("iframe");
    expect(frames.length).toBeGreaterThanOrEqual(1);
    const frame = frames[0]!;
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    const srcdoc = frame.getAttribute("srcdoc") ?? "";
    expect(srcdoc.startsWith("<p>hi</p>")).toBe(true);
    expect(srcdoc).toContain("scrollIntoView");
  });

  it("switches to source view and back when the toggle is clicked", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    // Preview mode: iframe present, code-block-static absent.
    expect(document.querySelector("iframe")).toBeTruthy();
    expect(screen.queryByTestId("code-block-static")).toBeNull();

    fireEvent.click(screen.getByTitle("Show source"));
    expect(screen.getByTestId("code-block-static").textContent).toBe("<p>hi</p>");
    // The inline iframe is gone in source mode; the Dialog's iframe stays
    // unmounted because the Dialog is closed.
    expect(document.querySelector("iframe")).toBeNull();

    fireEvent.click(screen.getByTitle("Show preview"));
    expect(document.querySelector("iframe")).toBeTruthy();
    expect(screen.queryByTestId("code-block-static")).toBeNull();
  });
});

describe("HtmlBlockPreview — Maximize → Dialog", () => {
  it("does not render the Fullscreen button in source view (only when iframe is visible)", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    expect(screen.getByTitle("Fullscreen")).toBeTruthy();
    fireEvent.click(screen.getByTitle("Show source"));
    expect(screen.queryByTitle("Fullscreen")).toBeNull();
  });

  it("opens the fullscreen Dialog with a second iframe carrying the same srcdoc", () => {
    render(<HtmlBlockPreview html="<p>hi</p>" />);
    // Before clicking Fullscreen, the Dialog has not mounted its content
    // (base-ui dialog renders Popup lazily).
    expect(document.querySelectorAll("iframe").length).toBe(1);

    fireEvent.click(screen.getByTitle("Fullscreen"));

    const frames = document.querySelectorAll("iframe");
    expect(frames.length).toBe(2);
    // Both iframes wrap the same body via the fragment-nav shim.
    for (const f of frames) {
      const srcdoc = f.getAttribute("srcdoc") ?? "";
      expect(srcdoc.startsWith("<p>hi</p>")).toBe(true);
      expect(srcdoc).toContain("scrollIntoView");
      expect(f.getAttribute("sandbox")).toBe("allow-scripts");
    }
  });
});
