// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentTask, TimelineEntry } from "@multica/core/types";
import { CompletionResultSchema } from "@multica/core/api/schemas";
import { commentRunOutput, buildCommentRunView } from "./comment-runs";

const groupCommentRuns = (...args: Parameters<typeof buildCommentRunView>) => buildCommentRunView(...args).runs;

function task(id: string, overrides: Partial<AgentTask> = {}): AgentTask {
  return { id, agent_id: "agent", runtime_id: "runtime", issue_id: "issue", status: "running", priority: 0,
    created_at: "2026-09-07T00:00:00Z", started_at: null, dispatched_at: null, completed_at: null, result: null, error: null, ...overrides };
}
function comment(id: string, overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return { id, type: "comment", actor_type: "member", actor_id: "user", created_at: "2026-09-07T00:00:00Z", ...overrides };
}

describe("groupCommentRuns", () => {
  it.each(["queued", "dispatched", "running", "completed"] as const)("waits for a missing trigger before placing a %s run and its reply", (status) => {
    const run = task("run", { status, trigger_comment_id: "trigger",
      delivered_comment_ids: status === "queued" || status === "dispatched" ? [] : ["trigger"] });
    const root = comment("root");
    const trigger = comment("trigger", { parent_id: root.id });
    const before = buildCommentRunView([run], [root]);
    expect(before.standaloneRuns).toEqual([]);
    expect(before.runs.size).toBe(0);
    const reply = comment("answer", { actor_type: "agent", source_task_id: run.id });
    const earlyReply = buildCommentRunView([run], [root, reply]);
    expect(earlyReply.standaloneRuns).toEqual([]);
    expect(earlyReply.timeline.find((entry) => entry.id === reply.id)?.parent_id).toBe(trigger.id);
    const after = buildCommentRunView([run], [root, trigger, reply]);
    expect(after.standaloneRuns).toEqual([]);
    expect(after.runs.get(root.id)).toEqual([{ task: run, anchorCommentId: trigger.id, commentId: reply.id, hasReply: true }]);
  });

  it("waits for the intended merged trigger and preserves retry ancestry", () => {
    const original = task("original", { status: "queued", trigger_comment_id: "new", coalesced_comment_ids: ["old"], delivered_comment_ids: [] });
    const retry = task("retry", { status: "queued", parent_task_id: original.id, delivered_comment_ids: [] });
    const old = comment("old");
    const next = comment("new", { parent_id: old.id });
    const before = buildCommentRunView([original, retry], [old]);
    expect(before.standaloneRuns).toEqual([]);
    expect(before.runs.size).toBe(0);
    const after = buildCommentRunView([original, retry], [old, next]);
    expect(after.runs.get(old.id)?.map((run) => run.anchorCommentId)).toEqual([next.id, next.id]);
    expect(after.standaloneRuns).toEqual([]);
  });

  it("moves a queued run after the latest instruction and keeps the answer there", () => {
    const root = comment("root");
    const first = comment("first", { parent_id: root.id });
    const other = comment("other-thread");
    const second = comment("second", { parent_id: root.id, created_at: "2026-09-07T00:01:00Z" });
    const run = task("run", { status: "queued", trigger_comment_id: first.id });
    const before = buildCommentRunView([run], [root, first, other]);
    const merged = { ...run, trigger_comment_id: second.id, coalesced_comment_ids: [first.id] };
    const awaitingComment = buildCommentRunView([merged], [root, first, other], before.runs);
    expect(awaitingComment.runs.get(root.id)?.[0]?.anchorCommentId).toBe(first.id);
    const after = buildCommentRunView([merged], [root, first, other, second], before.runs);
    expect(after.runs.get(root.id)?.[0]?.anchorCommentId).toBe(second.id);
    expect(after.runs.has(other.id)).toBe(false);
    const answer = comment("answer", { actor_type: "agent", source_task_id: run.id });
    const complete = buildCommentRunView([{ ...merged, status: "completed", delivered_comment_ids: [first.id, second.id] }], [root, first, other, second, answer]);
    expect(complete.runs.get(root.id)?.[0]).toMatchObject({ anchorCommentId: second.id, commentId: answer.id, hasReply: true });
  });

  it.each([
    { status: "queued" as const, deliveredCommentIds: [] },
    { status: "running" as const, deliveredCommentIds: ["first", "latest"] },
  ])("falls back to the latest remaining input when a $status batch anchor is deleted", ({ status, deliveredCommentIds }) => {
    const first = comment("first");
    const latest = comment("latest", { parent_id: first.id, created_at: "2026-09-07T00:01:00Z" });
    const run = task("run", {
      status,
      trigger_comment_id: latest.id,
      coalesced_comment_ids: [first.id],
      delivered_comment_ids: deliveredCommentIds,
    });
    const beforeDelete = buildCommentRunView([run], [first, latest]);
    expect(beforeDelete.runs.get(first.id)?.[0]?.anchorCommentId).toBe(latest.id);

    const afterDelete = buildCommentRunView([run], [first], beforeDelete.runs);
    expect(afterDelete.runs.get(first.id)?.[0])
      .toMatchObject({ anchorCommentId: first.id, commentId: first.id });
    expect(afterDelete.standaloneRuns).toEqual([]);
  });

  it("projects a run reply onto the latest remaining input when its deleted anchor disappears", () => {
    const first = comment("first");
    const latest = comment("latest", { parent_id: first.id, created_at: "2026-09-07T00:01:00Z" });
    const run = task("run", {
      trigger_comment_id: latest.id,
      coalesced_comment_ids: [first.id],
      delivered_comment_ids: [first.id, latest.id],
    });
    const beforeDelete = buildCommentRunView([run], [first, latest]);
    const answer = comment("answer", { actor_type: "agent", source_task_id: run.id });

    const afterDelete = buildCommentRunView([run], [first, answer], beforeDelete.runs);
    expect(afterDelete.runs.get(first.id)?.[0])
      .toMatchObject({ anchorCommentId: first.id, commentId: answer.id, hasReply: true });
    expect(afterDelete.timeline.find((entry) => entry.id === answer.id)?.parent_id).toBe(first.id);
  });

  it("uses timeline order to break ties between same-time inputs", () => {
    const root = comment("root");
    const first = comment("first", { parent_id: root.id });
    const latest = comment("latest", { parent_id: root.id });
    const run = task("run", {
      status: "queued",
      trigger_comment_id: latest.id,
      coalesced_comment_ids: [first.id],
      delivered_comment_ids: [],
    });

    expect(buildCommentRunView([run], [root, first, latest]).runs.get(root.id)?.[0]?.anchorCommentId)
      .toBe(latest.id);
  });

  it("projects chained answers and assignment subtrees before finding run roots", () => {
    const first = task("first", { trigger_comment_id: "root" });
    const second = task("second", { trigger_comment_id: "answer-a" });
    const assigned = task("assigned");
    const followup = task("followup", { trigger_comment_id: "nested" });
    const timeline = [comment("root"),
      comment("answer-a", { actor_type: "agent", source_task_id: first.id }),
      comment("answer-b", { actor_type: "agent", source_task_id: second.id }),
      comment("assigned-answer", { parent_id: "root", actor_type: "agent", source_task_id: assigned.id }),
      comment("nested", { parent_id: "assigned-answer" })];
    const view = buildCommentRunView([followup, second, assigned, first], timeline);
    expect(view.runs.get("root")?.map((run) => run.task.id)).toEqual(["first", "second"]);
    expect(view.runs.get("assigned-answer")?.map((run) => run.task.id)).toEqual(["assigned", "followup"]);
    expect(view.timeline.find((entry) => entry.id === "answer-a")?.parent_id).toBe("root");
    expect(view.timeline.find((entry) => entry.id === "answer-b")?.parent_id).toBe("answer-a");
    expect(view.timeline.find((entry) => entry.id === "assigned-answer")?.parent_id).toBeUndefined();
    expect(timeline.find((entry) => entry.id === "assigned-answer")?.parent_id).toBe("root");
  });

  it("does not project reply relationships that would create a comment cycle", () => {
    const first = task("first", { trigger_comment_id: "b" });
    const second = task("second", { trigger_comment_id: "a" });
    const timeline = [comment("a", { actor_type: "agent", source_task_id: first.id }),
      comment("b", { actor_type: "agent", source_task_id: second.id })];
    expect(buildCommentRunView([first, second], timeline).timeline).toEqual(timeline);
  });
  it("replaces invalidated queued runs without adding cancelled comment blocks", () => {
    const old = task("old", { status: "cancelled", trigger_comment_id: "root", cancelled_by_comment_change: true });
    const next = task("new", { status: "queued", trigger_comment_id: "root" });
    const later = task("later", { status: "queued", trigger_comment_id: "followup" });
    const tasks = [old, { ...old, id: "second-edit" }, next, later];
    const timeline = [comment("root"), comment("followup", { parent_id: "root" })];
    const grouped = groupCommentRuns(tasks, timeline);
    expect(grouped.get("root")?.map((run) => run.task.id)).toEqual(["later", "new"]);
    expect(buildCommentRunView(tasks, timeline).standaloneRuns).toEqual([]);
    expect(tasks).toHaveLength(4); // Full execution history remains intact.
    const reply = comment("answer", { actor_type: "agent", source_task_id: next.id });
    expect(groupCommentRuns(tasks, [...timeline, reply]).get("root")?.find((run) => run.task.id === next.id))
      .toMatchObject({ anchorCommentId: "root", commentId: "answer", hasReply: true });
  });

  it.each([
    { cancelled_by_comment_change: undefined },
    { cancelled_by_comment_change: false },
    { dispatched_at: "2026-09-07T00:00:01Z" },
    { started_at: "2026-09-07T00:00:01Z" },
    { delivered_comment_ids: ["root"] },
    { status: "failed" as const },
  ])("retains manual cancellation and actual execution: %j", (overrides) => {
    const run = task("old", { status: "cancelled", trigger_comment_id: "root", cancelled_by_comment_change: true, ...overrides });
    expect(groupCommentRuns([run], [comment("root")]).get("root")?.[0]?.task.id).toBe(run.id);
  });

  it("keeps actual replies even when cancellation metadata says the input changed", () => {
    const run = task("old", { status: "cancelled", cancelled_by_comment_change: true });
    const reply = comment("answer", { actor_type: "agent", source_task_id: run.id });
    expect(buildCommentRunView([run], [reply]).standaloneRuns).toHaveLength(1);
    expect(buildCommentRunView([run], []).standaloneRuns).toEqual([]);
  });
  it("keeps merged runs after their latest delivered input, including nested replies", () => {
    const timeline = [comment("root"), comment("reply", { parent_id: "root" }), comment("nested", { parent_id: "reply" })];
    const run = task("run", { trigger_comment_id: "nested", coalesced_comment_ids: ["root", "reply"], delivered_comment_ids: ["root", "reply", "nested"] });
    expect([...groupCommentRuns([run], timeline)]).toEqual([["root", [{ task: run, commentId: "nested", anchorCommentId: "nested", hasReply: false }]]]);
  });

  it("freezes a claimed run after the latest comment in its delivery receipt", () => {
    const timeline = [
      comment("root"),
      comment("first", { parent_id: "root", created_at: "2026-09-07T00:01:00Z" }),
      comment("second", { parent_id: "root", created_at: "2026-09-07T00:02:00Z" }),
      comment("next-run", { parent_id: "root", created_at: "2026-09-07T00:03:00Z" }),
    ];
    const run = task("run", {
      status: "running",
      trigger_comment_id: "next-run",
      coalesced_comment_ids: ["first", "second"],
      delivered_comment_ids: ["first", "second"],
    });
    expect(groupCommentRuns([run], timeline).get("root")?.[0])
      .toMatchObject({ anchorCommentId: "second", commentId: "second" });
  });

  it.each([{ receipt: [] }, { receipt: ["old"] }])("uses planned comment coverage while queued, even with a delivery receipt of $receipt", ({ receipt }) => {
    const run = task("queued", {
      status: "queued", trigger_comment_id: "new", coalesced_comment_ids: ["old"],
      delivered_comment_ids: receipt,
    });
    const timeline = [comment("old"), comment("new")];
    expect(groupCommentRuns([run], timeline).get("new")).toEqual([
      { task: run, commentId: "new", anchorCommentId: "new", hasReply: false },
    ]);
  });

  it("keeps the trigger position while binding the latest persisted reply", () => {
    const run = task("run", { status: "completed", trigger_comment_id: "root" });
    const timeline = [comment("root"),
      comment("progress", { parent_id: "root", actor_type: "agent", source_task_id: run.id }),
      comment("final", { parent_id: "root", actor_type: "agent", source_task_id: run.id, created_at: "2026-09-07T00:01:00Z" }),
      comment("unrelated", { actor_type: "agent", source_task_id: "other" })];
    const map = groupCommentRuns([run], timeline);
    expect(map.get("root")).toEqual([{ task: run, commentId: "final", anchorCommentId: "root", hasReply: true }]);
    expect(map.size).toBe(1);
  });

  it.each(["cancelled", "failed"] as const)("preserves the planned anchor after a queued run is %s before dispatch", (status) => {
    const run = task("stopped", {
      status, trigger_comment_id: "new", coalesced_comment_ids: ["old"],
      delivered_comment_ids: [], completed_at: "2026-09-07T00:01:00Z",
    });
    const retry = task("retry", { status: "queued", parent_task_id: run.id, delivered_comment_ids: [] });
    expect(groupCommentRuns([run, retry], [comment("old"), comment("new")]).get("new")?.map((row) => row.task.id))
      .toEqual(["retry", "stopped"]);
    expect(groupCommentRuns([{ ...run, dispatched_at: "2026-09-07T00:00:30Z" }], [comment("new")]).size).toBe(0);
  });

  it("uses authoritative delivered comments when the newest trigger was not delivered", () => {
    const run = task("run", { trigger_comment_id: "new", coalesced_comment_ids: ["old"], delivered_comment_ids: ["old"] });
    expect(groupCommentRuns([run], [comment("old"), comment("new")]).get("old")?.[0]?.commentId).toBe("old");
    expect(groupCommentRuns([{ ...run, delivered_comment_ids: [] }], [comment("old"), comment("new")]).size).toBe(0);
  });

  it("keeps retries on their original trigger and ignores missing or cyclic ancestry", () => {
    const original = task("original", { trigger_comment_id: "root" });
    const retry = task("retry", { parent_task_id: original.id });
    expect(groupCommentRuns([retry, original], [comment("root")]).get("root")?.map((row) => row.task.id)).toEqual(["original", "retry"]);
    expect(groupCommentRuns([task("a", { parent_task_id: "b" }), task("b", { parent_task_id: "a" })], []).size).toBe(0);
    expect(groupCommentRuns([task("deleted", { trigger_comment_id: "missing" })], []).size).toBe(0);
  });

  it("associates assignment runs with their own replies and preserves unrelated thread references", () => {
    const run = task("assigned");
    const timeline = [comment("delivery", { actor_type: "agent", source_task_id: run.id })];
    const before = groupCommentRuns([run], timeline);
    const after = groupCommentRuns([run, task("other", { trigger_comment_id: "other" })], [...timeline, comment("other")], before);
    expect(after.get("delivery")).toBe(before.get("delivery"));
  });
});

describe("commentRunOutput", () => {
  it("only exposes the completed daemon deliverable and tolerates response drift", () => {
    expect(commentRunOutput(task("r", { status: "completed", result: { version: 1, summary: "Done", artifact_ids: [] } }))).toBe("Done");
    for (const result of [null, "Done", {}, { version: 1, summary: 1 }, { version: 1, summary: " " }]) {
      expect(commentRunOutput(task("r", { status: "completed", result: CompletionResultSchema.parse(result) }))).toBeNull();
    }
    expect(commentRunOutput(task("r", { result: { version: 1, summary: "Still working", artifact_ids: [] } }))).toBeNull();
  });

  it("uses the API-normalized summary for a legacy server result", () => {
    const run = task("r", { status: "completed", result: CompletionResultSchema.parse({ output: "Legacy answer" }) });
    expect(commentRunOutput(run)).toBe("Legacy answer");
  });
});

describe("standaloneCommentRuns", () => {
  it.each(["queued", "dispatched", "running", "failed", "cancelled", "completed"] as const)("keeps an unanchored %s run visible", (status) => {
    const run = task("assignment", { status, delivered_comment_ids: [] });
    expect(buildCommentRunView([run], []).standaloneRuns)
      .toEqual([{ task: run, hasReply: false }]);
  });

  it("omits the issue-producing quick-create run from Activity", () => {
    const quickCreate = task("quick-create", { kind: "quick_create", status: "completed" });
    const assignment = task("assignment", { kind: "direct", status: "completed" });
    expect(buildCommentRunView([quickCreate, assignment], []).standaloneRuns)
      .toEqual([{ task: assignment, hasReply: false }]);
  });

  it("keeps assignment slots after replies arrive and excludes comment-triggered slots", () => {
    const assigned = task("assignment");
    const triggered = task("comment-run", { trigger_comment_id: "trigger" });
    const tasks = [assigned, triggered];
    const timeline = [comment("trigger")];
    expect(buildCommentRunView(tasks, timeline).standaloneRuns.map((run) => run.task.id)).toEqual([assigned.id]);
    const reply = comment("answer", { actor_type: "agent", source_task_id: assigned.id });
    expect(buildCommentRunView(tasks, [...timeline, reply]).standaloneRuns)
      .toEqual([{ task: assigned, commentId: reply.id, anchorCommentId: undefined, hasReply: true }]);
  });
});
