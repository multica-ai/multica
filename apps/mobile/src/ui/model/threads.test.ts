import { describe, expect, it } from "vitest";
import type { AgentTask, TimelineEntry } from "@multica/core/types";
import { buildTimeline } from "./threads";

const comment = (id: string, at: string, parent: string | null = null, extra: Partial<TimelineEntry> = {}): TimelineEntry => ({
  type: "comment",
  id,
  actor_type: "member",
  actor_id: "u1",
  created_at: at,
  content: id,
  parent_id: parent,
  ...extra,
});
const activity = (id: string, at: string): TimelineEntry => ({ type: "activity", id, actor_type: "member", actor_id: "u1", created_at: at, action: "created" });
const task = (id: string, at: string, status: AgentTask["status"], trigger?: string): AgentTask => ({ id, created_at: at, status, trigger_comment_id: trigger }) as AgentTask;

describe("buildTimeline", () => {
  it("nests replies, replies to replies included, under their thread in order", () => {
    const nodes = buildTimeline([activity("a", "1"), comment("c1", "2"), comment("c2", "3"), comment("r2", "5", "r1"), comment("r1", "4", "c1")], []);
    expect(nodes.map((node) => node.kind)).toEqual(["activity", "thread", "thread"]);
    expect(nodes[1]).toMatchObject({ root: { id: "c1" }, replies: [{ id: "r1" }, { id: "r2" }] });
  });

  it("keeps a reply whose parent is gone as its own thread", () => {
    expect(buildTimeline([comment("r", "1", "missing")], [])).toMatchObject([{ kind: "thread", root: { id: "r" } }]);
  });

  it("puts a run in the thread of the comment that started it", () => {
    const nodes = buildTimeline([comment("c1", "1"), comment("r1", "2", "c1")], [task("t1", "3", "running", "r1")]);
    expect(nodes).toMatchObject([{ kind: "thread", run: { id: "t1" } }]);
  });

  it("shows a thread's latest run only while it runs or after it failed", () => {
    const entries = [comment("c1", "1")];
    expect(buildTimeline(entries, [task("t1", "2", "failed", "c1"), task("t2", "3", "completed", "c1")])[0]).toMatchObject({ run: null });
    expect(buildTimeline(entries, [task("t1", "2", "completed", "c1"), task("t2", "3", "failed", "c1")])[0]).toMatchObject({ run: { id: "t2" } });
  });

  it("places other runs in time", () => {
    const nodes = buildTimeline([activity("a", "1"), comment("c1", "5")], [task("t1", "3", "queued"), task("t2", "4", "completed"), task("t3", "9", "failed")]);
    expect(nodes.map((node) => (node.kind === "run" ? node.task.id : node.kind))).toEqual(["activity", "t1", "thread", "t3"]);
  });

  it("marks a thread resolved when any of its comments is", () => {
    const nodes = buildTimeline([comment("c1", "1"), comment("r1", "2", "c1", { resolved_at: "3" })], []);
    expect(nodes[0]).toMatchObject({ resolved: true });
  });
});
