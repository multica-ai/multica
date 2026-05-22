import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: { download: "Download" },
        attachment: {
          preview_loading: "Loading preview…",
          preview_failed: "Couldn't load preview",
          close: "Close",
          open_in_new_tab: "Open in new tab",
        },
      }),
  }),
}));

import {
  HtmlArtifactPreviewModal,
  HtmlArtifactPreviewPage,
  downloadHtmlArtifact,
  readStoredHtmlArtifactPreview,
  storeHtmlArtifactPreview,
} from "./html-artifact-preview";

const originalCreateObjectURL = URL.createObjectURL;
const originalRevokeObjectURL = URL.revokeObjectURL;

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
  URL.createObjectURL = originalCreateObjectURL;
  URL.revokeObjectURL = originalRevokeObjectURL;
});

describe("inline HTML artifact storage", () => {
  it("stores and reads HTML for same-origin full-page preview", () => {
    vi.spyOn(crypto, "randomUUID").mockReturnValue(
      "11111111-1111-1111-1111-111111111111",
    );

    const key = storeHtmlArtifactPreview("<main>ok</main>", "demo");

    expect(key).toBe("11111111-1111-1111-1111-111111111111");
    expect(readStoredHtmlArtifactPreview(key)).toMatchObject({
      html: "<main>ok</main>",
      filename: "demo.html",
    });
  });
});

describe("HtmlArtifactPreviewModal", () => {
  it("renders a full-screen iframe with the same sandbox posture and actions", () => {
    const onClose = vi.fn();
    const onOpenInNewTab = vi.fn();
    const onDownload = vi.fn();

    render(
      <HtmlArtifactPreviewModal
        html={'<a href="#target">jump</a><section id="target">target</section>'}
        filename="artifact.html"
        open
        onClose={onClose}
        onOpenInNewTab={onOpenInNewTab}
        onDownload={onDownload}
      />,
    );

    const frame = document.querySelector<HTMLIFrameElement>("iframe");
    expect(frame).not.toBeNull();
    expect(frame?.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame?.getAttribute("srcdoc")).toContain("scrollIntoView");

    fireEvent.click(screen.getByTitle("Open in new tab"));
    expect(onOpenInNewTab).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByTitle("Download"));
    expect(onDownload).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("HtmlArtifactPreviewPage", () => {
  it("loads stored HTML into a full-page sandboxed iframe", async () => {
    vi.spyOn(crypto, "randomUUID").mockReturnValue(
      "22222222-2222-2222-2222-222222222222",
    );
    const key = storeHtmlArtifactPreview("<h1>full page</h1>", "page.html");

    render(<HtmlArtifactPreviewPage artifactKey={key} />);

    const frame = await waitFor(() => {
      const next = document.querySelector<HTMLIFrameElement>("iframe");
      expect(next).not.toBeNull();
      return next!;
    });
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("srcdoc")).toContain("<h1>full page</h1>");
    expect(frame.className).toContain("h-svh");
    expect(document.title).toBe("page.html");
  });
});

describe("downloadHtmlArtifact", () => {
  it("downloads inline HTML as an .html file", () => {
    const click = vi.fn();
    const appendChild = vi.spyOn(document.body, "appendChild");
    vi.spyOn(document, "createElement").mockImplementation((tagName) => {
      const element = document.createElementNS(
        "http://www.w3.org/1999/xhtml",
        tagName,
      ) as HTMLElement;
      if (tagName === "a") {
        Object.defineProperty(element, "click", { value: click });
      }
      return element;
    });
    URL.createObjectURL = vi.fn(() => "blob:artifact");
    URL.revokeObjectURL = vi.fn();

    downloadHtmlArtifact("<p>download</p>", "demo");

    expect(URL.createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
    const anchor = appendChild.mock.calls[0]?.[0] as HTMLAnchorElement;
    expect(anchor.download).toBe("demo.html");
    expect(anchor.href).toBe("blob:artifact");
    expect(click).toHaveBeenCalledTimes(1);
  });
});
