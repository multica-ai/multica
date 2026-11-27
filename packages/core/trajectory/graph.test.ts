// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  TrajectoryGraphSchema, TrajectoryNodeSchema, buildGraphProjects, buildGraphTimeline,
  emptyTrajectory, issueCreationContext, mergeTrajectoryGraphs, runExecutionSymbol,
  sanitizeTrajectoryGraph, trajectorySymbol, type TrajectoryGraph, type TrajectoryNode,
} from "./graph";

const at = (second: number) => `2026-11-19T00:00:${String(second).padStart(2, "0")}Z`;
function node(kind: string, id: string, issueId: string, second = 0, details: Record<string, unknown> = {}): TrajectoryNode {
  return { id, kind, issue_id: issueId, title: id, status: "unknown", created_at: at(second), details };
}
function issue(id: string, second = 0, details: Record<string, unknown> = {}) {
  return node("issue", `issue:${id}`, id, second, details);
}
function run(id: string, issueId: string, second = 0, details: Record<string, unknown> = {}) {
  return { ...node("run", `run:${id}`, issueId, second, details), run_id: id };
}
function graph(nodes: TrajectoryNode[], overrides: Partial<TrajectoryGraph> = {}): TrajectoryGraph {
  return { root_issue_id: "root", nodes, edges: [], findings: [], evidence: [],
    coverage: { complete: true, truncated: false, warnings: [] }, ...overrides };
}
const ids = (rows: ReturnType<typeof buildGraphTimeline>) => rows.map(row => row.id);

describe("native graph contract", () => {
  it("defaults missing native fields without dropping future kinds or unknown details", () => {
    const parsed = TrajectoryGraphSchema.parse({ root_issue_id: "root", nodes: [
      { id: "future:1", kind: "future", issue_id: "root", details: { later: { value: 3 } } },
    ], edges: [] });
    expect(parsed.nodes[0]).toEqual({ id: "future:1", kind: "future", issue_id: "root",
      title: "", status: "unknown", created_at: "", details: { later: { value: 3 } } });
    expect(parsed.coverage).toEqual({ complete: false, truncated: false, warnings: [] });
    expect(parsed.findings).toEqual([]);
    expect(parsed.evidence).toEqual([]);
  });
  it("accepts nullable optional native context but rejects invalid stages and counts", () => {
    expect(TrajectoryNodeSchema.safeParse({ id: "issue:r", kind: "issue", issue_id: "r",
      details: { project_id: null, stage: null, creation_run_id: null } }).success).toBe(true);
    for (const details of [{ stage: 0 }, { stage: 1.5 }, { run_count: -1 }, { run_count: 1.5 }]) {
      expect(TrajectoryNodeSchema.safeParse({ id: "issue:r", kind: "issue", issue_id: "r", details }).success).toBe(false);
    }
  });
  it("preserves optional finding and evidence references", () => {
    const payload = graph([], { findings: [{ id: "f", code: "missing", node_id: "run:r", severity: "warning",
      message: "missing", evidence_id: "e", source_quote: "quote", message_seq: 2, message_id: "m" }],
      evidence: [{ id: "e", issue_id: "root", run_id: "r", kind: "status", data: { status: "blocked" },
        recorded_by: "platform", created_at: at(0) }] });
    expect(TrajectoryGraphSchema.parse(payload)).toEqual(payload);
  });
  it("keeps malformed response fallback incomplete and actionable", () => {
    const fallback = emptyTrajectory("root");
    expect(fallback.root_issue_id).toBe("root");
    expect(fallback.nodes).toEqual([]);
    expect(fallback.coverage.complete).toBe(false);
    expect(fallback.coverage.warnings).toEqual(["Invalid trajectory response. Refresh to retry."]);
  });
});

describe("native status and creation context", () => {
  it.each(["queued", "deferred", "pending"])("shows %s as a neutral pending dot", status => {
    expect(runExecutionSymbol(status)).toEqual({ meaning: "pending", tone: "neutral", glyph: "circle" });
  });
  it.each([
    ["completed", "blocked", "workflow_blocked", "danger", "pause"],
    ["failed", "blocked", "failed", "danger", "x"],
    ["cancelled", "blocked", "cancelled", "neutral", "x"],
    ["blocked", null, "blocked", "danger", "pause"],
    ["completed", null, "ended", "success", "check"],
    ["running", null, "running", "info", "play"],
    ["dispatched", null, "starting", "info", "play"],
    ["waiting_local_directory", null, "waiting", "warning", "clock"],
  ])("maps %s with workflow %s using native precedence", (status, workflow, meaning, tone, glyph) => {
    expect(runExecutionSymbol(status!, workflow)).toEqual({ meaning, tone, glyph });
  });
  it("shows review before done category and never infers status from text", () => {
    expect(trajectorySymbol({ ...issue("r", 0, { status_category: "done" }), status: "in_review" }))
      .toEqual({ meaning: "review", tone: "warning", glyph: "clock" });
    expect(trajectorySymbol({ ...issue("r"), title: "failed blocked", status: "future" }))
      .toEqual({ meaning: "unknown", tone: "neutral", glyph: "circle" });
    expect(trajectorySymbol(node("artifact", "attachment:a", "r"))).toEqual({ meaning: "artifact", tone: "merged", glyph: "file" });
    expect(trajectorySymbol({ ...node("check", "check:a", "r"), status: "passed" })).toEqual({ meaning: "passed", tone: "success", glyph: "check" });
  });
  it("requires persisted blocked evidence no later than creation", () => {
    const details = { creation_run_id: "r", creation_issue_id: "owner", creation_source_status: "blocked",
      creation_status_event_id: "event", creation_status_event_at: at(10), creation_workflow_step: "blocked" };
    expect(issueCreationContext(issue("child", 10, details))).toEqual({ kind: "after_blocked", runId: "r", sourceIssueId: "owner", sourceId: "event" });
    expect(issueCreationContext(issue("child", 9, details))).toBeNull();
    expect(issueCreationContext(issue("child", 10, { ...details, creation_status_event_id: undefined }))).toBeNull();
    expect(issueCreationContext(issue("child", 10, { creation_run_id: "r", creation_issue_id: "owner", creation_workflow_step: "blocked" }))).toBeNull();
  });
  it("accepts only native delegated failure evidence as a failure creation marker", () => {
    const details = { creation_run_id: "r", creation_issue_id: "owner", creation_trigger_evidence_kind: "delegated_failure", creation_trigger_evidence_ref_id: "failure" };
    expect(issueCreationContext(issue("child", 10, details))).toEqual({ kind: "after_failure", runId: "r", sourceIssueId: "owner", sourceId: "failure" });
    expect(issueCreationContext(issue("child", 10, { ...details, creation_trigger_evidence_kind: "comment" }))).toBeNull();
    expect(issueCreationContext(run("r", "child", 10, details))).toBeNull();
  });
});

describe("project membership", () => {
  it("counts only matching loaded Issues and preserves unknown versus no project", () => {
    const g = graph([issue("context", 0, { project_id: "p", project_title: "Project" }),
      issue("b", 2, { project_id: "p", project_title: "Project" }), issue("a", 1, { project_id: "p", project_title: "Project" }),
      issue("none", 3, { project_id: null }), issue("unknown", 4), run("r", "a")]);
    expect(buildGraphProjects(g, new Set(["b", "a", "none", "unknown", "missing"]))).toEqual([
      { key: "p", projectId: "p", title: "Project", issueIds: ["a", "b"] },
      { key: "none", projectId: null, title: "No project", issueIds: ["none"] },
      { key: "unknown", projectId: null, title: "Unknown project", issueIds: ["unknown"] },
    ]);
  });
});

describe("Issue and Run rows", () => {
  it("leaves the Run count unknown when neither a stored count nor loaded Runs exists", () => {
    const rows = buildGraphTimeline(graph([issue("unknown")]), new Set(["unknown"]), new Set());
    expect(rows[0]?.runCount).toBeUndefined();
  });
  it("keeps structural children visible while each Issue independently expands its own Runs", () => {
    const g = graph([issue("parent", 0, { run_count: 3 }), issue("child", 2, { parent_id: "parent", stage: 1 }),
      run("p", "parent", 1), run("c", "child", 3)]);
    const collapsed = buildGraphTimeline(g, new Set(["child"]), new Set());
    expect(collapsed.map(({ id, depth, parentId, context, runCount }) => ({ id, depth, parentId, context, runCount }))).toEqual([
      { id: "issue:parent", depth: 0, parentId: undefined, context: true, runCount: 3 },
      { id: "issue:child", depth: 1, parentId: "parent", context: false, runCount: 1 },
    ]);
    expect(ids(buildGraphTimeline(g, new Set(["child"]), new Set(["child"]))))
      .toEqual(["issue:parent", "issue:child", "run:c"]);
    expect(ids(buildGraphTimeline(g, new Set(["child"]), new Set(["parent"]))))
      .toEqual(["issue:parent", "run:p", "issue:child"]);
  });
  it("interleaves sibling and parent execution by time then id inside each root", () => {
    const g = graph([run("z", "parent", 4), issue("sibling", 2, { parent_id: "parent" }),
      issue("parent", 8), run("b", "parent", 2), issue("child", 1, { parent_id: "parent" }),
      run("a", "child", 2), issue("other", 0), run("other", "other", 3)]);
    expect(ids(buildGraphTimeline(g, new Set(["parent", "other"]), new Set(["parent", "child", "other"]))))
      .toEqual(["issue:other", "run:other", "issue:parent", "issue:child", "issue:sibling", "run:a", "run:b", "run:z"]);
  });
  it("emits cycles, orphans and duplicated identities once without assigning a Run through relation edges", () => {
    const g = graph([issue("a", 0, { parent_id: "b" }), issue("b", 1, { parent_id: "a" }),
      issue("orphan", 2, { parent_id: "absent" }), issue("a", 0, { parent_id: "b" }), run("r", "b", 3), run("r", "b", 3), run("outside", "absent", 4)],
      { edges: [{ id: "wrong", from: "issue:a", to: "run:r", kind: "contains" }] });
    const rows = buildGraphTimeline(g, new Set(["a", "orphan"]), new Set(["a", "b", "orphan"]));
    expect(ids(rows)).toEqual(["issue:a", "issue:b", "run:r", "issue:orphan"]);
    expect(rows.find(row => row.id === "run:r")?.parentId).toBe("b");
    expect(new Set(ids(rows)).size).toBe(rows.length);
  });
  it("keeps output previews and source-owned records with their expanded Run", () => {
    const g = graph([issue("owner"), run("r", "owner", 1, { output: "preview" }),
      node("comment", "comment:c", "owner", 3, { comment_id: "c", source_run_id: "r", content: "result" }),
      node("artifact", "attachment:a", "owner", 4, { attachment_id: "a", comment_id: "c", source_run_id: "r" }),
      node("platform_event", "fact:f", "owner", 2, { event: "status" }),
      node("artifact", "attachment:unowned", "owner", 5, { attachment_id: "unowned", task_id: "r" })]);
    expect(ids(buildGraphTimeline(g, new Set(["owner"]), new Set(["owner"]))))
      .toEqual(["issue:owner", "run:r", "fact:f", "attachment:unowned"]);
    const expanded = buildGraphTimeline(g, new Set(["owner"]), new Set(["owner"]), new Set(["r"]));
    expect(ids(expanded)).toEqual(["issue:owner", "run:r", "outputs:run:r", "comment:c", "attachment:a", "fact:f", "attachment:unowned"]);
    expect(expanded.find(row => row.id === "comment:c")).toMatchObject({ kind: "record", parentId: "run:r", depth: 2 });
  });
  it("does not attribute a comment or artifact to a Run owned by another Issue", () => {
    const g = graph([issue("a"), issue("b", 1), run("rb", "b", 2),
      node("comment", "comment:wrong", "a", 3, { source_run_id: "rb" }),
      node("artifact", "attachment:wrong", "a", 4, { source_run_id: "rb" })]);
    const rows = buildGraphTimeline(g, new Set(["a", "b"]), new Set(["a", "b"]), new Set(["rb"]));
    expect(rows.find(row => row.id === "comment:wrong")).toMatchObject({ parentId: "a", depth: 1 });
    expect(rows.find(row => row.id === "attachment:wrong")).toMatchObject({ parentId: "a", depth: 1 });
  });
});

describe("merging separately loaded graphs", () => {
  it("updates by identity while retaining all records, bounded coverage and base root", () => {
    const base = graph([issue("root", 0, { run_count: 1 })], { coverage: { complete: true, truncated: true, warnings: ["window"] } });
    const own = graph([issue("root", 0, { run_count: 2 }), run("b", "root", 2), run("a", "root", 1)],
      { root_issue_id: "different", coverage: { complete: false, truncated: false, warnings: ["window", "snapshot"] } });
    const merged = mergeTrajectoryGraphs(base, [own]);
    expect(merged.root_issue_id).toBe("root");
    expect(merged.nodes.map(value => value.id)).toEqual(["issue:root", "run:a", "run:b"]);
    expect(merged.nodes[0]?.details.run_count).toBe(2);
    expect(merged.coverage).toEqual({ complete: false, truncated: true, warnings: ["window", "snapshot"] });
    expect(base.nodes[0]?.details.run_count).toBe(1);
  });
});

describe("denied graph content", () => {
  it.each([
    ["native trigger comment", { trigger_comment_id: "c" }],
    ["native evidence comment", { trigger_evidence_kind: "comment", trigger_evidence_ref_id: "c" }],
    ["trigger comment alias", { triggerCommentId: "c" }],
    ["evidence comment alias", { triggerEvidenceKind: "comment", triggerEvidenceRefId: "c" }],
  ])("removes a title copied from a denied %s while preserving allowed Run metadata", (_name, reference) => {
    const payload = TrajectoryGraphSchema.parse(graph([
      { ...run("r", "root", 1, { ...reference, trigger_content: "deleted-trigger-body",
        instructions: "Allowed instructions", output: "Allowed output", agent_name: "Agent" }),
      title: "deleted-trigger-summary-sentinel" },
    ]));
    const clean = sanitizeTrajectoryGraph(payload, new Set(["comment:c"]));
    expect(clean.nodes[0]?.title).toBe("Agent run");
    expect(clean.nodes[0]).toMatchObject({ id: "run:r", run_id: "r", issue_id: "root" });
    expect(clean.nodes[0]?.details).toEqual({ ...reference, instructions: "Allowed instructions", output: "Allowed output", agent_name: "Agent" });
    expect(payload.nodes[0]?.title).toBe("deleted-trigger-summary-sentinel");
    expect(payload.nodes[0]?.details.trigger_content).toBe("deleted-trigger-body");
  });
  it("preserves unrelated Run titles even if their text resembles a denied comment", () => {
    const payload = TrajectoryGraphSchema.parse(graph([
      { ...run("allowed-comment", "root", 1, { trigger_comment_id: "allowed", trigger_content: "Allowed body" }), title: "Allowed comment summary" },
      { ...run("no-reference", "root", 2), title: "deleted-trigger-summary-sentinel comment:c" },
      { ...run("non-comment", "root", 3, { trigger_evidence_kind: "platform_fact", trigger_evidence_ref_id: "c" }), title: "Allowed fact summary" },
    ]));
    const clean = sanitizeTrajectoryGraph(payload, new Set(["comment:c"]));
    expect(clean.nodes.map(value => value.title)).toEqual(["Allowed comment summary", "deleted-trigger-summary-sentinel comment:c", "Allowed fact summary"]);
    expect(clean.nodes[0]?.details.trigger_content).toBe("Allowed body");
  });
  it("scrubs denied prefixed endpoints even when the referenced node is outside the loaded window", () => {
    const g = graph([issue("root"), run("r", "root", 1, { trigger_comment_id: "comment:c", trigger_content: "private" })],
      { edges: [{ id: "outside", from: "run:r", to: "comment:c", kind: "produces" }],
        findings: [{ id: "outside", code: "comment", node_id: "comment:c", severity: "warning", message: "private" }] });
    const clean = sanitizeTrajectoryGraph(g, new Set(["c"]));
    expect(clean.edges).toEqual([]);
    expect(clean.findings).toEqual([]);
    expect(clean.nodes[1]?.details).toEqual({ trigger_comment_id: "comment:c" });
  });
  it("preserves unavailable creation source IDs without returning deleted comment content", () => {
    const g = graph([issue("comment-child", 0, { creation_trigger_evidence_kind: "comment", creation_trigger_evidence_ref_id: "c" }),
      issue("failure-child", 1, { creation_trigger_evidence_kind: "delegated_failure", creation_trigger_evidence_ref_id: "c" })]);
    const clean = sanitizeTrajectoryGraph(g, new Set(["comment:c"]));
    expect(clean.nodes[0]?.details).toEqual({ creation_trigger_evidence_kind: "comment", creation_trigger_evidence_ref_id: "c" });
    expect(clean.nodes[1]?.details).toEqual({ creation_trigger_evidence_kind: "delegated_failure", creation_trigger_evidence_ref_id: "c" });
  });
  it("removes denied comments and linked attachments and scrubs their Run trigger copies only", () => {
    const r = run("r", "root", 1, { trigger_comment_id: "c", trigger_content: "private", triggerContent: "private",
      instructions: "allowed", output: "allowed", trigger_kind: "comment", trigger_evidence_ref_id: "allowed-fact", agent_name: "Agent" });
    const g = graph([issue("root"), r, node("comment", "comment:c", "root", 2, { comment_id: "c", content: "private" }),
      node("artifact", "attachment:a", "root", 3, { comment_id: "c", attachment_id: "a" }),
      node("comment", "comment:allowed", "root", 4, { comment_id: "allowed", content: "allowed" })],
      { edges: [{ id: "edge", from: "run:r", to: "comment:c", kind: "produces" }],
        findings: [{ id: "f", code: "comment", node_id: "comment:c", severity: "warning", message: "private" }] });
    const clean = sanitizeTrajectoryGraph(g, new Set(["comment:c"]));
    expect(clean.nodes.map(value => value.id)).toEqual(["issue:root", "run:r", "comment:allowed"]);
    expect(clean.nodes[1]?.details).toEqual({ trigger_comment_id: "c", instructions: "allowed", output: "allowed", trigger_kind: "comment",
      trigger_evidence_ref_id: "allowed-fact", agent_name: "Agent" });
    expect(clean.edges).toEqual([]);
    expect(clean.findings).toEqual([]);
    expect(r.details.trigger_content).toBe("private");
  });
  it("scrubs comment-evidence trigger aliases without erasing an allowed Run or inferring provenance from text", () => {
    const g = graph([issue("root"), run("denied-copy", "root", 1, { triggerEvidenceKind: "comment", triggerEvidenceRefId: "c", triggerBody: "secret", trigger_body: "secret", workflow_step: "blocked" }),
      run("allowed", "root", 2, { trigger_evidence_kind: "platform_fact", trigger_evidence_ref_id: "c", trigger_content: "literal comment:c", instructions: "comment:c" }),
      run("no-reference", "root", 3, { trigger_content: "literal comment:c" })]);
    const clean = sanitizeTrajectoryGraph(g, new Set(["c"]));
    expect(clean.nodes[1]?.details).toEqual({ triggerEvidenceKind: "comment", triggerEvidenceRefId: "c", workflow_step: "blocked" });
    expect(clean.nodes[2]?.details).toEqual(g.nodes[2]?.details);
    expect(clean.nodes[3]?.details).toEqual(g.nodes[3]?.details);
  });
  it("removes denied Issue-owned content but retains an allowed Issue's metadata and references", () => {
    const g = graph([issue("secret"), run("secret", "secret"), node("comment", "comment:secret", "secret"),
      issue("allowed", 0, { parent_id: "secret", project_id: "p", creation_run_id: "secret", creation_issue_id: "secret" }),
      run("allowed", "allowed", 1, { issue_snapshot: { title: "Allowed" }, trigger_content: "allowed", agent_name: "Agent" })],
      { edges: [{ id: "rel", from: "issue:secret", to: "issue:allowed", kind: "parent" }] });
    const clean = sanitizeTrajectoryGraph(g, new Set(["issue:secret"]));
    expect(clean.nodes.map(value => value.id)).toEqual(["issue:allowed", "run:allowed"]);
    expect(clean.nodes[0]?.details).toEqual({ parent_id: "secret", project_id: "p", creation_run_id: "secret", creation_issue_id: "secret" });
    expect(clean.nodes[1]?.details).toEqual(g.nodes[4]?.details);
    expect(clean.edges).toEqual([]);
  });
  it("removes denied evidence and quotes while preserving allowed finding metadata", () => {
    const g = graph([issue("root"), run("r", "root")], { evidence: [
      { id: "e", issue_id: "root", kind: "comment", data: { comment_id: "c", content: "private" }, recorded_by: "platform", created_at: at(0) },
      { id: "ok", issue_id: "root", kind: "status", data: { status: "blocked" }, recorded_by: "platform", created_at: at(0) }],
      findings: [{ id: "f", code: "missing", node_id: "run:r", severity: "warning", message: "missing",
        evidence_id: "e", source_quote: "private", message_id: "c", message_seq: 1 }],
      edges: [{ id: "e-edge", from: "issue:root", to: "run:r", kind: "contains", evidence_id: "e" }] });
    const clean = sanitizeTrajectoryGraph(g, new Set(["comment:c"]));
    expect(clean.evidence.map(value => value.id)).toEqual(["ok"]);
    expect(clean.findings).toEqual([{ id: "f", code: "missing", node_id: "run:r", severity: "warning", message: "missing", message_seq: 1 }]);
    expect(clean.edges).toEqual([{ id: "e-edge", from: "issue:root", to: "run:r", kind: "contains" }]);
  });
});
