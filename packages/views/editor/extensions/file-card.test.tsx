import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// Tiptap NodeView primitives can't be instantiated without a full editor.
// Stub the wrapper so FileCardView renders as a plain React component and
// the DOM can be inspected directly.
vi.mock("@tiptap/react", () => ({
  NodeViewWrapper: ({ children, ...rest }: any) => <div {...rest}>{children}</div>,
}));

const { getAttachmentTextContentMock, resolveAttachmentMock, openByUrlMock, tryOpenMock } =
  vi.hoisted(() => ({
    getAttachmentTextContentMock: vi.fn(),
    resolveAttachmentMock: vi.fn(),
    openByUrlMock: vi.fn(),
    tryOpenMock: vi.fn(),
  }));

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: getAttachmentTextContentMock },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("../attachment-download-context", () => ({
  useAttachmentDownloadResolver: () => ({
    openByUrl: openByUrlMock,
    resolveAttachment: resolveAttachmentMock,
  }),
}));

vi.mock("../attachment-preview-modal", () => ({
  useAttachmentPreview: () => ({ tryOpen: tryOpenMock, open: vi.fn(), modal: null }),
}));

// The attachment renderer reads useNavigation() + useWorkspaceSlug() on some
// paths. Provide minimal mocks so the component renders without a real
// provider.
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

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (s: Record<string, Record<string, string>>) => string) =>
      sel({
        image: { download: "Download" },
        attachment: {
          preview: "Preview",
          preview_loading: "Loading preview…",
          preview_failed: "Couldn't load preview",
          open_in_new_tab: "Open in new tab",
        },
        code_block: { copy_code: "Copy code" },
        file_card: { uploading: "Uploading {{filename}}" },
      }),
  }),
}));

import { FileCardView } from "./file-card";

function renderWithQuery(ui: ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

describe("FileCardView — HTML attachment", () => {
  // Reverses the MUL-2330 pin: an HTML file is a file (MUL-7649), shown as the
  // file-card row that opens the viewer. HTML meant to be read in place is a
  // ```html block.
  it("renders the file-card row, not an embedded preview, for an HTML attachment", () => {
    resolveAttachmentMock.mockReturnValue({
      id: "att-1",
      content_type: "text/html",
      url: "/uploads/report.html",
      filename: "report.html",
    });
    const node = {
      attrs: {
        href: "/uploads/report.html",
        filename: "report.html",
        uploading: false,
      },
    } as any;

    renderWithQuery(<FileCardView node={node} {...({} as any)} />);

    expect(screen.getByText("report.html")).toBeTruthy();
    expect(document.querySelector("iframe")).toBeNull();
    expect(getAttachmentTextContentMock).not.toHaveBeenCalled();
  });
});
