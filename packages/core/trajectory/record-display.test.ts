// @vitest-environment node
import { expect, it } from "vitest";
import { emptyTrajectory, sanitizeTrajectoryGraph, type TrajectoryNode } from "./graph";
import { latestLoadedRecord, loadedRunPositions, runConversationTarget } from "./record-display";

const node = (id: string, status: string, created_at: string, issue_id = "owner"): TrajectoryNode => ({
  id: "run:" + id, run_id: id, kind: "run", issue_id, status, created_at, title: "Returned purpose", details: {},
});
it("finds the newest of 22 loaded records by creation time", () => {
  const nodes = Array.from({ length: 22 }, (_, n) => node(String(n), n % 2 ? "cancelled" : "completed", new Date(Date.UTC(2026, 0, 1, 0, n)).toISOString()));
  expect(latestLoadedRecord([...nodes].reverse())?.id).toBe("run:21");
});
it("ignores unknown timestamps and returns nothing for empty or unknown facts", () => {
  expect(latestLoadedRecord([node("a", "running", "2026-01-02"), node("b", "completed", "bad"), node("c", "running", "2026-01-01")])?.id).toBe("run:a");
  expect(latestLoadedRecord([])).toBeUndefined();
  expect(latestLoadedRecord([node("x", "blocked", "")])).toBeUndefined();
});
it("positions the latest of 22 terminal Runs without claiming any is executing", () => {
  const graph = { ...emptyTrajectory("owner"), nodes: Array.from({ length: 22 }, (_, n) => node(String(n), n % 2 ? "cancelled" : "completed", new Date(Date.UTC(2026, 0, 1, 0, n)).toISOString())) };
  expect(loadedRunPositions(graph, "owner")).toEqual({ runningIds: [], latestId: "run:21" });
});
it("keeps every parallel native running Run, ignores other owners and unknown timestamps", () => {
  const graph = { ...emptyTrajectory("owner"), nodes: [
    node("a", "running", "2026-01-02"), node("b", "running", "2026-01-01"),
    node("c", "dispatched", "2026-01-03"), node("d", "running", "bad", "another"), node("e", "completed", "bad"),
  ] };
  expect(loadedRunPositions(graph, "owner")).toEqual({ runningIds: ["run:b", "run:a"], latestId: "run:c" });
});
it("does not manufacture a latest or current Run from empty or unknown facts", () => {
  expect(loadedRunPositions(emptyTrajectory("owner"), "owner")).toEqual({ runningIds: [], latestId: null });
  expect(loadedRunPositions({ ...emptyTrajectory("owner"), nodes: [node("x", "blocked", "")] }, "owner")).toEqual({ runningIds: [], latestId: null });
});
it("removes stored trigger-purpose aliases for a denied comment while retaining allowed Run facts", () => {
  const run = { ...node("x", "completed", "2026-01-01"), details: { trigger_comment_id: "denied", trigger_summary: "Denied purpose", triggerSummary: "Denied alias", instructions: "Allowed instructions", output: "Allowed result" } };
  const cleaned = sanitizeTrajectoryGraph({ ...emptyTrajectory("owner"), nodes: [run] }, new Set(["comment:denied"]));
  expect(cleaned.nodes[0]!.details).not.toHaveProperty("trigger_summary");
  expect(cleaned.nodes[0]!.details).not.toHaveProperty("triggerSummary");
  expect(cleaned.nodes[0]!.details.instructions).toBe("Allowed instructions");
  expect(cleaned.nodes[0]!.details.output).toBe("Allowed result");
});

const issueNode = (issue_id: string, key: string): TrajectoryNode => ({ id: `issue:${issue_id}`, kind: "issue", issue_id, status: "todo", created_at: "", title: key, details: { issue_key: key } });
const commentNode = (id: string, issue_id: string, created_at: string, run_id?: string): TrajectoryNode => ({ id: `comment:${id}`, kind: "comment", issue_id, status: "recorded", created_at, title: "comment", ...(run_id ? { run_id } : {}), details: { comment_id: id } });
const conversationRun = (details: Record<string, unknown> = {}): TrajectoryNode => ({ ...node("r", "completed", "2026-01-01T00:00:00Z"), details });
const withNodes = (...nodes: TrajectoryNode[]) => ({ ...emptyTrajectory("owner"), nodes });
it("prefers the run's latest visible reply over its trigger", () => {
  const graph = withNodes(issueNode("owner", "REL-1"), commentNode("t", "owner", "2026-01-01T00:00:00Z"), commentNode("a", "owner", "2026-01-01T00:01:00Z", "r"), commentNode("b", "owner", "2026-01-01T00:02:00Z", "r"));
  expect(runConversationTarget(graph, conversationRun({ trigger_comment_id: "t" }))).toEqual({ relation: "reply", commentId: "b", issueId: "owner", identifier: "REL-1" });
});
it("falls back to a visible trigger comment, including comment trigger evidence on another issue", () => {
  const graph = withNodes(issueNode("other", "REL-2"), commentNode("e", "other", "2026-01-01T00:00:00Z"));
  expect(runConversationTarget(graph, conversationRun({ trigger_evidence_kind: "comment", trigger_evidence_ref_id: "e" }))).toEqual({ relation: "trigger", commentId: "e", issueId: "other", identifier: "REL-2" });
});
it("offers no conversation when neither a reply nor the trigger is visible", () => {
  const graph = withNodes(commentNode("unrelated", "owner", "2026-01-01T00:00:00Z"));
  expect(runConversationTarget(graph, conversationRun({ trigger_comment_id: "deleted" }))).toBeNull();
  const denied = sanitizeTrajectoryGraph(withNodes(commentNode("t", "owner", "")), new Set(["comment:t"]));
  expect(runConversationTarget(denied, conversationRun({ trigger_comment_id: "t" }))).toBeNull();
});
it("keeps the identifier unknown when the comment's issue is not loaded", () => {
  expect(runConversationTarget(withNodes(commentNode("a", "elsewhere", "", "r")), conversationRun())).toMatchObject({ issueId: "elsewhere", identifier: null });
});
