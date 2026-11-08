import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { collectDeliverableFiles } from "@multica/core/attachments/deliverables";
import type { Attachment } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../../navigation";
import { renderWithI18n } from "../../../test/i18n";

const { openAtMock, tryOpenMock, downloadMock, copyTextMock, toastMock } = vi.hoisted(() => ({
  openAtMock: vi.fn((_key: string) => true),
  tryOpenMock: vi.fn(() => false),
  downloadMock: vi.fn(),
  copyTextMock: vi.fn(async (_text: string) => true),
  toastMock: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("../../../editor", () => ({
  usePreviewSequence: () => ({ openAt: openAtMock }),
  useAttachmentPreview: () => ({ open: vi.fn(), tryOpen: tryOpenMock, modal: null }),
  useDownloadAttachment: () => downloadMock,
}));

vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: copyTextMock }));
vi.mock("sonner", () => ({ toast: toastMock }));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});

import { OpenDeliverableOnLoad, useCopyDeliverableLink } from "./deliverable-link";

function attachment(over: Partial<Attachment> & { id: string }): Attachment {
  return {
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: "c-1",
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "agent",
    uploader_id: "lambda",
    filename: "调研 报告.html",
    url: `https://cdn.example.test/${over.id}`,
    download_url: `https://cdn.example.test/${over.id}?sig=1`,
    markdown_url: `https://cdn.example.test/${over.id}`,
    content_type: "text/html",
    size_bytes: 1024,
    created_at: "2026-09-20T10:00:00Z",
    ...over,
  };
}

const V1 = attachment({ id: "v1", comment_id: "c-1", created_at: "2026-09-20T10:00:00Z" });
const V2 = attachment({ id: "v2", comment_id: "c-2", created_at: "2026-09-21T10:00:00Z" });
const ONE_VERSION = collectDeliverableFiles([{ id: "c-1", attachments: [V1] }]);
const TWO_VERSIONS = collectDeliverableFiles([
  { id: "c-1", attachments: [V1] },
  { id: "c-2", attachments: [V2] },
]);

function navigation(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues/MUL-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p: string) => `https://app.example.test${p}`,
  };
}

function withNav(children: ReactNode) {
  return <NavigationProvider value={navigation()}>{children}</NavigationProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("OpenDeliverableOnLoad", () => {
  function renderOpener(props: Partial<Parameters<typeof OpenDeliverableOnLoad>[0]> = {}) {
    const onOpenedInViewer = vi.fn();
    const onDone = vi.fn();
    const all = {
      name: "调研 报告.html",
      files: ONE_VERSION,
      ready: true,
      onOpenedInViewer,
      onDone,
      ...props,
    };
    const view = renderWithI18n(<OpenDeliverableOnLoad {...all} />);
    return { ...view, onOpenedInViewer, onDone, props: all };
  }

  it("opens the newest version in the page's viewer", () => {
    const { onOpenedInViewer, onDone } = renderOpener({ files: TWO_VERSIONS });
    expect(openAtMock).toHaveBeenCalledTimes(1);
    expect(openAtMock).toHaveBeenCalledWith("v2");
    expect(onOpenedInViewer).toHaveBeenCalledTimes(1);
    // The viewer is still open; the host puts the address back when it closes.
    expect(onDone).not.toHaveBeenCalled();
  });

  it("opens the new version when the same link is visited after a re-upload", () => {
    const first = renderOpener({ files: ONE_VERSION });
    expect(openAtMock).toHaveBeenLastCalledWith("v1");
    first.unmount();

    renderOpener({ files: TWO_VERSIONS });
    expect(openAtMock).toHaveBeenLastCalledWith("v2");
  });

  it("waits for the timeline before deciding", () => {
    const { rerender, onDone, props } = renderOpener({ files: [], ready: false });
    expect(openAtMock).not.toHaveBeenCalled();
    expect(onDone).not.toHaveBeenCalled();
    expect(toastMock.error).not.toHaveBeenCalled();

    rerender(<OpenDeliverableOnLoad {...props} files={ONE_VERSION} ready />);
    expect(openAtMock).toHaveBeenCalledWith("v1");
    expect(toastMock.error).not.toHaveBeenCalled();
  });

  it("does not re-open when a new version arrives while the page is open", () => {
    const { rerender, props } = renderOpener({ files: ONE_VERSION });
    rerender(<OpenDeliverableOnLoad {...props} files={TWO_VERSIONS} />);
    expect(openAtMock).toHaveBeenCalledTimes(1);
  });

  it("says so, and gives the address back, when no deliverable has the name", () => {
    const { onDone, onOpenedInViewer } = renderOpener({ name: "missing.html" });
    expect(openAtMock).not.toHaveBeenCalled();
    expect(toastMock.error).toHaveBeenCalledWith(
      expect.stringContaining("missing.html"),
    );
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(onOpenedInViewer).not.toHaveBeenCalled();
  });

  it("falls back to the standalone viewer or a download outside the sequence", () => {
    // Not in the sequence: the opener's check and the fallback's both say so.
    openAtMock.mockReturnValueOnce(false).mockReturnValueOnce(false);
    const zip = collectDeliverableFiles([
      {
        id: "c-1",
        attachments: [attachment({ id: "zip", filename: "bundle.zip", content_type: "application/zip" })],
      },
    ]);
    const { onDone, onOpenedInViewer } = renderOpener({ name: "bundle.zip", files: zip });
    expect(tryOpenMock).toHaveBeenCalled();
    expect(downloadMock).toHaveBeenCalledWith("zip");
    expect(onOpenedInViewer).not.toHaveBeenCalled();
    expect(onDone).toHaveBeenCalledTimes(1);
  });
});

describe("useCopyDeliverableLink", () => {
  it("copies the latest-version link, not a link to one upload", async () => {
    const { result } = renderHook(() => useCopyDeliverableLink("MUL-1"), {
      wrapper: ({ children }) => withNav(children),
    });
    result.current?.("调研 报告.html");
    await waitFor(() => expect(toastMock.success).toHaveBeenCalled());
    expect(copyTextMock).toHaveBeenCalledWith(
      "https://app.example.test/acme/issues/MUL-1/deliverables/%E8%B0%83%E7%A0%94%20%E6%8A%A5%E5%91%8A.html",
    );
  });

  it("reports a failed copy", async () => {
    copyTextMock.mockResolvedValueOnce(false);
    const { result } = renderHook(() => useCopyDeliverableLink("MUL-1"), {
      wrapper: ({ children }) => withNav(children),
    });
    result.current?.("report.html");
    await waitFor(() => expect(toastMock.error).toHaveBeenCalled());
  });

  it("offers nothing without an issue to link into", () => {
    const { result } = renderHook(() => useCopyDeliverableLink(undefined), {
      wrapper: ({ children }) => withNav(children),
    });
    expect(result.current).toBeUndefined();
  });
});
