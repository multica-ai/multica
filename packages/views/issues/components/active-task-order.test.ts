// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { compareActiveIssueTasks } from "./active-task-order";

const task = (id: string, status: AgentTask["status"], priority = 0, created_at = "2026-09-08T03:00:00Z") =>
  ({ id, status, priority, created_at });
const ids = (tasks: ReturnType<typeof task>[]) => tasks.toSorted(compareActiveIssueTasks).map((t) => t.id);

describe("active issue task display order", () => {
  it("keeps running and already claimed tasks ahead of pending work", () => {
    expect(ids([task("queued", "queued", 10), task("parked", "waiting_local_directory"),
      task("dispatched", "dispatched"), task("running", "running")]))
      .toEqual(["running", "dispatched", "parked", "queued"]);
  });

  it("matches queue priority descending, enqueue time ascending, then task ID", () => {
    expect(ids([task("new", "queued", 0, "2026-09-08T04:00:00Z"),
      task("old-b", "queued"), task("urgent", "queued", 2, "2026-09-08T05:00:00Z"),
      task("old-a", "queued")])).toEqual(["urgent", "old-a", "old-b", "new"]);
  });

  it("compares timestamp instants instead of timezone strings", () => {
    expect(ids([task("later", "queued", 0, "2026-09-08T03:01:00Z"),
      task("earlier", "queued", 0, "2026-09-08T11:00:00+08:00")])).toEqual(["earlier", "later"]);
    expect(compareActiveIssueTasks(task("a", "queued", 0, "2026-09-08T11:00:00+08:00"), task("b", "queued"))).toBeLessThan(0);
  });
});
