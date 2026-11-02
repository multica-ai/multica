import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  setApiInstance,
  type BaselineChapter,
  type BaselineRevision,
  type ChapterState,
  type EngineeringConfig,
} from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { EngineeringOverview } from "./engineering-overview";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "11111111-1111-4111-8111-111111111111" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: currentUserId } }),
}));
vi.mock("@multica/core/issues/dag/use-dag-data", () => ({
  useDagGraph: () => ({ data: { nodes: [] }, isLoading: false, isError: false }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Owner" }) }));
vi.mock("../../issues/utils/status-label", () => ({ useStatusLabel: () => (status: string) => status }));
vi.mock("../../rich-content", () => ({ RichContent: ({ content }: { content: string }) => <div>{content}</div> }));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

const WS = "11111111-1111-4111-8111-111111111111";
const OWNER = "22222222-2222-4222-8222-222222222222";
const MEMBER = "66666666-6666-4666-8666-666666666666";
const PROJECT = "33333333-3333-4333-8333-333333333333";
const RULES_CANDIDATE = "44444444-4444-4444-8444-444444444444";
const ARCHITECTURE_CANDIDATE = "55555555-5555-4555-8555-555555555555";
const HISTORICAL = "77777777-7777-4777-8777-777777777777";
const MISSING = "88888888-8888-4888-8888-888888888888";

let currentUserId = OWNER;
let config: EngineeringConfig;
let chapters: ChapterState[];
let histories: Record<BaselineChapter, BaselineRevision[]>;
let decideProjectBaseline: ReturnType<typeof vi.fn>;
let updateProjectEngineering: ReturnType<typeof vi.fn>;
let queryClient: QueryClient;

function revision(
  id: string,
  chapter: BaselineChapter,
  revisionNumber: number,
  changeSummary: string,
  status: BaselineRevision["status"] = "candidate",
  decidedAt: string | null = null,
  decisionCredential: BaselineRevision["decisionCredential"] = null,
): BaselineRevision {
  return {
    id,
    workspaceId: WS,
    projectId: PROJECT,
    chapter,
    revisionNumber,
    status,
    contentMarkdown: `### [Rule-1] ${changeSummary}`,
    changeSummary,
    baseRevisionId: null,
    replacesRevisionId: null,
    sourceIssueId: null,
    anchorsDiff: {
      chapter,
      baseRevisionNumber: Math.max(0, revisionNumber - 1),
      added: [{ anchor: "Rule-1", title: changeSummary, before: "", after: changeSummary, beforeChars: 0, afterChars: changeSummary.length }],
      modified: [],
      removed: [],
      unchangedCount: 0,
      warnings: [],
    },
    createdByType: "member",
    createdById: OWNER,
    createdByTaskId: null,
    createdAt: "2026-10-29T00:00:00Z",
    decidedByType: decidedAt ? "member" : null,
    decidedById: decidedAt ? OWNER : null,
    decidedByTaskId: null,
    decidedAt,
    decisionCredential,
  };
}

function emptyHistory() {
  return { revisions: [], hasMore: false, nextBefore: null };
}

beforeEach(() => {
  currentUserId = OWNER;
  config = {
    projectId: PROJECT,
    workspaceId: WS,
    enabled: true,
    currentStage: 1,
    modeSummary: "",
    deciderMemberIds: [],
  };
  chapters = [
    { chapter: "overview", adopted: null, candidate: null },
    { chapter: "domain_rules", adopted: null, candidate: revision(RULES_CANDIDATE, "domain_rules", 2, "RULES-CANDIDATE") },
    { chapter: "architecture", adopted: null, candidate: revision(ARCHITECTURE_CANDIDATE, "architecture", 1, "ARCH-CANDIDATE") },
    { chapter: "exclusions_and_risks", adopted: null, candidate: null },
  ];
  histories = { overview: [], domain_rules: [], architecture: [], exclusions_and_risks: [] };
  decideProjectBaseline = vi.fn(async () => {
    const candidate = chapters.find((chapter) => chapter.chapter === "domain_rules")?.candidate;
    if (!candidate) throw new Error("Missing candidate");
    const adopted = { ...candidate, status: "adopted" as const, decidedAt: "2026-10-31T00:00:00Z", decisionCredential: "session" as const };
    chapters = chapters.map((chapter) => chapter.chapter === "domain_rules" ? { chapter: chapter.chapter, adopted, candidate: null } : chapter);
    histories.domain_rules = [adopted, ...histories.domain_rules];
    return { chapters: [], openIssues: { inFlight: [], queued: [], truncated: false } };
  });
  updateProjectEngineering = vi.fn(async () => {
    config = { ...config, enabled: true };
    return { engineering: config };
  });
  setApiInstance({
    getProject: async () => ({ id: PROJECT, title: "Project", lead_type: null, lead_id: null }),
    listMembers: async () => [
      { id: "member-owner", user_id: OWNER, role: "owner", name: "Owner" },
      { id: "member-regular", user_id: MEMBER, role: "member", name: "Member" },
    ],
    getProjectEngineering: async () => ({ engineering: config, chapters }),
    getProjectBaselineHistory: async (_ws: string, _project: string, chapter: BaselineChapter, options: { limit?: number } = {}) => ({
      ...emptyHistory(),
      revisions: histories[chapter].slice(0, options.limit ?? 50),
    }),
    listIssues: async () => ({ issues: [], total: 0 }),
    listTimeline: async () => [],
    decideProjectBaseline,
    updateProjectEngineering,
  } as unknown as ApiClient);
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
});

afterEach(() => {
  queryClient.clear();
});

function renderOverview(search = "") {
  const adapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: `/acme/projects/${PROJECT}/engineering`,
    searchParams: new URLSearchParams(search),
    hash: "",
    openInNewTab: vi.fn(),
    getShareableUrl: (path: string) => path,
  } as unknown as NavigationAdapter;
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <NavigationProvider value={adapter}>
        <EngineeringOverview workspaceSlug="acme" projectId={PROJECT} />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

describe("EngineeringOverview deep links", () => {
  it("shows all warnings without blocking a decision or adopting on navigation", async () => {
    const user = userEvent.setup();
    const candidate = chapters.find((chapter) => chapter.chapter === "domain_rules")!.candidate!;
    candidate.anchorsDiff.warnings = [
      { code: "rule_removed", anchor: "Rule-1" },
      { code: "rule_shrunk", anchor: "Rule-2" },
      { code: "rule_deferred", anchor: "Rule-3" },
      { code: "defer_missing_pool", anchor: "Defer-1" },
    ];
    renderOverview(`candidate=${RULES_CANDIDATE}`);
    expect(await screen.findByText("Rule-1 removes a mandatory rule")).toBeInTheDocument();
    expect(screen.getByText("Rule-2 substantially weakens a mandatory rule")).toBeInTheDocument();
    expect(screen.getByText("Rule-3 defers a mandatory rule")).toBeInTheDocument();
    expect(screen.getByText("Defer-1 is deferred without a non-blocking pool task")).toBeInTheDocument();
    expect(decideProjectBaseline).not.toHaveBeenCalled();
    const adopt = screen.getByRole("button", { name: "Adopt" });
    expect(adopt).toBeEnabled();
    expect(screen.getByRole("button", { name: "Reject" })).toBeEnabled();
    await user.click(adopt);
    await waitFor(() => expect(decideProjectBaseline).toHaveBeenCalledTimes(1));
  });
  it("does not substitute the current candidate when the requested revision is missing", async () => {
    renderOverview(`candidate=${MISSING}`);

    expect(await screen.findByText("The requested revision was not found")).toBeInTheDocument();
    expect(screen.queryByText("RULES-CANDIDATE", { selector: "p" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Adopt" })).not.toBeInTheDocument();
  });

  it("keeps a historical deep-linked revision read-only", async () => {
    histories.domain_rules = [revision(HISTORICAL, "domain_rules", 1, "OLD-RULES", "withdrawn", "2026-10-29T01:00:00Z", "session")];
    renderOverview(`candidate=${HISTORICAL}`);

    expect(await screen.findByText("OLD-RULES", { selector: "p" })).toBeInTheDocument();
    expect(screen.getByText(/historical revision is read-only/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Adopt" })).not.toBeInTheDocument();
  });

  it("shows the selected chapter's own candidate after leaving the deep-linked chapter", async () => {
    const user = userEvent.setup();
    renderOverview(`candidate=${RULES_CANDIDATE}`);
    expect(await screen.findByText("RULES-CANDIDATE", { selector: "p" })).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "Architecture" }));

    expect(screen.getByRole("tab", { name: "Architecture" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("ARCH-CANDIDATE", { selector: "p" })).toBeInTheDocument();
    expect(screen.queryByText("RULES-CANDIDATE", { selector: "p" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Adopt" })).toBeInTheDocument();
  });

  it("refreshes the deep-linked revision status after adoption", async () => {
    const user = userEvent.setup();
    renderOverview(`candidate=${RULES_CANDIDATE}`);
    expect(await screen.findByText("RULES-CANDIDATE", { selector: "p" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Adopt" }));

    await waitFor(() => expect(decideProjectBaseline).toHaveBeenCalled());
    await waitFor(() => expect(document.querySelector("[data-baseline-revision-status]")).toHaveAttribute("data-baseline-revision-status", "adopted"));
    expect(screen.queryByRole("button", { name: "Adopt" })).not.toBeInTheDocument();
  });
});

describe("EngineeringOverview permissions", () => {
  it("offers only the project capability switch", async () => {
    renderOverview();
    const toggle = await screen.findByRole("switch", { name: "Enable engineering overview" });
    expect(toggle).toBeChecked();
    expect(screen.getAllByRole("switch")).toHaveLength(1);
    expect(screen.getByRole("group", { name: "Decision makers" })).toBeInTheDocument();
  });
  it("lets an admin enable the overview", async () => {
    const user = userEvent.setup();
    config.enabled = false;
    renderOverview();

    await user.click(await screen.findByRole("button", { name: "Enable overview" }));

    expect(updateProjectEngineering).toHaveBeenCalledWith(WS, PROJECT, { enabled: true });
  });

  it("does not show enable or settings controls to a regular member", async () => {
    currentUserId = MEMBER;
    config.enabled = false;
    const view = renderOverview();

    expect(await screen.findByText("Engineering overview is not enabled")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Enable overview" })).not.toBeInTheDocument();

    config.enabled = true;
    view.unmount();
    renderOverview();
    expect(await screen.findByText("Managed baseline and revision approval")).toBeInTheDocument();
    expect(screen.queryByText("Engineering settings")).not.toBeInTheDocument();
  });
});

describe("EngineeringOverview recent decisions", () => {
  it("labels agent rejections as decisions without confirmation wording", async () => {
    histories.domain_rules = [revision(HISTORICAL, "domain_rules", 1, "Agent decision", "rejected", "2026-10-30T01:00:00Z", "agent_task")];
    renderOverview();
    expect(await screen.findByText("Decided by an agent")).toBeInTheDocument();
    expect(screen.getByText("Rejected")).toBeInTheDocument();
    expect(screen.queryByText(/confirmed by an agent/i)).not.toBeInTheDocument();
  });
  it("shows the latest decided revisions with their outcome, credential, and time", async () => {
    histories.domain_rules = [revision("99999999-9999-4999-8999-999999999991", "domain_rules", 2, "Earlier decision", "adopted", "2026-10-29T01:00:00Z", "session")];
    histories.architecture = [revision("99999999-9999-4999-8999-999999999992", "architecture", 4, "Latest decision summary", "rejected", "2026-10-30T01:00:00Z", "pat")];
    renderOverview();

    const region = await waitFor(() => {
      const element = document.querySelector('[data-engineering-region="recent-changes"]');
      expect(element).not.toBeNull();
      return element as HTMLElement;
    });
    expect(within(region).getByText("What changed since your last visit")).toBeInTheDocument();
    expect(within(region).getByText("Latest decision summary")).toBeInTheDocument();
    expect(within(region).getByText("Revision 4")).toBeInTheDocument();
    expect(within(region).getByText("Rejected")).toBeInTheDocument();
    expect(within(region).getByText("Confirmed with a personal token")).toBeInTheDocument();
    expect(region.querySelector('time[datetime="2026-10-30T01:00:00Z"]')).not.toBeNull();
    expect(screen.getByText("Latest decision")).toBeInTheDocument();
  });
});
