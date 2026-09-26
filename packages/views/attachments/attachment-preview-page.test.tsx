import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import type { Attachment } from "@multica/core/types";
import type { ScrollRestorationAdapter } from "../platform";
import { ScrollRestorationProvider } from "../platform";
import { renderWithI18n } from "../test/i18n";
import { AttachmentPreviewPage } from "./attachment-preview-page";
import { hashString } from "../editor/utils/hash-string";
import { buildScrollBridge } from "../editor/utils/iframe-scroll-bridge";

const htmlText = "<html><body><h1>Report</h1></body></html>";

const { getAttachmentMock, getAttachmentTextContentMock } = vi.hoisted(() => ({
  getAttachmentMock: vi.fn(),
  getAttachmentTextContentMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getAttachment: getAttachmentMock,
    getAttachmentTextContent: getAttachmentTextContentMock,
    getBaseUrl: () => "",
  },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("../editor/use-download-attachment", () => ({
  useDownloadAttachment: () => vi.fn(),
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: index,
        start: index * 37,
        end: (index + 1) * 37,
        size: 37,
      })),
    getTotalSize: () => count * 37,
    measureElement: () => {},
  }),
}));

function makeAttachment(overrides: Partial<Attachment>): Attachment {
  return {
    id: "att-1",
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "u-1",
    filename: "report.html",
    url: "https://cdn.example.test/att-1",
    download_url: "https://cdn.example.test/att-1?Signature=s",
    markdown_url: "https://cdn.example.test/att-1",
    content_type: "text/html",
    size_bytes: 1024,
    created_at: "2026-05-13T00:00:00Z",
    ...overrides,
  };
}

function renderPage(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  getAttachmentMock.mockReset();
  getAttachmentTextContentMock.mockReset();
  getAttachmentMock.mockResolvedValue(makeAttachment({}));
  getAttachmentTextContentMock.mockResolvedValue({
    text: htmlText,
    originalContentType: "text/html",
  });
});

describe("AttachmentPreviewPage", () => {
  it("shows the viewer's bar and stage — without a close button, since the tab is the frame", async () => {
    renderPage(<AttachmentPreviewPage attachmentId="att-1" filename="report.html" />);

    expect(await screen.findByTitle("report.html")).toBeTruthy();
    expect(screen.getByText("report.html")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Download" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Fit" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.queryByRole("button", { name: "Close" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Open in new tab" })).toBeNull();
    expect(document.title).toBe("report.html");
  });

  it("injects the scroll bridge under the desktop adapter and keys the iframe on contentKey", async () => {
    const adapter: ScrollRestorationAdapter = {
      get: () => undefined,
      registerExternalSource: vi.fn(),
    };
    renderPage(
      <ScrollRestorationProvider adapter={adapter}>
        <AttachmentPreviewPage attachmentId="att-1" />
      </ScrollRestorationProvider>,
    );
    const iframe = await screen.findByTitle("report.html");
    expect(iframe.getAttribute("srcdoc")).toContain(buildScrollBridge(hashString(htmlText)));
    // The iframe stays inside the sandbox the viewer uses.
    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts");
    expect(adapter.registerExternalSource).toHaveBeenCalledWith(
      "html-iframe",
      expect.objectContaining({ capture: expect.any(Function) }),
    );
  });

  it("does NOT inject the bridge or register a source on web (adapter without registerExternalSource)", async () => {
    const webAdapter: ScrollRestorationAdapter = { get: () => undefined };
    renderPage(
      <ScrollRestorationProvider adapter={webAdapter}>
        <AttachmentPreviewPage attachmentId="att-1" />
      </ScrollRestorationProvider>,
    );
    const iframe = await screen.findByTitle("report.html");
    expect(iframe.getAttribute("srcdoc")).not.toContain("__multica");
    expect(iframe.getAttribute("srcdoc")).not.toContain(buildScrollBridge("x"));
    // Still applies the fragment-nav shim on both surfaces.
    expect(iframe.getAttribute("srcdoc")).toContain("scrollIntoView");
  });

  it("shows an image", async () => {
    getAttachmentMock.mockResolvedValue(
      makeAttachment({ filename: "shot.png", content_type: "image/png" }),
    );
    renderPage(<AttachmentPreviewPage attachmentId="att-1" />);
    const img = await screen.findByAltText("shot.png");
    expect(img.getAttribute("src")).toBe("https://cdn.example.test/att-1?Signature=s");
  });

  it("shows a CSV as a table", async () => {
    getAttachmentMock.mockResolvedValue(
      makeAttachment({ filename: "report.csv", content_type: "text/csv" }),
    );
    getAttachmentTextContentMock.mockResolvedValue({
      text: "name,count\nalpha,3\n",
      originalContentType: "text/csv",
    });
    renderPage(<AttachmentPreviewPage attachmentId="att-1" />);
    expect(await screen.findByTestId("table-preview")).toBeTruthy();
    expect(screen.getByText("alpha")).toBeTruthy();
  });

  it("titles the tab from the URL's name until the record lands", () => {
    getAttachmentMock.mockReturnValue(new Promise(() => {}));
    renderPage(<AttachmentPreviewPage attachmentId="att-1" filename="from-url.pdf" />);
    expect(screen.getByText("Loading preview…")).toBeTruthy();
    expect(document.title).toBe("from-url.pdf");
  });

  it("says the preview failed when the record cannot be loaded", async () => {
    getAttachmentMock.mockRejectedValue(new Error("404"));
    renderPage(<AttachmentPreviewPage attachmentId="att-1" />);
    await waitFor(() =>
      expect(screen.getByTestId("attachment-preview-page-error")).toHaveTextContent(
        "Couldn't load preview",
      ),
    );
  });
});
