/**
 * MUL-5208 — a link that points back at this deployment is an in-app
 * destination, not an external one.
 *
 * Chat and comments render agent-written content full of absolute URLs. When one
 * of them addresses this app, `window.open` sends it to the system browser on
 * desktop (Electron routes every renderer-opened window through
 * `shell.openExternal`), which is how "click an issue link, get a browser
 * window" happens. The real `openLink` is exercised here — mocking it would test
 * nothing about the routing decision.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { configStore } from "@multica/core/config";
import type { Attachment } from "@multica/core/types";

const APP_ORIGIN = "https://app.example";

vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: () => null,
}));

// Only the workspace hooks are stubbed — the real path helpers stay in place so
// the reserved-slug rule that decides in-app vs external is the shipped one.
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
    projectDetail: (id: string) => `/test/projects/${id}`,
  }),
  useWorkspaceSlug: () => "test",
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
  useOptionalNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
  resolveClickIntent: () => "push",
  useAppOrigin: () => APP_ORIGIN,
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

vi.mock("../editor/link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

// Intercepted-attachment-link click handler. A plain `<a>` to the API-host
// attachment endpoint cannot authenticate a bare navigation (WK-11), and a raw
// CDN link lands on the storage host's media page (MUL-9566), so the renderer
// opens the file in the viewer and downloads only what the viewer cannot show.
// The download hook is mocked so the test asserts the dispatch, not the
// download plumbing.
const downloadAttachmentSpy = vi.fn();
vi.mock("../editor/use-download-attachment", () => ({
  useDownloadAttachment: () => downloadAttachmentSpy,
}));

const getAttachmentForDownload = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "", getAttachmentForDownload },
}));

const toastError = vi.hoisted(() => vi.fn());
vi.mock("sonner", () => ({ toast: { error: toastError, success: vi.fn() } }));

const CDN_DOMAIN = "static.example";
const WS_ID = "0e0e0e0e-0000-4000-8000-000000000000";

function attachmentRecord(id: string, filename: string, contentType: string): Attachment {
  return {
    id,
    workspace_id: WS_ID,
    issue_id: null,
    comment_id: null,
    uploader_type: "member",
    uploader_id: "u",
    filename,
    url: `https://${CDN_DOMAIN}/workspaces/${WS_ID}/${id}`,
    download_url: `https://${CDN_DOMAIN}/workspaces/${WS_ID}/${id}?Signature=s`,
    content_type: contentType,
    size_bytes: 1024,
    created_at: "2026-11-18T00:00:00Z",
  } as Attachment;
}

vi.mock("mermaid", () => ({
  default: { initialize: vi.fn(), render: vi.fn() },
}));

import { RichContent } from "./rich-content";

let navigatedPaths: string[] = [];
let openSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  navigatedPaths = [];
  act(() => {
    configStore.getState().setAttachmentPublicUrl("https://multica-api.erlinerd.com");
    configStore.getState().setCdnConfig({ cdnDomain: CDN_DOMAIN });
  });
  window.addEventListener("multica:navigate", captureNavigate);
  openSpy = vi.spyOn(window, "open").mockImplementation(() => null);
});

afterEach(() => {
  window.removeEventListener("multica:navigate", captureNavigate);
  vi.restoreAllMocks();
  downloadAttachmentSpy.mockClear();
  getAttachmentForDownload.mockReset();
  toastError.mockClear();
  act(() => {
    configStore.getState().setAttachmentPublicUrl("");
    configStore.getState().setCdnConfig({ cdnDomain: "" });
  });
});

function captureNavigate(e: Event) {
  const path = (e as CustomEvent<{ path?: string }>).detail?.path;
  if (path) navigatedPaths.push(path);
}

function renderContent(content: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={client}>
      <RichContent content={content} />
    </QueryClientProvider>,
  );
}

describe("RichContent link routing", () => {
  it("routes a link to this deployment into the app instead of the browser", () => {
    renderContent(`[MUL-1](${APP_ORIGIN}/acme/issues/MUL-1)`);

    screen.getByText("MUL-1").click();

    expect(navigatedPaths).toEqual(["/acme/issues/MUL-1"]);
    expect(openSpy).not.toHaveBeenCalled();
  });

  it("still hands a genuinely external link to the browser", () => {
    const external = "https://github.com/multica-ai/multica/pull/1";
    renderContent(`[#1](${external})`);

    screen.getByText("#1").click();

    expect(navigatedPaths).toEqual([]);
    expect(openSpy).toHaveBeenCalledWith(
      external,
      "_blank",
      "noopener,noreferrer",
    );
  });

  it("keeps an attachment download URL on the app origin external", () => {
    const download = `${APP_ORIGIN}/api/attachments/abc/download`;
    renderContent(`[report.pdf](${download})`);

    screen.getByText("report.pdf").click();

    expect(navigatedPaths).toEqual([]);
    expect(openSpy).toHaveBeenCalledWith(
      download,
      "_blank",
      "noopener,noreferrer",
    );
  });

  it("keeps a same-origin /uploads file external — the backend serves it, not the router", () => {
    const upload = `${APP_ORIGIN}/uploads/2026/07/notes.pdf`;
    renderContent(`[notes.pdf](${upload})`);

    screen.getByText("notes.pdf").click();

    expect(navigatedPaths).toEqual([]);
    expect(openSpy).toHaveBeenCalledWith(
      upload,
      "_blank",
      "noopener,noreferrer",
    );
  });

  // WK-11: a plain-link attachment download URL (the shape `buildMarkdownURL`
  // persists into bodies in private-storage deployments — absolute, API-host
  // pinned) cannot authenticate a bare navigation, so it must never reach
  // window.open. A file the viewer cannot show downloads through the
  // credentialed hook.
  it("downloads a non-previewable attachment link through the download hook (WK-11)", async () => {
    const id = "01a13179-08aa-7898-ae86-3a1994d2147e";
    const download = `https://multica-api.erlinerd.com/api/attachments/${id}/download`;
    getAttachmentForDownload.mockResolvedValue(attachmentRecord(id, "audit.zip", "application/zip"));
    renderContent(`[audit.zip](${download})`);

    screen.getByText("audit.zip").click();

    await vi.waitFor(() => expect(downloadAttachmentSpy).toHaveBeenCalledWith(id));
    expect(getAttachmentForDownload).toHaveBeenCalledWith(id);
    expect(openSpy).not.toHaveBeenCalled();
    expect(navigatedPaths).toEqual([]);
  });

  it("recognizes the API-host link when runtime config arrives after rendering", async () => {
    const id = "01a13179-08aa-7898-ae86-3a1994d2147e";
    const download = `https://multica-api.erlinerd.com/api/attachments/${id}/download`;
    getAttachmentForDownload.mockResolvedValue(attachmentRecord(id, "report.zip", "application/zip"));
    act(() => configStore.getState().setAttachmentPublicUrl(""));
    renderContent(`[report.zip](${download})`);

    act(() => configStore.getState().setAttachmentPublicUrl("https://multica-api.erlinerd.com"));
    screen.getByText("report.zip").click();

    await vi.waitFor(() => expect(downloadAttachmentSpy).toHaveBeenCalledWith(id));
    expect(openSpy).not.toHaveBeenCalled();
  });

  // MUL-9566: a video link played on the storage host's native page, in a tab
  // with no way back into the app. It plays in the viewer instead.
  it("plays a video download link in the viewer", async () => {
    const id = "01a13179-08aa-7898-ae86-3a1994d2147e";
    getAttachmentForDownload.mockResolvedValue(attachmentRecord(id, "demo.mp4", "video/mp4"));
    renderContent(`[demo.mp4](/api/attachments/${id}/download)`);

    screen.getByText("demo.mp4").click();

    expect(await screen.findByRole("dialog", { name: "demo.mp4" })).toBeTruthy();
    expect(openSpy).not.toHaveBeenCalled();
    expect(downloadAttachmentSpy).not.toHaveBeenCalled();
  });

  it("plays an inline CDN video link in the viewer instead of opening the raw URL", async () => {
    const id = "01a13179-08aa-7898-ae86-3a1994d2147e";
    const cdnUrl = `https://${CDN_DOMAIN}/workspaces/${WS_ID}/${id}.mp4`;
    getAttachmentForDownload.mockResolvedValue(attachmentRecord(id, "demo.mp4", "video/mp4"));
    // Inline, so it stays a link rather than becoming a file card.
    renderContent(`The recording: [demo.mp4](${cdnUrl}) — have a look.`);

    screen.getByText("demo.mp4").click();

    expect(await screen.findByRole("dialog", { name: "demo.mp4" })).toBeTruthy();
    expect(getAttachmentForDownload).toHaveBeenCalledWith(id);
    expect(openSpy).not.toHaveBeenCalled();
  });

  it("plays a bare CDN video URL in the viewer", async () => {
    const id = "01a13179-08aa-7898-ae86-3a1994d2147e";
    const cdnUrl = `https://${CDN_DOMAIN}/workspaces/${WS_ID}/${id}.mp4`;
    getAttachmentForDownload.mockResolvedValue(attachmentRecord(id, "demo.mp4", "video/mp4"));
    renderContent(`Recording at ${cdnUrl} for review.`);

    screen.getByText(cdnUrl).click();

    expect(await screen.findByRole("dialog", { name: "demo.mp4" })).toBeTruthy();
    expect(openSpy).not.toHaveBeenCalled();
  });

  it("reports a failed lookup instead of falling back to the raw URL", async () => {
    const id = "01a13179-08aa-7898-ae86-3a1994d2147e";
    const cdnUrl = `https://${CDN_DOMAIN}/workspaces/${WS_ID}/${id}.mp4`;
    getAttachmentForDownload.mockRejectedValue(new Error("403"));
    renderContent(`See [demo.mp4](${cdnUrl}) here.`);

    screen.getByText("demo.mp4").click();

    await vi.waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(openSpy).not.toHaveBeenCalled();
    expect(downloadAttachmentSpy).not.toHaveBeenCalled();
  });

  it("keeps a non-attachment path on the CDN host external", () => {
    const other = `https://${CDN_DOMAIN}/marketing/launch.mp4`;
    renderContent(`See [launch](${other}) here.`);

    screen.getByText("launch").click();

    expect(getAttachmentForDownload).not.toHaveBeenCalled();
    expect(openSpy).toHaveBeenCalledWith(other, "_blank", "noopener,noreferrer");
  });

  it("does not intercept a non-UUID /api/attachments link", () => {
    const download = `${APP_ORIGIN}/api/attachments/abc/download`;
    renderContent(`[report.pdf](${download})`);

    screen.getByText("report.pdf").click();

    expect(downloadAttachmentSpy).not.toHaveBeenCalled();
    expect(openSpy).toHaveBeenCalledWith(download, "_blank", "noopener,noreferrer");
  });

  it("keeps another deployment's attachment URL on the external link path", () => {
    const id = "01a13179-08aa-7898-ae86-3a1994d2147e";
    const external = `https://other.example/api/attachments/${id}/download`;
    renderContent(`[report.pdf](${external})`);

    screen.getByText("report.pdf").click();

    expect(downloadAttachmentSpy).not.toHaveBeenCalled();
    expect(openSpy).toHaveBeenCalledWith(external, "_blank", "noopener,noreferrer");
  });
});
