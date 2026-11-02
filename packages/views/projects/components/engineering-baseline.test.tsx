import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { BaselineDecision, BaselineRevision, ChapterState } from "@multica/core/api";
import { EngineeringBaseline } from "./engineering-baseline";
import { renderWithI18n } from "../../test/i18n";

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => `Actor ${id}` }),
}));
vi.mock("../../issues/utils/status-label", () => ({
  useStatusLabel: () => (status: string) => status,
}));

const revision = (status: BaselineRevision["status"] = "candidate"): BaselineRevision => ({
  id: "rev-1", workspaceId: "ws-1", projectId: "project-1", chapter: "domain_rules", revisionNumber: 3, status,
  contentMarkdown: "### [Rule-1] Updated", changeSummary: "Review wording", baseRevisionId: "rev-base", replacesRevisionId: null,
  sourceIssueId: null,
  anchorsDiff: {
    chapter: "domain_rules", baseRevisionNumber: 2,
    added: [], modified: [{ anchor: "Rule-1", title: "Mandatory", before: "MUST keep the rule", after: "SHOULD keep the rule", beforeChars: 18, afterChars: 20 }], removed: [], unchangedCount: 4,
    warnings: [{ code: "rule_shrunk", anchor: "Rule-1" }],
  },
  createdByType: "member", createdById: "user-1", createdByTaskId: null, createdAt: "2026-01-01T00:00:00Z",
  decidedByType: status === "candidate" ? null : "member", decidedById: status === "candidate" ? null : "user-1", decidedByTaskId: null,
  decidedAt: status === "candidate" ? null : "2026-01-02T00:00:00Z", decisionCredential: status === "candidate" ? null : "pat",
});
const chapter = (candidate: BaselineRevision | null): ChapterState => ({ chapter: "domain_rules", adopted: null, candidate });
const baseProps = {
  workspaceId: "ws-1", selectedChapter: "domain_rules" as const, onChapterChange: vi.fn(), candidateLoading: false,
  candidateNotFound: false, canDecide: true, decisionPending: false, decisionError: false, decisionResult: null,
  history: null, historyLoading: false, historyError: false, onHistoryMore: vi.fn(),
};

describe("EngineeringBaseline", () => {
  it("renders line-level changes and warnings and decides only the reviewed revision", async () => {
    const user = userEvent.setup();
    const onDecision = vi.fn();
    const current = revision();
    renderWithI18n(<EngineeringBaseline {...baseProps} chapters={[chapter(current)]} revision={current} currentCandidate onDecision={onDecision} />);

    expect(document.querySelector('[data-baseline-warnings]')).toHaveTextContent("Rule-1");
    expect(document.querySelector('[data-diff-kind="modified"]')).toHaveTextContent("MUST keep the rule");
    expect(document.querySelector('[data-diff-kind="modified"]')).toHaveTextContent("SHOULD keep the rule");
    await user.click(screen.getByRole("button", { name: "Adopt" }));
    expect(onDecision).toHaveBeenCalledWith("adopt", current);
  });

  it("keeps a historical withdrawn revision read-only", () => {
    const historical = revision("withdrawn");
    renderWithI18n(<EngineeringBaseline {...baseProps} chapters={[chapter(null)]} revision={historical} currentCandidate={false} onDecision={vi.fn()} />);

    expect(screen.getByText("Withdrawn")).toBeInTheDocument();
    expect(screen.getByText(/historical revision is read-only/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Adopt" })).not.toBeInTheDocument();
  });

  it("shows an exact deep-link miss without displaying another candidate", () => {
    renderWithI18n(<EngineeringBaseline {...baseProps} chapters={[chapter(revision())]} revision={null} currentCandidate={false} candidateNotFound onDecision={vi.fn()} />);

    expect(screen.getByText("The requested revision was not found")).toBeInTheDocument();
    expect(screen.queryByText("Review wording")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Adopt" })).not.toBeInTheDocument();
  });

  it("shows decision open issues and history credentials", () => {
    const current = revision();
    const result = {
      chapters: [], openIssues: { inFlight: [{ id: "issue-1", identifier: "OL-92", title: "Frontend", status: "in_progress", statusCategory: "started", assigneeType: "member", assigneeId: "user-1" }], queued: [], truncated: false },
    } as BaselineDecision;
    renderWithI18n(<EngineeringBaseline {...baseProps} chapters={[chapter(current)]} revision={current} currentCandidate decisionResult={result} history={{ revisions: [revision("adopted")], hasMore: false, nextBefore: null }} onDecision={vi.fn()} />);

    expect(document.querySelector('[data-open-issues]')).toHaveTextContent("OL-92");
    expect(screen.getByText("Confirmed with a personal token")).toBeInTheDocument();
  });

  it("uses neutral agent execution labels for adopted and rejected history", () => {
    const adopted = { ...revision("adopted"), id: "rev-adopted", revisionNumber: 3, decisionCredential: "agent_task" as const, decidedByType: "agent" as const, decidedById: "agent-1" };
    const rejected = { ...revision("rejected"), id: "rev-rejected", revisionNumber: 2, decisionCredential: "agent_task" as const, decidedByType: "agent" as const, decidedById: "agent-1" };
    renderWithI18n(
      <EngineeringBaseline {...baseProps} chapters={[chapter(null)]} revision={null} currentCandidate={false} history={{ revisions: [adopted, rejected], hasMore: false, nextBefore: null }} onDecision={vi.fn()} />,
      { locale: "zh-Hans" },
    );

    const adoptedRow = screen.getByText("第 3 版").closest("li");
    const rejectedRow = screen.getByText("第 2 版").closest("li");
    expect(adoptedRow).toHaveTextContent("已采用");
    expect(adoptedRow).toHaveTextContent("智能体执行");
    expect(adoptedRow).toHaveTextContent("Actor agent-1");
    expect(rejectedRow).toHaveTextContent("已驳回");
    expect(rejectedRow).toHaveTextContent("智能体执行");
    expect(rejectedRow).toHaveTextContent("Actor agent-1");
    expect(rejectedRow).not.toHaveTextContent("采用");
    expect(rejectedRow).not.toHaveTextContent(/confirmed/i);
  });
});
