import type { TrajectoryGraph, TrajectoryNode } from "@multica/core/trajectory/graph";

/** Synthetic native responses, used only by tests and visual harnesses. */
export const graphId = (n: number) => `00000000-0000-4000-8000-${String(n).padStart(12, "0")}`;
export const graphScope = { accountId: graphId(1), workspaceId: graphId(2) };
export const graphQuery = { scope: { kind: "workspace" as const }, filters: {}, sort: { field: "created_at" as const, direction: "asc" as const } };
const at = "2026-11-19T00:00:00Z";
export const graphNode = (kind: string, n: number, owner: number, title: string, details: Record<string, unknown> = {}): TrajectoryNode => ({
  id: `${kind === "artifact" ? "attachment" : kind}:${graphId(n)}`, kind, issue_id: graphId(owner),
  ...(kind === "run" ? { run_id: graphId(n) } : {}), title, status: kind === "run" ? "completed" : "todo", created_at: at, details,
});
const graph = (nodes: TrajectoryNode[]): TrajectoryGraph => ({ root_issue_id: graphId(10), nodes, edges: [], findings: [], evidence: [], coverage: { complete: true, truncated: false, warnings: [] } });
export function graphFixture(count = 3) {
  const parent = graphNode("issue", 10, 10, "Synthetic parent", { project_id: graphId(30), project_title: "First project", issue_key: "SYN-10", run_count: 1, description: "Current parent description" });
  const child = graphNode("issue", 11, 11, "Synthetic child", { project_id: graphId(30), project_title: "First project", issue_key: "SYN-11", stage: 2, parent_id: graphId(10), run_count: 1 });
  const other = graphNode("issue", 12, 12, "Synthetic other", { project_id: graphId(31), project_title: "Second project", issue_key: "SYN-12", run_count: 0 });
  const run = graphNode("run", 20, 10, "Synthetic execution", { started_at: at, completed_at: "2026-11-19T00:01:05Z", models: ["Synthetic model"], issue_snapshot: { title: "Historical task title" }, instructions: "Saved instructions", trigger_content: "Private trigger body", trigger_comment_id: graphId(40), output: "x".repeat(16000), output_truncated: true });
  const childRun = graphNode("run", 21, 11, "Child execution", { output: "Child result" });
  const comment = { ...graphNode("comment", 40, 10, "Synthetic comment", { source_run_id: graphId(20), comment_id: graphId(40), content: "Private comment body" }), run_id: graphId(20) };
  const attachment = { ...graphNode("artifact", 41, 10, "report.txt", { source_run_id: graphId(20), attachment_id: graphId(41), filename: "report.txt", content_type: "text/plain", comment_id: graphId(40) }), run_id: graphId(20) };
  const issues = [parent, child, other, ...Array.from({ length: Math.max(0, count - 3) }, (_, i) => graphNode("issue", 100 + i, 100 + i, `Window issue ${i}`, { project_id: graphId(30), project_title: "First project" }))].slice(0, count);
  const list = graph(issues);
  let failure = 0;
  const requests: { path: string; method: string; body: Record<string, unknown> | null; signal?: AbortSignal | null }[] = [];
  // The capability is tracked apart from `requests`, which records graph reads.
  let capability: unknown = { enabled: true };
  let capabilityReads = 0;
  const fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "https://synthetic.invalid").pathname;
    if (path === "/api/trajectory/capability") { capabilityReads += 1; return Response.json(capability); }
    const body = typeof init?.body === "string" ? JSON.parse(init.body) : null;
    requests.push({ path, method: init?.method ?? "GET", body, signal: init?.signal });
    if (failure) return new Response("SENSITIVE_SERVER_RESPONSE", { status: failure });
    if (path === "/api/issues/table/rows") {
      const matching = issues.filter(issue => !body.query?.search || issue.title.toLowerCase().includes(String(body.query.search).toLowerCase()));
      const offset = body.page?.cursor === "page-2" ? 100 : 0;
      const rows = matching.slice(offset, offset + 100).map((node, i) => ({ issue: {
        id: node.issue_id, workspace_id: graphId(2), number: i, identifier: node.details.issue_key ?? `SYN-${i}`, title: node.title,
        description: null, status: "todo", priority: "none", assignee_type: null, assignee_id: null, creator_type: "user", creator_id: graphId(1),
        parent_issue_id: node.details.parent_id ?? null, project_id: node.details.project_id ?? null, position: i, stage: null,
        start_date: null, due_date: null, metadata: {}, properties: {}, created_at: at, updated_at: at,
      }, direct_child_count: 0 }));
      return Response.json({ query_fingerprint: "synthetic", group_key: null, parent_id: null, total: matching.length, rows, branch_total: matching.length, next_cursor: offset + 100 < matching.length ? "page-2" : null });
    }
    if (path === "/api/issues/trajectory") return Response.json({ ...list, nodes: list.nodes.filter(node => body.issue_ids.includes(node.issue_id) || body.issue_ids.some((id: string) => list.nodes.find(item => item.issue_id === id)?.details.parent_id === node.issue_id)) });
    if (path === `/api/issues/${graphId(10)}/trajectory`) return Response.json({ ...graph([parent, run, comment, attachment]), edges: [{ id: "run-comment", from: run.id, to: comment.id, kind: "comment" }, { id: "run-attachment", from: run.id, to: attachment.id, kind: "artifact" }] });
    if (path === `/api/issues/${graphId(11)}/trajectory`) return Response.json(graph([child, childRun]));
    if (path.endsWith("/trajectory")) return Response.json(graph([]));
    if (path === `/api/tasks/${graphId(20)}/messages`) return Response.json([{ task_id: graphId(20), issue_id: graphId(10), seq: 7, type: "tool_use", tool: "Read", input: { path: "synthetic.txt" }, content: "Native message body" }]);
    throw new Error(`Unexpected synthetic request: ${path}`);
  };
  return {
    fetch, requests, list, run, comment, attachment,
    fail: (status: number) => { failure = status; },
    setCapability: (value: unknown) => { capability = value; },
    capabilityReads: () => capabilityReads,
  };
}
