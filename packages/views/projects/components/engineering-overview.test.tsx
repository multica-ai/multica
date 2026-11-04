import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { SupportedLocale } from "@multica/core/i18n";
import { RESOURCES } from "../../locales";
import {
  setApiInstance,
  type BaselineChapter,
  type BaselineRevision,
  type ChapterState,
  type EngineeringConfig,
  type IssueGraphNode,
} from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { Agent } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { EngineeringOverview } from "./engineering-overview";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "11111111-1111-4111-8111-111111111111" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: currentUserId } }),
}));
vi.mock("@multica/core/issues/dag/use-dag-data", () => ({
  useDagGraph: () => ({ data: { nodes: graphNodes }, isLoading: false, isError: false }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: (type: string, id: string) => type === "agent" ? `Agent ${id}` : "Owner" }) }));
vi.mock("../../issues/utils/status-label", () => ({ useStatusLabel: () => (status: string) => status }));
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
let graphNodes: IssueGraphNode[];
let agents: Agent[];
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
    contentMarkdown: `### [Rule-1] ${changeSummary}\n所有写接口必须携带幂等键。`,
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
    createdByType: "agent",
    createdById: "agent-1",
    createdByTaskId: null,
    createdAt: "2026-10-29T00:00:00Z",
    decidedByType: decidedAt ? "member" : null,
    decidedById: decidedAt ? OWNER : null,
    decidedByTaskId: null,
    decidedAt,
    decisionCredential,
  };
}

function node(id: string, stage: number | null, options: {
  title?: string;
  status?: string;
  category?: string;
  parentId?: string | null;
} = {}): IssueGraphNode {
  return {
    id,
    identifier: `OL-${id}`,
    title: options.title ?? `Task ${id}`,
    status: options.status ?? "todo",
    statusCategory: options.category ?? "unstarted",
    revision: 1,
    parentIssueId: options.parentId ?? null,
    hasRestrictedParent: false,
    projectId: PROJECT,
    stage,
    priority: "none",
    assignee: null,
    role: "match",
    runSummary: null,
    dependencySummary: null,
  } as IssueGraphNode;
}

function emptyHistory() {
  return { revisions: [], hasMore: false, nextBefore: null };
}

beforeEach(() => {
  currentUserId = OWNER;
  graphNodes = [];
  agents = [];
  config = {
    projectId: PROJECT,
    workspaceId: WS,
    enabled: true,
    currentStage: 1,
    modeSummary: "Ship the smallest end-to-end path first.",
    deciderMemberIds: [],
    stages: [{ stage: 1, title: "Foundation", goal: "Core contracts" }],
  };
  chapters = [
    { chapter: "overview", adopted: null, candidate: null },
    { chapter: "domain_rules", adopted: null, candidate: null },
    { chapter: "architecture", adopted: null, candidate: null },
    { chapter: "exclusions_and_risks", adopted: null, candidate: null },
  ];
  histories = { overview: [], domain_rules: [], architecture: [], exclusions_and_risks: [] };
  decideProjectBaseline = vi.fn(async (_ws: string, _project: string, action: "adopt" | "reject", revisions: { revisionId: string }[]) => {
    const id = revisions[0]?.revisionId;
    const candidate = chapters.map((chapter) => chapter.candidate).find((item) => item?.id === id);
    if (!candidate) throw new Error("Missing candidate");
    const decided = {
      ...candidate,
      status: (action === "adopt" ? "adopted" : "rejected") as BaselineRevision["status"],
      decidedAt: "2026-10-31T00:00:00Z",
      decisionCredential: "session" as const,
    };
    chapters = chapters.map((chapter) => chapter.candidate?.id === id ? { chapter: chapter.chapter, adopted: action === "adopt" ? decided : chapter.adopted, candidate: null } : chapter);
    histories[candidate.chapter] = [decided, ...histories[candidate.chapter]];
    return { chapters: [], openIssues: { inFlight: [], queued: [], truncated: false } };
  });
  updateProjectEngineering = vi.fn(async (_ws: string, _project: string, data: Partial<EngineeringConfig>) => {
    config = { ...config, ...data };
    return { engineering: config };
  });
  setApiInstance({
    getProject: async () => ({ id: PROJECT, title: "Project", lead_type: null, lead_id: null }),
    listMembers: async () => [
      { id: "member-owner", user_id: OWNER, role: "owner", name: "Owner" },
      { id: "member-regular", user_id: MEMBER, role: "member", name: "Member" },
    ],
    listAgents: async () => agents,
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
  Element.prototype.scrollIntoView = vi.fn();
  window.localStorage.clear();
});

afterEach(() => {
  queryClient.clear();
});

function renderOverview(search = "", currentLocale?: SupportedLocale) {
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
  const ui = (
    <QueryClientProvider client={queryClient}>
      <NavigationProvider value={adapter}>
        <EngineeringOverview workspaceSlug="acme" projectId={PROJECT} />
      </NavigationProvider>
    </QueryClientProvider>
  );
  if (currentLocale) {
    return render(<I18nProvider locale={currentLocale} resources={{ [currentLocale]: RESOURCES[currentLocale] }}>{ui}</I18nProvider>);
  }
  return renderWithI18n(ui);
}

describe("EngineeringOverview roadmap", () => {
  it("renders dynamic stages beyond five and every task without truncation", async () => {
    config.currentStage = 7;
    config.stages = [{ stage: 7, title: "Frontend", goal: "All views" }];
    graphNodes = Array.from({ length: 7 }, (_, index) => node(String(index + 1), 7));
    renderOverview();

    expect((await screen.findAllByText("Frontend")).length).toBeGreaterThan(0);
    expect(document.querySelector('[data-stage-node="7"]')).toHaveAttribute("data-stage-status", "current");
    expect(document.querySelector('[data-stage-node="6"]')).not.toBeInTheDocument();
    for (let index = 1; index <= 7; index += 1) expect(screen.getByText(`Task ${index}`)).toBeInTheDocument();
  });

  it("filters closed-parent history and folds unstaged work outside the mainline", async () => {
    graphNodes = [
      node("parent", null, { status: "done", category: "done" }),
      node("historical", 1, { parentId: "parent", title: "Historical child" }),
      node("outside", null, { title: "Unstaged task" }),
      ...Array.from({ length: 6 }, (_, index) => node(`open-${index}`, 1)),
    ];
    renderOverview();

    expect(await screen.findByText("Task open-0")).toBeInTheDocument();
    expect(screen.queryByText("Historical child")).not.toBeInTheDocument();
    expect(screen.queryByText("Unstaged task")).not.toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: /Outside the mainline/ }));
    expect(screen.getByText("Unstaged task")).toBeInTheDocument();
    expect(screen.getAllByText(/Task open-/).length).toBeGreaterThanOrEqual(6);
  });

  it("saves stage name and goal through the engineering mutation", async () => {
    const user = userEvent.setup();
    renderOverview();
    await user.click(await screen.findByRole("button", { name: "Edit name and goal" }));
    const name = screen.getByRole("textbox", { name: "Stage name" });
    await user.clear(name);
    await user.type(name, "Discovery");
    const goal = screen.getByRole("textbox", { name: "Stage goal" });
    await user.clear(goal);
    await user.type(goal, "Confirm scope");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(updateProjectEngineering).toHaveBeenCalledWith(WS, PROJECT, {
      stages: [{ stage: 1, title: "Discovery", goal: "Confirm scope" }],
    });
  });

  it("reorders stage metadata through the same whole-table mutation", async () => {
    const user = userEvent.setup();
    config.stages = [
      { stage: 1, title: "Foundation", goal: "Contracts" },
      { stage: 2, title: "Delivery", goal: "Views" },
    ];
    renderOverview();
    await user.click(await screen.findByRole("button", { name: "Move stage later" }));
    expect(updateProjectEngineering).toHaveBeenCalledWith(WS, PROJECT, {
      stages: [
        { stage: 1, title: "Delivery", goal: "Views" },
        { stage: 2, title: "Foundation", goal: "Contracts" },
      ],
    });
  });

  it("adds a stage beyond the current maximum", async () => {
    const user = userEvent.setup();
    config.currentStage = 6;
    config.stages = [{ stage: 6, title: "Six", goal: "" }];
    renderOverview();
    await user.click(await screen.findByRole("button", { name: "Add stage" }));
    await user.type(screen.getByRole("textbox", { name: "Stage name" }), "Seven");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(updateProjectEngineering).toHaveBeenCalledWith(WS, PROJECT, {
      stages: [{ stage: 6, title: "Six", goal: "" }, { stage: 7, title: "Seven", goal: "" }],
    });
  });

  it("offers stage review when all current tasks are closed", async () => {
    const user = userEvent.setup();
    graphNodes = [node("done", 1, { status: "done", category: "done" })];
    renderOverview();
    await user.click((await screen.findAllByRole("button", { name: /Review Stage 1 and unlock the next stage/ }))[0]!);
    await user.click(screen.getByRole("button", { name: "Unlock Stage 2" }));
    expect(updateProjectEngineering).toHaveBeenCalledWith(WS, PROJECT, { currentStage: 2 });
  });
});

describe("EngineeringOverview skills and permissions", () => {
  it("shows active agent bindings", async () => {
    agents = [{ id: "agent-1", name: "Frontend", archived_at: null, skills: [{ id: "skill-1", name: "strixnova-deck", description: "", enabled: true }] } as Agent];
    renderOverview();
    expect(await screen.findByText("Strixnova engineering skill bindings")).toBeInTheDocument();
    expect(screen.getByText("Frontend")).toBeInTheDocument();
    expect(screen.getByText("strixnova-deck")).toBeInTheDocument();
  });

  it("keeps engineering enabled and editable without any skill bindings", async () => {
    renderOverview();
    expect(await screen.findByText(/No active agent is bound to Strixnova engineering skills/)).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Enable engineering overview" })).toBeChecked();
    expect(screen.getByRole("button", { name: "Add stage" })).toBeEnabled();
  });

  it("lets an admin enable the overview", async () => {
    const user = userEvent.setup();
    config.enabled = false;
    renderOverview();
    await user.click(await screen.findByRole("button", { name: "Enable overview" }));
    expect(updateProjectEngineering).toHaveBeenCalledWith(WS, PROJECT, { enabled: true });
  });

  it("does not show enable, stage editing, review, or settings controls to a regular member", async () => {
    currentUserId = MEMBER;
    graphNodes = [node("done", 1, { status: "done", category: "done" })];
    renderOverview();
    expect(await screen.findByText("Delivery stages")).toBeInTheDocument();
    expect(screen.queryByText("Engineering settings")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add stage" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit name and goal" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Review Stage 1/ })).not.toBeInTheDocument();
  });
});

describe("EngineeringOverview baseline navigation", () => {
  beforeEach(() => {
    chapters = chapters.map((chapter) => chapter.chapter === "domain_rules"
      ? { ...chapter, candidate: revision(RULES_CANDIDATE, "domain_rules", 2, "RULES-CANDIDATE") }
      : chapter.chapter === "architecture"
        ? { ...chapter, candidate: revision(ARCHITECTURE_CANDIDATE, "architecture", 1, "ARCH-CANDIDATE") }
        : chapter);
  });

  it("opens the candidate from the waiting list in compare and management mode", async () => {
    const user = userEvent.setup();
    renderOverview();
    await user.click(await screen.findByText("Baseline revision v2 needs confirmation"));
    expect(await screen.findByText("RULES-CANDIDATE", { selector: "p" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Project baseline" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Project management" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Compare v2" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText(/Agent agent-1/)).toBeInTheDocument();
  });

  it("opens a candidate deep link without adopting on navigation", async () => {
    renderOverview(`candidate=${RULES_CANDIDATE}`);
    expect(await screen.findByText("RULES-CANDIDATE", { selector: "p" })).toBeInTheDocument();
    expect(decideProjectBaseline).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Adopt" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Return for changes" })).toBeEnabled();
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

  it("selects the first pending chapter when opening the baseline tab directly", async () => {
    const user = userEvent.setup();
    renderOverview();
    await user.click(await screen.findByRole("tab", { name: "Project baseline" }));
    expect(await screen.findByText("RULES-CANDIDATE", { selector: "p" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Current v0" })).toHaveAttribute("aria-selected", "true");
    await user.click(screen.getByRole("tab", { name: "Compare v2" }));
    expect(document.querySelector('[data-diff-kind="added"]')).toBeInTheDocument();
  });

  it("refreshes the deep-linked revision status after adoption", async () => {
    const user = userEvent.setup();
    renderOverview(`candidate=${RULES_CANDIDATE}`);
    await user.click(await screen.findByRole("button", { name: "Adopt" }));
    await waitFor(() => expect(decideProjectBaseline).toHaveBeenCalled());
    await waitFor(() => expect(document.querySelector("[data-baseline-revision-status]")).toHaveAttribute("data-baseline-revision-status", "adopted"));
    expect(screen.queryByRole("button", { name: "Adopt" })).not.toBeInTheDocument();
  });
});

describe("EngineeringOverview localization", () => {
  it("renders the dedicated zh-Hant bundle without raw keys", async () => {
    renderOverview("", "zh-Hant");
    expect(await screen.findByText("交付階段")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "總覽" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "專案基線" })).toBeInTheDocument();
    for (const key of ["stage.flowLabel", "views.overview", "views.baseline"]) expect(screen.queryByText(key)).not.toBeInTheDocument();
  });
});
