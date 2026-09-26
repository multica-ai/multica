import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const { getAttachmentTextContentMock } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getAttachmentTextContent: getAttachmentTextContentMock,
    getAttachment: vi.fn(),
  },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

// HtmlAttachmentPreview (kind="html" dispatch from AttachmentBlock) reads
// useNavigation() + useWorkspaceSlug() for the Open-in-new-tab button.
// Mock both so the standalone-attachment-routes-to-iframe test does not
// need the surrounding NavigationProvider / WorkspaceSlugProvider tree.
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams(),
    hash: "",
    openInNewTab: vi.fn(),
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspaceSlug: () => "acme",
  };
});

import { collectDeliverableFiles } from "@multica/core/attachments/deliverables";
import { renderWithI18n } from "../../test/i18n";
import { AttachmentList } from "./comment-card";
import { AttachmentVersionsProvider } from "./deliverables/attachment-versions";

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

describe("AttachmentList — standalone HTML attachment routes through AttachmentBlock", () => {
  // Regression pin for comment-card.tsx:152. This is the entry point
  // MUL-2330 originally regressed on: standalone HTML attachments (not
  // referenced inline in the markdown body) MUST render through
  // <AttachmentBlock> so the html+attachmentId dispatch fires. Reverting to
  // <AttachmentCard> here re-introduces the "report.html shows as a bare
  // file card row instead of the rendered chart" bug.
  it("renders an iframe (no file-card chrome) for a standalone HTML attachment", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>chart</p>",
      originalContentType: "text/html",
    });
    const attachment = {
      id: "att-1",
      url: "/uploads/report.html",
      filename: "report.html",
      content_type: "text/html",
      size_bytes: 0,
    } as any;

    renderWithQuery(<AttachmentList attachments={[attachment]} content="" />);

    const frame = await waitFor(() => {
      const f = document.querySelector("iframe") as HTMLIFrameElement | null;
      expect(f).toBeTruthy();
      return f!;
    });
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("srcdoc")).toContain("<p>chart</p>");
    // AttachmentCard chrome would render the filename as visible <p> text;
    // HtmlAttachmentPreview replaces the row entirely.
    expect(screen.queryByText("report.html")).toBeNull();
  });
});

describe("AttachmentList — inline attachment filtering", () => {
  it("does not render a bottom attachment row when the body already has the stable file-card URL", () => {
    const id = "11111111-2222-3333-4444-555555555555";
    const href = `/api/attachments/${id}/download`;
    const attachment = {
      id,
      url: "/uploads/report.pdf",
      filename: "report.pdf",
      content_type: "application/pdf",
      size_bytes: 1024,
    } as any;

    const { container } = renderWithQuery(
      <AttachmentList
        attachments={[attachment]}
        content={`!file[report.pdf](${href})`}
      />,
    );

    expect(screen.queryByText("report.pdf")).toBeNull();
    expect(container.firstChild).toBeNull();
  });

  it("does not render a bottom attachment row when the body already has the response download_url", () => {
    const href = "https://cdn.example.test/report.pdf?Signature=stale";
    const attachment = {
      id: "11111111-2222-3333-4444-555555555555",
      url: "/uploads/report.pdf",
      download_url: "https://cdn.example.test/report.pdf?Signature=fresh",
      filename: "report.pdf",
      content_type: "application/pdf",
      size_bytes: 1024,
    } as any;

    const { container } = renderWithQuery(
      <AttachmentList
        attachments={[attachment]}
        content={`!file[report.pdf](${href})`}
      />,
    );

    expect(screen.queryByText("report.pdf")).toBeNull();
    expect(container.firstChild).toBeNull();
  });
});

describe("AttachmentList — layout (MUL-7649)", () => {
  const file = (id: string, filename: string, content_type: string) =>
    ({
      id,
      url: `/uploads/${filename}`,
      download_url: `/uploads/${filename}`,
      filename,
      content_type,
      size_bytes: 2048,
      created_at: "2026-09-20T10:00:00Z",
    }) as any;

  function renderList(ui: ReactElement) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    return renderWithI18n(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
  }

  it("shows files as cards, not full-width rows", () => {
    renderList(
      <AttachmentList
        attachments={[file("a", "notes.md", "text/markdown"), file("b", "data.csv", "text/csv")]}
        content=""
      />,
    );
    expect(screen.getByRole("button", { name: "notes.md" })).toBeTruthy();
    expect(screen.getByText("MD · 2 KB")).toBeTruthy();
    expect(screen.getByText("CSV · 2 KB")).toBeTruthy();
    // The row form's Eye button is gone — the whole card opens the file.
    expect(screen.queryByTitle("Preview")).toBeNull();
  });

  it("puts several images in one row of tiles, and keeps a lone image full size", () => {
    const { container, unmount } = renderList(
      <AttachmentList
        attachments={[file("a", "a.png", "image/png"), file("b", "b.png", "image/png")]}
        content=""
      />,
    );
    expect(container.querySelectorAll(".image-tile")).toHaveLength(2);
    unmount();

    const single = renderList(
      <AttachmentList attachments={[file("c", "c.png", "image/png")]} content="" />,
    );
    expect(single.container.querySelector(".image-figure")).toBeTruthy();
    expect(single.container.querySelector(".image-tile")).toBeNull();
  });

  it("lays groups out images, then files, whatever the upload order", () => {
    const { container } = renderList(
      <AttachmentList
        attachments={[
          file("a", "notes.md", "text/markdown"),
          file("b", "a.png", "image/png"),
          file("c", "b.png", "image/png"),
        ]}
        content=""
      />,
    );
    const tile = container.querySelector(".image-tile")!;
    const card = screen.getByRole("button", { name: "notes.md" });
    expect(tile.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("marks a re-uploaded file with its version on the issue page", () => {
    const v1 = { ...file("v1", "report.md", "text/markdown"), comment_id: "c1" };
    const v2 = { ...file("v2", "report.md", "text/markdown"), comment_id: "c2", created_at: "2026-09-21T10:00:00Z" };
    const files = collectDeliverableFiles([
      { id: "c1", attachments: [v1] },
      { id: "c2", attachments: [v2] },
    ]);
    renderList(
      <AttachmentVersionsProvider files={files}>
        <AttachmentList attachments={[v2]} content="" />
      </AttachmentVersionsProvider>,
    );
    expect(screen.getByText("v2")).toBeTruthy();
  });
});
