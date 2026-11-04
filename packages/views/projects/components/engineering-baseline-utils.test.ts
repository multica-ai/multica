// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { BaselineRevision, IssueGraphNode } from "@multica/core/api";
import { matchBaselineReferences, mergeBaselineSections, parseBaselineChapter } from "./engineering-baseline-utils";
import type { EngineeringStageSummary } from "./engineering-utils";

const revision = (): BaselineRevision => ({
  id: "rev-1",
  workspaceId: "ws-1",
  projectId: "project-1",
  chapter: "domain_rules",
  revisionNumber: 2,
  status: "candidate",
  contentMarkdown: "## [Rule-1] 幂等键\n所有写接口必须携带幂等键，OL-2 在 Stage 6 实现。\n\n## 预占快照\n记录快照。",
  changeSummary: "Update rules",
  baseRevisionId: "rev-base",
  replacesRevisionId: null,
  sourceIssueId: null,
  anchorsDiff: {
    chapter: "domain_rules",
    baseRevisionNumber: 1,
    added: [{ anchor: "预占快照", title: "预占快照", before: "", after: "记录快照。", beforeChars: 0, afterChars: 5 }],
    modified: [{ anchor: "Rule-1", title: "幂等键", before: "所有写接口应该携带幂等键。", after: "所有写接口必须携带幂等键。", beforeChars: 12, afterChars: 12 }],
    removed: [{ anchor: "Rule-old", title: "旧规则", before: "删除这段。", after: "", beforeChars: 5, afterChars: 0 }],
    unchangedCount: 0,
    warnings: [],
  },
  createdByType: "agent",
  createdById: "agent-1",
  createdByTaskId: null,
  createdAt: "2026-01-01T00:00:00Z",
  decidedByType: null,
  decidedById: null,
  decidedByTaskId: null,
  decidedAt: null,
  decisionCredential: null,
});

describe("parseBaselineChapter", () => {
  it("parses natural headings, skips fences, and extracts optional rationale and examples", () => {
    const document = parseBaselineChapter("domain_rules", [
      "## [Rule-1] 幂等键",
      "所有写接口必须携带幂等键。",
      "",
      "为什么：重试不能产生重复订单。",
      "",
      "例：同一请求重发两次只创建一个订单。",
      "",
      "```markdown",
      "## 不是小节",
      "```",
      "### 订单状态",
      "已支付后进入履约。",
    ].join("\n"));
    expect(document.sections).toHaveLength(2);
    expect(document.sections[0]).toMatchObject({
      key: "domain_rules:Rule-1",
      title: "幂等键",
      body: "所有写接口必须携带幂等键。\n\n```markdown\n## 不是小节\n```",
      why: "重试不能产生重复订单。",
      example: "同一请求重发两次只创建一个订单。",
    });
    expect(document.sections[1]?.title).toBe("订单状态");
  });

  it("keeps content before the first heading instead of dropping it", () => {
    const document = parseBaselineChapter("overview", "项目的自然语言简介。");
    expect(document.sections[0]).toMatchObject({ anchor: "intro", title: "", body: "项目的自然语言简介。" });
  });
});

describe("baseline references", () => {
  const sections = [
    ...parseBaselineChapter("domain_rules", "## 幂等键\n定义幂等键。").sections,
    ...parseBaselineChapter("architecture", "## 调用约束\n幂等键再次出现幂等键，OL-2 在 Stage 6。").sections,
  ];
  const nodes = [{ id: "issue-2", identifier: "OL-2", title: "Implement", stage: 6 }] as IssueGraphNode[];
  const stages = [{ stage: 6, title: "Frontend", goal: "", total: 1, closed: 0 }] as EngineeringStageSummary[];

  it("marks only the first external concept occurrence in understanding mode", () => {
    const current = sections[1]!;
    const matches = matchBaselineReferences(current.body, {
      sections,
      currentSectionKey: current.key,
      nodes,
      stages,
      conceptsOnly: true,
      seenConcepts: new Set(),
    });
    expect(matches.map((match) => match.reference.kind)).toEqual(["concept"]);
    expect(current.body.slice(matches[0]!.start, matches[0]!.end)).toBe("幂等键");
  });

  it("marks concepts, issue identifiers, and stages in management mode", () => {
    const current = sections[1]!;
    const matches = matchBaselineReferences(current.body, {
      sections,
      currentSectionKey: current.key,
      nodes,
      stages,
    });
    expect(matches.map((match) => match.reference.kind)).toEqual(["concept", "concept", "issue", "stage"]);
  });
});

describe("mergeBaselineSections", () => {
  it("embeds added, modified, and removed sections in the Wiki context", () => {
    const sections = mergeBaselineSections(
      "domain_rules",
      "## [Rule-1] 幂等键\n旧正文。\n\n## [Rule-old] 旧规则\n删除这段。",
      revision(),
      true,
    );
    expect(sections.map((section) => [section.title, section.change])).toEqual([
      ["幂等键", "modified"],
      ["旧规则", "removed"],
      ["预占快照", "added"],
    ]);
    expect(sections[0]?.beforeBody).toBe("所有写接口应该携带幂等键。");
  });

  it("uses the adopted document outside comparison mode", () => {
    const sections = mergeBaselineSections("domain_rules", "## 正本\n内容。", revision(), false);
    expect(sections).toEqual([expect.objectContaining({ title: "正本", change: "same" })]);
  });
});
