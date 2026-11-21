// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentTask, TimelineEntry } from "@multica/core/types";
import type { CommentRun } from "../components/comment-runs";
import { buildTimelineFindUnits } from "./timeline-find-units";
import type { TimelineFolds, TimelineRowItem } from "./timeline-row-content";

function task(id: string, overrides: Partial<AgentTask> = {}): AgentTask {
  return { id, agent_id: "agent", runtime_id: "runtime", issue_id: "issue", status: "completed", priority: 0,
    created_at: "2026-09-07T00:00:00Z", started_at: "2026-09-07T00:00:00Z", dispatched_at: null,
    completed_at: "2026-09-07T00:00:00Z", result: null, error: null, ...overrides };
}
function comment(id: string, content: string, overrides: Partial<TimelineEntry> = {}): TimelineEntry {
  return { id, type: "comment", actor_type: "member", actor_id: "user", created_at: "2026-09-07T00:00:00Z", content, ...overrides };
}
const at = (minute: number) => `2026-09-07T00:0${minute}:00Z`;
const OPEN: TimelineFolds = { expandedResolved: new Set(), collapsed: new Set() };
const keys = (units: { key: string }[]) => units.map((unit) => unit.key);

describe("buildTimelineFindUnits", () => {
  const root = comment("root", "Root **question**");
  const reply = comment("reply", "A reply", { parent_id: root.id, created_at: at(1) });
  const answer = comment("answer", "Agent answer", { parent_id: reply.id, actor_type: "agent", source_task_id: "run-1", created_at: at(2) });
  const deleted = comment("deleted", "gone", { parent_id: root.id, created_at: at(3), deleted_at: at(4) });
  const answered: CommentRun = { task: task("run-1", { completed_at: at(2) }), commentId: answer.id, anchorCommentId: reply.id, hasReply: true };
  const silent: CommentRun = {
    task: task("run-2", { completed_at: at(5), result: { comment: "Output with `code`" } }),
    anchorCommentId: root.id, hasReply: false,
  };
  const standalone: CommentRun = { task: task("run-3", { result: { comment: "Standalone output" } }), hasReply: false };
  const replies = new Map([[root.id, [reply, answer, deleted]]]);
  const runs = new Map([[root.id, [answered, silent]]]);

  it("lists every comment and run output the timeline shows, in reading order", () => {
    const items: TimelineRowItem[] = [
      { kind: "comment", id: root.id, entry: root },
      { kind: "activity-group", id: "activity" },
      { kind: "run", id: standalone.task.id, run: standalone },
    ];

    const units = buildTimelineFindUnits(items, replies, runs, OPEN);

    expect(units.map(({ key, itemId, anchorId }) => ({ key, itemId, anchorId }))).toEqual([
      { key: "root", itemId: "root", anchorId: "root" },
      { key: "reply", itemId: "root", anchorId: "reply" },
      { key: "answer", itemId: "root", anchorId: "answer" },
      { key: "run:run-2", itemId: "root", anchorId: "run-2" },
      { key: "run:run-3", itemId: "run-3", anchorId: "run-3" },
    ]);
    expect(units[0]!.text).toBe("root \nquestion\n");
    expect(units[3]!.text).toBe("output with \ncode\n");
  });

  it("leaves out what a fold hides, as the rendered timeline does", () => {
    // A thread the reader collapsed shows only its header.
    expect(buildTimelineFindUnits([{ kind: "comment", id: root.id, entry: root }], replies, runs,
      { ...OPEN, collapsed: new Set([root.id]) })).toEqual([]);
    // A resolved thread folded into its bar shows none of its comments.
    const resolved = { ...root, resolved_at: at(9) };
    expect(buildTimelineFindUnits([{ kind: "resolved-bar", id: root.id, entry: resolved }], replies, runs, OPEN)).toEqual([]);
    // A reply that resolved the thread folds the other replies behind a bar.
    const resolution = { ...reply, resolved_at: at(9) };
    const folded = new Map([[root.id, [resolution, answer]]]);
    const thread: TimelineRowItem[] = [{ kind: "comment", id: root.id, entry: root }];
    expect(keys(buildTimelineFindUnits(thread, folded, new Map(), OPEN))).toEqual(["root", "reply"]);
    expect(keys(buildTimelineFindUnits(thread, folded, new Map(),
      { ...OPEN, expandedResolved: new Set([root.id]) }))).toEqual(["root", "reply", "answer"]);
  });

  it("leaves out a failure notice, which renders as its run's block", () => {
    const failed = task("run-1", { status: "failed" });
    const notice = comment("notice", "agent exited with status 1", {
      actor_type: "agent", comment_type: "system", source_task_id: failed.id,
    });
    const run: CommentRun = { task: failed, commentId: notice.id, hasReply: true };

    expect(buildTimelineFindUnits([{ kind: "run", id: notice.id, run, entry: notice }], new Map(), new Map(), OPEN)).toEqual([]);
  });
});
