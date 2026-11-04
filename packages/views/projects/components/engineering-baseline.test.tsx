import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import type { BaselineDecision, BaselineRevision, ChapterState, IssueGraphNode } from "@multica/core/api";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { EngineeringBaseline } from "./engineering-baseline";
import { buildEngineeringRoadmap } from "./engineering-utils";

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (type: string, id: string) => type === "agent" ? `Agent ${id}` : `Actor ${id}` }),
}));
vi.mock("../../issues/utils/status-label", () => ({
  useStatusLabel: () => (status: string) => status,
}));

const navigation = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/projects/project-1/engineering",
  searchParams: new URLSearchParams(),
  hash: "",
  openInNewTab: vi.fn(),
  getShareableUrl: (path: string) => path,
} as unknown as NavigationAdapter;

const revision = (status: BaselineRevision["status"] = "candidate"): BaselineRevision => ({
  id: "rev-1",
  workspaceId: "ws-1",
  projectId: "project-1",
  chapter: "domain_rules",
  revisionNumber: 3,
  status,
  contentMarkdown: [
    "### [Rule-1] 幂等键",
    "所有写接口必须携带幂等键，由 OL-42 在 Stage 6 实现。",
    "",
    "为什么：网络重试不能创建重复订单。",
    "",
    "例：同一请求重发两次只创建一个订单。",
    "",
    "### 预占快照",
    "下单时记录库存快照。",
  ].join("\n"),
  changeSummary: "Review wording",
  baseRevisionId: "rev-base",
  replacesRevisionId: null,
  sourceIssueId: null,
  anchorsDiff: {
    chapter: "domain_rules",
    baseRevisionNumber: 2,
    added: [{ anchor: "预占快照", title: "预占快照", before: "", after: "下单时记录库存快照。", beforeChars: 0, afterChars: 10 }],
    modified: [{ anchor: "Rule-1", title: "幂等键", before: "所有写接口应该携带幂等键。", after: "所有写接口必须携带幂等键。", beforeChars: 15, afterChars: 15 }],
    removed: [{ anchor: "Rule-old", title: "旧规则", before: "删除这段旧规则。", after: "", beforeChars: 8, afterChars: 0 }],
    unchangedCount: 0,
    warnings: [{ code: "rule_shrunk", anchor: "Rule-1" }],
  },
  createdByType: "agent",
  createdById: "agent-1",
  createdByTaskId: null,
  createdAt: "2026-01-01T00:00:00Z",
  decidedByType: status === "candidate" ? null : "member",
  decidedById: status === "candidate" ? null : "user-1",
  decidedByTaskId: null,
  decidedAt: status === "candidate" ? null : "2026-01-02T00:00:00Z",
  decisionCredential: status === "candidate" ? null : "pat",
});

const adopted = (): BaselineRevision => ({
  ...revision("adopted"),
  id: "rev-base",
  revisionNumber: 2,
  contentMarkdown: "### [Rule-1] 幂等键\n所有写接口应该携带幂等键。\n\n### [Rule-old] 旧规则\n删除这段旧规则。",
  anchorsDiff: { ...revision().anchorsDiff, added: [], modified: [], removed: [] },
});
const chapter = (candidate: BaselineRevision | null, current: BaselineRevision | null = null): ChapterState => ({ chapter: "domain_rules", adopted: current, candidate });
const nodes = [{ id: "issue-42", identifier: "OL-42", title: "Implement idempotency", stage: 6, status: "in_progress", statusCategory: "started" }] as IssueGraphNode[];
const roadmap = buildEngineeringRoadmap(nodes, [{ stage: 6, title: "Frontend", goal: "Ship the views" }], 6);
const baseProps = {
  workspaceSlug: "acme",
  workspaceId: "ws-1",
  projectId: "project-1",
  projectTitle: "Project",
  selectedChapter: "domain_rules" as const,
  onChapterChange: vi.fn(),
  candidateLoading: false,
  candidateNotFound: false,
  canDecide: true,
  decisionPending: false,
  decisionError: false,
  decisionResult: null,
  history: null,
  historyLoading: false,
  historyError: false,
  onHistoryMore: vi.fn(),
  chapterRevisions: {},
  roadmap,
  reviewRequested: false,
  onOpenStage: vi.fn(),
};

function renderBaseline(ui: ReactElement) {
  return renderWithI18n(<NavigationProvider value={navigation}>{ui}</NavigationProvider>);
}

beforeEach(() => {
  window.localStorage.clear();
  vi.clearAllMocks();
  Element.prototype.scrollIntoView = vi.fn();
});

describe("EngineeringBaseline review", () => {
  it("renders changes in the Wiki context and decides only the reviewed revision", async () => {
    const user = userEvent.setup();
    const onDecision = vi.fn();
    const current = revision();
    renderBaseline(<EngineeringBaseline {...baseProps} chapters={[chapter(current, adopted())]} revision={current} currentCandidate reviewRequested onDecision={onDecision} />);

    await waitFor(() => expect(document.querySelector('[data-diff-kind="modified"]')).toHaveTextContent("所有写接口必须携带幂等键"));
    expect(document.querySelector("[data-diff-before]")).toHaveTextContent("所有写接口应该携带幂等键");
    expect(document.querySelector('[data-diff-kind="added"]')).toHaveTextContent("预占快照");
    expect(document.querySelector('[data-diff-kind="removed"]')).toHaveTextContent("旧规则");
    expect(document.querySelector("[data-baseline-warnings]")).not.toBeInTheDocument();
    expect(screen.getByText(/Agent agent-1/)).toBeInTheDocument();
    expect(screen.queryByText("Source task")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Adopt" }));
    expect(onDecision).toHaveBeenCalledWith("adopt", current);
  });

  it("keeps a historical withdrawn revision read-only", () => {
    const historical = revision("withdrawn");
    renderBaseline(<EngineeringBaseline {...baseProps} chapters={[chapter(null, adopted())]} revision={historical} currentCandidate={false} onDecision={vi.fn()} />);

    expect(screen.getByText("Withdrawn")).toBeInTheDocument();
    expect(screen.getByText(/historical revision is read-only/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Adopt" })).not.toBeInTheDocument();
  });

  it("shows an exact deep-link miss without displaying another candidate", () => {
    renderBaseline(<EngineeringBaseline {...baseProps} chapters={[chapter(revision(), adopted())]} revision={null} currentCandidate={false} candidateNotFound onDecision={vi.fn()} />);

    expect(screen.getByText("The requested revision was not found")).toBeInTheDocument();
    expect(screen.queryByText("Review wording")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Adopt" })).not.toBeInTheDocument();
  });

  it("shows decision open issues and history credentials", () => {
    const current = revision();
    const result = {
      chapters: [],
      openIssues: { inFlight: [{ id: "issue-1", identifier: "OL-92", title: "Frontend", status: "in_progress", statusCategory: "started", assigneeType: "member", assigneeId: "user-1" }], queued: [], truncated: false },
    } as BaselineDecision;
    renderBaseline(<EngineeringBaseline {...baseProps} chapters={[chapter(current, adopted())]} revision={current} currentCandidate decisionResult={result} history={{ revisions: [revision("adopted")], hasMore: false, nextBefore: null }} onDecision={vi.fn()} />);

    expect(document.querySelector("[data-open-issues]")).toHaveTextContent("OL-92");
    expect(screen.getByText("Confirmed with a personal token")).toBeInTheDocument();
  });
});

describe("EngineeringBaseline Wiki", () => {
  it("switches between concept and management lenses and remembers the choice", async () => {
    const user = userEvent.setup();
    const current = revision();
    const wikiContent = current.contentMarkdown.replace("下单时记录库存快照。", "预占快照依赖幂等键，由 OL-42 在 Stage 6 实现。");
    renderBaseline(<EngineeringBaseline {...baseProps} chapters={[chapter(null, { ...adopted(), contentMarkdown: wikiContent })]} revision={null} currentCandidate={false} onDecision={vi.fn()} />);

    expect(screen.getByRole("tab", { name: "Project understanding" })).toHaveAttribute("aria-selected", "true");
    const article = document.querySelector('[data-baseline-section-key="domain_rules:预占快照"]')!;
    expect(within(article.querySelector("[data-baseline-markdown]") as HTMLElement).getByRole("button", { name: "幂等键" })).toBeInTheDocument();
    expect(within(article.querySelector("[data-baseline-markdown]") as HTMLElement).queryByRole("button", { name: "OL-42" })).not.toBeInTheDocument();
    expect(screen.getByText("Why this rule exists")).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "Project management" }));
    expect(screen.getByRole("tab", { name: "Project management" })).toHaveAttribute("aria-selected", "true");
    expect(window.localStorage.getItem("multica.engineering-baseline.lens.project-1")).toBe("manage");
    const issue = within(article.querySelector("[data-baseline-markdown]") as HTMLElement).getByRole("button", { name: "OL-42" });
    await user.hover(issue);
    expect(await screen.findByText("Implement idempotency")).toBeInTheDocument();
    expect(document.querySelector('[data-reference-card="issue"]')).toHaveTextContent("Stage 6");
    await user.click(issue);
    expect(navigation.push).toHaveBeenCalledWith("/acme/issues/issue-42");
  });

  it("searches the full Wiki and exposes reading guidance", async () => {
    const user = userEvent.setup();
    const current = revision();
    renderBaseline(<EngineeringBaseline {...baseProps} chapters={[chapter(null, { ...adopted(), contentMarkdown: current.contentMarkdown })]} revision={null} currentCandidate={false} onDecision={vi.fn()} />);

    expect(screen.getByText(/Start here/)).toBeInTheDocument();
    await user.type(screen.getByRole("searchbox", { name: "Search the baseline" }), "快照");
    expect(screen.getByText("1 matches")).toBeInTheDocument();
    expect(document.querySelector("[data-search-results]")).toHaveTextContent("预占快照");
  });
});
