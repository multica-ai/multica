import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ScrollRestorationAdapter } from "../platform";
import { ScrollRestorationProvider } from "../platform";
import { AttachmentPreviewPage } from "./attachment-preview-page";
import { hashString } from "../editor/utils/hash-string";
import { buildScrollBridge } from "../editor/utils/iframe-scroll-bridge";
import enEditor from "../locales/en/editor.json";

const htmlText = "<html><body><h1>Report</h1></body></html>";

// The HTML body is fetched via this hook; stub it so the page renders the iframe
// branch without a network/query-client.
vi.mock("../editor/hooks/use-attachment-html-text", () => ({
  useAttachmentHtmlText: () => ({
    data: { text: htmlText },
    isLoading: false,
    error: null,
  }),
}));

const resources = { en: { editor: enEditor } };

/** Real i18n (not a `useT` stub) — the find bar reads its placeholder from it. */
function renderPage(adapter?: ScrollRestorationAdapter) {
  const page = (
    <I18nProvider locale="en" resources={resources}>
      <AttachmentPreviewPage attachmentId="att-1" filename="report.html" />
    </I18nProvider>
  );
  return render(
    adapter ? (
      <ScrollRestorationProvider adapter={adapter}>
        {page}
      </ScrollRestorationProvider>
    ) : (
      page
    ),
  );
}

describe("AttachmentPreviewPage", () => {
  it("injects the scroll bridge under the desktop adapter and keys the iframe on contentKey", () => {
    const adapter: ScrollRestorationAdapter = {
      get: () => undefined,
      registerExternalSource: vi.fn(),
    };
    const { container } = renderPage(adapter);
    const iframe = container.querySelector("iframe")!;
    expect(iframe).toBeTruthy();
    expect(iframe.getAttribute("srcdoc")).toContain(
      buildScrollBridge(hashString(htmlText)),
    );
    // React only exposes `key` via the reconciled element, but we can assert
    // the bridge was built for the expected token — that's the signal the
    // component derived contentKey from the text.
    expect(adapter.registerExternalSource).toHaveBeenCalledWith(
      "html-iframe",
      expect.objectContaining({ capture: expect.any(Function) }),
    );
  });

  it("does NOT inject the bridge or register a source on web (adapter without registerExternalSource)", () => {
    const webAdapter: ScrollRestorationAdapter = { get: () => undefined };
    const { container } = renderPage(webAdapter);
    const iframe = container.querySelector("iframe")!;
    expect(iframe.getAttribute("srcdoc")).not.toContain("__multica");
    expect(iframe.getAttribute("srcdoc")).not.toContain(buildScrollBridge("x"));
    // Still applies the fragment-nav shim on both surfaces.
    expect(iframe.getAttribute("srcdoc")).toContain("scrollIntoView");
  });
});

describe("AttachmentPreviewPage — in-page find (#5259)", () => {
  it("does not show the find bar until requested", () => {
    renderPage();
    expect(screen.queryByPlaceholderText("Find in page")).not.toBeInTheDocument();
  });

  it("opens the find bar on Ctrl+F and closes it on Escape", () => {
    renderPage();

    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    expect(screen.getByPlaceholderText("Find in page")).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByPlaceholderText("Find in page")).not.toBeInTheDocument();
  });

  it("opens the find bar on Cmd+F (macOS)", () => {
    renderPage();
    fireEvent.keyDown(window, { key: "f", metaKey: true });
    expect(screen.getByPlaceholderText("Find in page")).toBeInTheDocument();
  });

  it("renders the sandboxed iframe with the injected find shim in srcDoc", () => {
    const { container } = renderPage();
    const iframe = container.querySelector("iframe");
    expect(iframe).toBeTruthy();
    // Sandbox posture preserved: allow-scripts, never allow-same-origin.
    expect(iframe!.getAttribute("sandbox")).toBe("allow-scripts");
    expect(iframe!.getAttribute("srcdoc")).toContain("multica-find-cmd");
  });

  // The find shim rides on top of the scroll-restore srcDoc (multica-ai#6405),
  // so both must survive under the desktop adapter.
  it("keeps both the scroll bridge and the find shim under the desktop adapter", () => {
    const adapter: ScrollRestorationAdapter = {
      get: () => undefined,
      registerExternalSource: vi.fn(),
    };
    const { container } = renderPage(adapter);
    const srcdoc = container.querySelector("iframe")!.getAttribute("srcdoc")!;
    expect(srcdoc).toContain(buildScrollBridge(hashString(htmlText)));
    expect(srcdoc).toContain("multica-find-cmd");
  });
});
