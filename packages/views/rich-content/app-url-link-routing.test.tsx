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
// attachment endpoint cannot authenticate a bare navigation (WK-11), so the
// renderer must hand the click to this hook instead of `window.open`. We mock
// the module so the test asserts the dispatch, not the download plumbing.
const downloadAttachmentSpy = vi.fn();
vi.mock("../editor/use-download-attachment", () => ({
  useDownloadAttachment: () => downloadAttachmentSpy,
}));

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
  });
  window.addEventListener("multica:navigate", captureNavigate);
  openSpy = vi.spyOn(window, "open").mockImplementation(() => null);
});

afterEach(() => {
  window.removeEventListener("multica:navigate", captureNavigate);
  vi.restoreAllMocks();
  downloadAttachmentSpy.mockClear();
  act(() => {
    configStore.getState().setAttachmentPublicUrl("");
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
  // pinned) cannot authenticate a bare navigation, so it must be routed through
  // the credentialed download hook rather than window.open.
  it("routes a UUID attachment download link through the download hook (WK-11)", () => {
    const id = "01a13179-08aa-7898-ae86-3a1994d2147e";
    const download = `https://multica-api.erlinerd.com/api/attachments/${id}/download`;
    renderContent(`[audit-report.md](${download})`);

    screen.getByText("audit-report.md").click();

    // Not opened as an unauthenticated external navigation, and not treated as
    // an in-app path.
    expect(openSpy).not.toHaveBeenCalled();
    expect(navigatedPaths).toEqual([]);
    expect(downloadAttachmentSpy).toHaveBeenCalledWith(id);
  });

  it("recognizes the API-host link when runtime config arrives after rendering", () => {
    const id = "01a13179-08aa-7898-ae86-3a1994d2147e";
    const download = `https://multica-api.erlinerd.com/api/attachments/${id}/download`;
    act(() => configStore.getState().setAttachmentPublicUrl(""));
    renderContent(`[report.pdf](${download})`);

    act(() => configStore.getState().setAttachmentPublicUrl("https://multica-api.erlinerd.com"));
    screen.getByText("report.pdf").click();

    expect(downloadAttachmentSpy).toHaveBeenCalledWith(id);
    expect(openSpy).not.toHaveBeenCalled();
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
