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
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

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
  useAppOrigin: () => APP_ORIGIN,
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

vi.mock("../editor/link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

vi.mock("mermaid", () => ({
  default: { initialize: vi.fn(), render: vi.fn() },
}));

import { RichContent } from "./rich-content";

let navigatedPaths: string[] = [];
let openSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  navigatedPaths = [];
  window.addEventListener("multica:navigate", captureNavigate);
  openSpy = vi.spyOn(window, "open").mockImplementation(() => null);
});

afterEach(() => {
  window.removeEventListener("multica:navigate", captureNavigate);
  vi.restoreAllMocks();
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
});
