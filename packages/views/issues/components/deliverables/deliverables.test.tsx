import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Attachment, GitHubPullRequest, TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";

const { openAtMock, tryOpenMock, downloadMock, githubSettings, pullRequests } = vi.hoisted(() => ({
  openAtMock: vi.fn((_key: string) => true),
  tryOpenMock: vi.fn(() => false),
  downloadMock: vi.fn(),
  githubSettings: { prSidebar: true },
  pullRequests: { current: [] as GitHubPullRequest[] },
}));

vi.mock("@multica/core/github", () => ({
  useGitHubSettings: () => githubSettings,
  issuePullRequestsOptions: (issueId: string) => ({
    queryKey: ["github", "pull-requests", issueId],
    queryFn: async () => ({ pull_requests: pullRequests.current }),
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) => (type === "agent" ? `Agent ${id}` : `Member ${id}`),
  }),
}));

vi.mock("../../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

vi.mock("../../../editor", () => ({
  usePreviewSequence: () => ({ openAt: openAtMock }),
  useAttachmentPreview: () => ({ open: vi.fn(), tryOpen: tryOpenMock, modal: null }),
  useDownloadAttachment: () => downloadMock,
}));

vi.mock("../../../editor/hooks/use-inline-media-url", () => ({
  useResignedInlineMedia: (_id: string | undefined, url: string) => ({ url, pending: false }),
}));

vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string) => url,
}));

vi.mock("../../../platform", () => ({ useImmersiveMode: () => {} }));

vi.mock("../pull-requests-section", () => ({
  PullRequestsGroup: ({ identifier }: { identifier: string }) => (
    <div data-testid="pull-requests-group">{identifier}</div>
  ),
}));

vi.mock("../pull-request-list", () => ({ PullRequestStateIcon: () => null }));

import { DeliverablesSection } from "./deliverables-section";
import { DeliverablesOverview } from "./deliverables-overview";
import { useIssueDeliverables, type IssueDeliverables } from "./use-issue-deliverables";

function attachment(over: Partial<Attachment> & { id: string }): Attachment {
  return {
    workspace_id: "ws-1",
    issue_id: "issue-1",
    comment_id: "c-1",
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "agent",
    uploader_id: "lambda",
    filename: "report.md",
    url: `https://cdn.example.test/${over.id}`,
    download_url: `https://cdn.example.test/${over.id}?sig=1`,
    markdown_url: `https://cdn.example.test/${over.id}`,
    content_type: "text/markdown",
    size_bytes: 6 * 1024,
    created_at: "2026-09-20T10:00:00Z",
    ...over,
  };
}

function comment(
  id: string,
  attachments: Attachment[],
  over: Partial<TimelineEntry> = {},
): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "agent",
    actor_id: "lambda",
    actor_name: "Lambda",
    created_at: attachments[0]?.created_at ?? "2026-09-20T10:00:00Z",
    content: "Done — see the files.",
    attachments: attachments.map((a) => ({ ...a, comment_id: id })),
    ...over,
  };
}

function pr(over: Partial<GitHubPullRequest> = {}): GitHubPullRequest {
  return {
    id: "pr-1",
    provider: "github",
    workspace_id: "ws-1",
    repo_owner: "acme",
    repo_name: "widget",
    number: 8712,
    title: "Notification matrix",
    state: "open",
    html_url: "https://example.test/pr/8712",
    branch: "feat/x",
    author_login: "octocat",
    author_avatar_url: null,
    merged_at: null,
    closed_at: null,
    pr_created_at: "2026-09-20T00:00:00Z",
    pr_updated_at: "2026-09-20T00:00:00Z",
    mergeable: null,
    merge_state_status: null,
    snapshot_available: true,
    checks_rollup: null,
    checks_total: 0,
    checks_passed: 0,
    checks_failed: 0,
    checks_running: 0,
    failed_check_names: [],
    snapshot_stale: false,
    snapshot_fetched_at: null,
    additions: 164,
    deletions: 58,
    changed_files: 7,
    ...over,
  };
}

// A run posted a screenshot and a report, a later run re-uploaded the
// report: three uploads, two deliverables.
const SHOT = attachment({
  id: "shot",
  filename: "settings.png",
  content_type: "image/png",
  size_bytes: 412 * 1024,
  created_at: "2026-09-20T10:00:00Z",
});
const REPORT_V1 = attachment({ id: "report-1", created_at: "2026-09-20T10:00:01Z" });
const REPORT_V2 = attachment({ id: "report-2", created_at: "2026-09-21T09:00:00Z" });
const CSV = attachment({
  id: "csv",
  filename: "latency.csv",
  content_type: "text/csv",
  size_bytes: 18 * 1024,
  created_at: "2026-09-21T09:00:01Z",
});
const TIMELINE: TimelineEntry[] = [
  comment("c-1", [SHOT, REPORT_V1]),
  comment("c-2", [REPORT_V2, CSV], { created_at: "2026-09-21T09:00:00Z" }),
];

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

async function deliverablesFor(timeline: TimelineEntry[]): Promise<IssueDeliverables> {
  const { result } = renderHook(() => useIssueDeliverables("issue-1", timeline), { wrapper });
  if (githubSettings.prSidebar && pullRequests.current.length > 0) {
    await waitFor(() => expect(result.current.pullRequests).toHaveLength(pullRequests.current.length));
  }
  return result.current;
}

function renderSection(deliverables: IssueDeliverables, onOpenOverview = vi.fn()) {
  return renderWithI18n(
    wrapper({
      children: (
        <DeliverablesSection
          issueId="issue-1"
          identifier="MUL-7588"
          deliverables={deliverables}
          onOpenOverview={onOpenOverview}
        />
      ),
    }),
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  githubSettings.prSidebar = true;
  pullRequests.current = [];
});

describe("useIssueDeliverables", () => {
  it("counts pull requests and files, a re-upload as one file", async () => {
    pullRequests.current = [pr()];
    const deliverables = await deliverablesFor(TIMELINE);
    expect(deliverables.files.map((f) => f.latest.id)).toEqual(["csv", "report-2", "shot"]);
    expect(deliverables.count).toBe(4);
  });

  it("drops the code group when the workspace hides the PR sidebar", async () => {
    githubSettings.prSidebar = false;
    pullRequests.current = [pr()];
    const deliverables = await deliverablesFor(TIMELINE);
    expect(deliverables.pullRequests).toEqual([]);
    expect(deliverables.count).toBe(3);
  });
});

describe("DeliverablesSection", () => {
  it("renders nothing while nothing has been delivered and PRs are hidden", async () => {
    githubSettings.prSidebar = false;
    const { container } = renderSection(await deliverablesFor([]));
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps the code group up with nothing delivered, so a PR can be linked", async () => {
    renderSection(await deliverablesFor([]));
    const header = screen.getByRole("button", { name: /Deliverables/ });
    expect(header).not.toHaveTextContent(/\d/);
    expect(screen.getByTestId("pull-requests-group")).toHaveTextContent("MUL-7588");
    expect(screen.queryByRole("button", { name: /View all/ })).toBeNull();
  });

  it("shows the code group, the latest version of each file and the total", async () => {
    pullRequests.current = [pr()];
    renderSection(await deliverablesFor(TIMELINE));

    expect(screen.getByRole("button", { name: /Deliverables/ })).toHaveTextContent("4");
    expect(screen.getByTestId("pull-requests-group")).toBeInTheDocument();
    // One row for the report, marked as its second version.
    const report = screen.getByRole("button", { name: "report.md, version 2" });
    expect(report).toHaveTextContent("v2");
    expect(screen.getAllByTitle("report.md")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "settings.png" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "View all 4 deliverables" })).toBeInTheDocument();
  });

  it("opens the latest version in the page's viewer", async () => {
    renderSection(await deliverablesFor(TIMELINE));
    fireEvent.click(screen.getByRole("button", { name: "report.md, version 2" }));
    expect(openAtMock).toHaveBeenCalledWith("report-2");
  });

  it("downloads a file neither the sequence nor the viewer can open", async () => {
    openAtMock.mockReturnValueOnce(false);
    renderSection(
      await deliverablesFor([
        comment("c-9", [
          attachment({ id: "zip", filename: "bundle.zip", content_type: "application/zip" }),
        ]),
      ]),
    );
    fireEvent.click(screen.getByRole("button", { name: "bundle.zip" }));
    expect(tryOpenMock).toHaveBeenCalled();
    expect(downloadMock).toHaveBeenCalledWith("zip");
  });

  it("opens the overview from view all", async () => {
    const onOpenOverview = vi.fn();
    renderSection(await deliverablesFor(TIMELINE), onOpenOverview);
    fireEvent.click(screen.getByRole("button", { name: "View all 3 deliverables" }));
    expect(onOpenOverview).toHaveBeenCalledTimes(1);
  });
});

describe("DeliverablesOverview", () => {
  const commentById = new Map(TIMELINE.map((entry) => [entry.id, entry]));

  function renderOverview(
    deliverables: IssueDeliverables,
    props: Partial<Parameters<typeof DeliverablesOverview>[0]> = {},
  ) {
    const onClose = vi.fn();
    const onLocate = vi.fn();
    renderWithI18n(
      wrapper({
        children: (
          <DeliverablesOverview
            open
            onClose={onClose}
            identifier="MUL-7588"
            deliverables={deliverables}
            commentById={commentById}
            onLocate={onLocate}
            returnKey={null}
            {...props}
          />
        ),
      }),
    );
    return { onClose, onLocate };
  }

  it("counts exactly what the sidebar counts", async () => {
    pullRequests.current = [pr()];
    const deliverables = await deliverablesFor(TIMELINE);
    renderOverview(deliverables);
    const dialog = screen.getByRole("dialog", { name: "MUL-7588" });
    expect(within(dialog).getByText(/^4 deliverables/)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: /^All\s*4$/ })).toBeInTheDocument();
    // Code, then one group per posting comment; the report sits with its v2.
    expect(within(dialog).getByRole("region", { name: "Code" })).toBeInTheDocument();
    const groups = within(dialog).getAllByRole("region", { name: "Comment by Lambda" });
    expect(groups).toHaveLength(2);
    expect(within(groups[0]!).getByTitle("settings.png")).toBeInTheDocument();
    expect(within(groups[1]!).getByTitle("report.md")).toHaveTextContent("v2");
  });

  it("filters by kind", async () => {
    pullRequests.current = [pr()];
    renderOverview(await deliverablesFor(TIMELINE));
    fireEvent.click(screen.getByRole("button", { name: /^Images\s*1$/ }));
    expect(screen.queryByRole("region", { name: "Code" })).toBeNull();
    expect(screen.getByTitle("settings.png")).toBeInTheDocument();
    expect(screen.queryByTitle("report.md")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /^Videos\s*0$/ }));
    expect(screen.getByText("No deliverables of this type.")).toBeInTheDocument();
  });

  it("opens a file and locates a comment, closing itself first", async () => {
    const { onClose, onLocate } = renderOverview(await deliverablesFor(TIMELINE));
    fireEvent.click(screen.getByTitle("latency.csv"));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(openAtMock).toHaveBeenCalledWith("csv");

    const [, secondGroup] = screen.getAllByRole("region", { name: "Comment by Lambda" });
    fireEvent.click(within(secondGroup!).getByRole("button", { name: "Show in comments" }));
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(onLocate).toHaveBeenCalledWith({ kind: "comment", commentId: "c-2" });
  });

  it("goes back to the viewer's file with G, and closes with Escape", async () => {
    const { onClose } = renderOverview(await deliverablesFor(TIMELINE), { returnKey: "shot" });
    act(() => {
      fireEvent.keyDown(document, { key: "g" });
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(openAtMock).toHaveBeenCalledWith("shot");

    act(() => {
      fireEvent.keyDown(document, { key: "Escape" });
    });
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
