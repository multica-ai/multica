import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, screen } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { fitViewport, HtmlViewportFrame } from "./html-viewport-frame";
import { HTML_PREVIEW_WIDTH_KEY } from "./utils/iframe-width-bridge";

describe("fitViewport", () => {
  it("keeps the device width and scales it down into a narrower area", () => {
    expect(fitViewport({ width: 1100, height: 800 }, 1440)).toEqual({
      width: 1440,
      height: 1047,
      scale: 1100 / 1440,
    });
  });

  it("never scales up past the device's own size", () => {
    expect(fitViewport({ width: 1600, height: 900 }, 390)).toEqual({
      width: 390,
      height: 900,
      scale: 1,
    });
  });
});

describe("HtmlViewportFrame", () => {
  // jsdom has no layout: give every element the stage area's layout box.
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(1100);
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(800);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  const frame = <iframe title="doc" className="h-full w-full" />;

  it("fills the area with no caption by default", () => {
    renderWithI18n(<HtmlViewportFrame viewport="fill">{frame}</HtmlViewportFrame>);
    expect(screen.getByTestId("html-viewport").style.width).toBe("100%");
    expect(screen.queryByText(/×/)).toBeNull();
  });

  it("lays a wider device out at full size and scales it to fit, saying by how much", () => {
    renderWithI18n(<HtmlViewportFrame viewport="desktop">{frame}</HtmlViewportFrame>);

    const box = screen.getByTestId("html-viewport");
    expect(box.style.width).toBe("1100px");
    const layout = screen.getByTitle("doc").parentElement!;
    expect(layout.style.width).toBe("1440px");
    expect(layout.style.height).toBe("1047px");
    expect(layout.style.transform).toBe(`scale(${1100 / 1440})`);
    expect(screen.getByText(/1440 × 1047/)).toHaveTextContent("1440 × 1047 · Scaled to 76%");
  });

  it("shows a device that fits at its own size", () => {
    renderWithI18n(<HtmlViewportFrame viewport="phone">{frame}</HtmlViewportFrame>);

    expect(screen.getByTestId("html-viewport").style.width).toBe("390px");
    const layout = screen.getByTitle("doc").parentElement!;
    expect(layout.style.transform).toBe("");
    expect(screen.getByText("390 × 800")).toBeTruthy();
  });

  function report(data: Record<string, unknown>, source = screen.getByTitle("doc")) {
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { [HTML_PREVIEW_WIDTH_KEY]: 1, type: "width", ...data },
          source: (source as HTMLIFrameElement).contentWindow,
        }),
      );
    });
  }

  describe("fit", () => {
    it("lays a document wider than the stage out at its own width and scales it to fit", () => {
      renderWithI18n(<HtmlViewportFrame viewport="fill">{frame}</HtmlViewportFrame>);
      report({ doc: "a", viewport: 1100, needed: 1320 });

      expect(screen.getByTestId("html-viewport").style.width).toBe("1100px");
      const layout = screen.getByTitle("doc").parentElement!;
      expect(layout.style.width).toBe("1320px");
      expect(layout.style.transform).toBe(`scale(${1100 / 1320})`);
      expect(screen.getByText(/1320 × 960/)).toHaveTextContent("1320 × 960 · Scaled to 83%");
    });

    it("keeps the width once the widened document stops overflowing", () => {
      renderWithI18n(<HtmlViewportFrame viewport="fill">{frame}</HtmlViewportFrame>);
      report({ doc: "a", viewport: 1100, needed: 1320 });
      report({ doc: "a", viewport: 1320, needed: 1320 });

      expect(screen.getByTitle("doc").parentElement!.style.width).toBe("1320px");
    });

    it("starts over for a new document", () => {
      renderWithI18n(<HtmlViewportFrame viewport="fill">{frame}</HtmlViewportFrame>);
      report({ doc: "a", viewport: 1100, needed: 1320 });
      report({ doc: "b", viewport: 1320, needed: 1320 });

      expect(screen.getByTestId("html-viewport").style.width).toBe("100%");
      expect(screen.queryByText(/×/)).toBeNull();
    });

    it("stops widening at twice the desktop width", () => {
      renderWithI18n(<HtmlViewportFrame viewport="fill">{frame}</HtmlViewportFrame>);
      report({ doc: "a", viewport: 1100, needed: 9000 });

      expect(screen.getByTitle("doc").parentElement!.style.width).toBe("2880px");
    });

    it("ignores reports from any other window", () => {
      renderWithI18n(
        <>
          <HtmlViewportFrame viewport="fill">{frame}</HtmlViewportFrame>
          <iframe title="other" />
        </>,
      );
      report({ doc: "a", viewport: 1100, needed: 1320 }, screen.getByTitle("other"));

      expect(screen.getByTestId("html-viewport").style.width).toBe("100%");
    });

    it("leaves device widths alone", () => {
      renderWithI18n(<HtmlViewportFrame viewport="phone">{frame}</HtmlViewportFrame>);
      report({ doc: "a", viewport: 390, needed: 600 });

      expect(screen.getByTestId("html-viewport").style.width).toBe("390px");
    });
  });

  it("localizes the scale", () => {
    renderWithI18n(<HtmlViewportFrame viewport="desktop">{frame}</HtmlViewportFrame>, {
      locale: "zh-Hans",
    });
    expect(screen.getByText(/缩放/)).toHaveTextContent("1440 × 1047 · 缩放 76%");
  });
});
