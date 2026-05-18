import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const { getAttachmentTextContentMock } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: getAttachmentTextContentMock },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: { download: "Download" },
        attachment: {
          preview: "Preview",
          preview_loading: "Loading preview…",
          preview_failed: "Couldn't load preview",
        },
        code_block: { copy_code: "Copy code" },
        file_card: { uploading: "Uploading {{filename}}" },
      }),
  }),
}));

import { AttachmentBlock } from "./attachment-block";

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

describe("AttachmentBlock — dispatcher", () => {
  it("routes html + attachmentId to HtmlAttachmentPreview (no file-card chrome)", () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>chart</p>",
      originalContentType: "text/html",
    });
    renderWithQuery(
      <AttachmentBlock
        filename="report.html"
        contentType="text/html"
        attachmentId="att-1"
        href="https://cdn.example/report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    // HtmlAttachmentPreview never renders the filename row — that's the
    // file-card chrome it replaces.
    expect(screen.queryByText("report.html")).toBeNull();
    // Toolbar shows the Maximize-style preview button.
    expect(screen.getByTitle("Preview")).toBeTruthy();
    expect(screen.getByTitle("Copy code")).toBeTruthy();
  });

  it("routes html WITHOUT attachmentId to AttachmentCard (URL-only is chrome-only)", () => {
    renderWithQuery(
      <AttachmentBlock
        filename="report.html"
        contentType="text/html"
        href="https://cdn.example/report.html"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.getByText("report.html")).toBeTruthy();
    expect(document.querySelector("iframe")).toBeNull();
  });

  it("routes html while uploading to AttachmentCard (no iframe before upload finishes)", () => {
    renderWithQuery(
      <AttachmentBlock
        filename="report.html"
        contentType="text/html"
        attachmentId="att-1"
        href="https://cdn.example/report.html"
        uploading
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    // Uploading state surfaces the chrome row with the upload template.
    expect(screen.getByText("Uploading {{filename}}")).toBeTruthy();
    expect(document.querySelector("iframe")).toBeNull();
  });

  it("routes non-html kinds (pdf, image, other) to AttachmentCard", () => {
    renderWithQuery(
      <AttachmentBlock
        filename="manual.pdf"
        contentType="application/pdf"
        attachmentId="att-1"
        href="https://cdn.example/manual.pdf"
        onPreview={() => {}}
        onDownload={() => {}}
      />,
    );
    expect(screen.getByText("manual.pdf")).toBeTruthy();
    expect(document.querySelector("iframe")).toBeNull();
  });
});
