import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import type { ScrollRestorationAdapter } from "../platform";
import { ScrollRestorationProvider } from "../platform";
import { AttachmentPreviewPage } from "./attachment-preview-page";
import { hashString } from "../editor/utils/hash-string";
import { buildScrollBridge } from "../editor/utils/iframe-scroll-bridge";

const htmlText = "<html><body><h1>Report</h1></body></html>";

vi.mock("../editor/hooks/use-attachment-html-text", () => ({
  useAttachmentHtmlText: () => ({
    data: { text: htmlText },
    isLoading: false,
    error: null,
  }),
}));

vi.mock("../i18n", () => ({
  useT: () => (fn: (d: Record<string, unknown>) => string) =>
    fn({ attachment: { preview_loading: "l", preview_failed: "f" } }),
}));

describe("AttachmentPreviewPage", () => {
  it("injects the scroll bridge under the desktop adapter and keys the iframe on contentKey", () => {
    const adapter: ScrollRestorationAdapter = {
      get: () => undefined,
      registerExternalSource: vi.fn(),
    };
    const { container } = render(
      <ScrollRestorationProvider adapter={adapter}>
        <AttachmentPreviewPage attachmentId="att-1" />
      </ScrollRestorationProvider>,
    );
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
    const { container } = render(
      <ScrollRestorationProvider adapter={webAdapter}>
        <AttachmentPreviewPage attachmentId="att-1" />
      </ScrollRestorationProvider>,
    );
    const iframe = container.querySelector("iframe")!;
    expect(iframe.getAttribute("srcdoc")).not.toContain("__multica");
    expect(iframe.getAttribute("srcdoc")).not.toContain(buildScrollBridge("x"));
    // Still applies the fragment-nav shim on both surfaces.
    expect(iframe.getAttribute("srcdoc")).toContain("scrollIntoView");
  });
});
