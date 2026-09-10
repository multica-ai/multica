// @vitest-environment node
import { describe, it, expect } from "vitest";
import { effectiveTaskStatus, pickStageKeys } from "./task-status-pill";

describe("pickStageKeys", () => {
  it("returns retrying while a deferred chat retry waits for backoff", () => {
    expect(pickStageKeys("deferred", [], "offline")).toEqual({ stageKey: "retrying" });
  });

  it("keeps deferred authoritative over task messages from the earlier attempt", () => {
    expect(
      effectiveTaskStatus("deferred", [
        {
          task_id: "task-1",
          issue_id: "",
          seq: 1,
          type: "thinking",
          created_at: "2026-07-01T00:00:00Z",
        },
      ]),
    ).toBe("deferred");
  });

  it("returns queued when status is queued and agent is online", () => {
    expect(pickStageKeys("queued", [], "online")).toEqual({ stageKey: "queued" });
  });

  it("returns offline when status is queued and agent is offline", () => {
    expect(pickStageKeys("queued", [], "offline")).toEqual({
      stageKey: "offline",
      static: true,
    });
  });

  it("returns waiting_local_directory on the daemon-emitted hold status", () => {
    // Daemon publishes this when it dequeues a task but another task owns the
    // local_directory's lock. The pill becomes static (no shimmer) because
    // nothing is actively happening from the user's point of view.
    expect(pickStageKeys("waiting_local_directory", [], "online")).toEqual({
      needsWaitReason: true,
      stageKey: "waiting_local_directory",
      static: true,
    });
  });

  it("waiting_local_directory wins over availability hints", () => {
    // Even if availability says reconnecting/offline, the directory-release
    // status is the more specific signal — surface it.
    expect(
      pickStageKeys("waiting_local_directory", [], "unstable"),
    ).toEqual({
      stageKey: "waiting_local_directory",
      static: true,
      needsWaitReason: true,
    });
    expect(
      pickStageKeys("waiting_local_directory", [], "offline"),
    ).toEqual({
      stageKey: "waiting_local_directory",
      static: true,
      needsWaitReason: true,
    });
  });

  it("returns thinking for running with no messages", () => {
    expect(pickStageKeys("running", [], "online")).toEqual({ stageKey: "thinking" });
  });
});

describe("pickStageKeys wait reason", () => {
  it("flags the hold status as wanting a reason", () => {
    // The flag is what lets the renderer choose the explaining label; every
    // other stage leaves it unset so no other status can pick up stale text.
    expect(
      pickStageKeys("waiting_local_directory", [], "online").needsWaitReason,
    ).toBe(true);
    for (const status of ["queued", "dispatched", "running", "deferred"]) {
      expect(pickStageKeys(status, [], "online").needsWaitReason).toBeUndefined();
    }
  });
});
