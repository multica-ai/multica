import { describe, it, expect } from "vitest";
import { pickStageKeys } from "./task-status-pill";

describe("pickStageKeys", () => {
  it("returns queued when status is queued and agent is online", () => {
    expect(pickStageKeys("queued", [], "online")).toEqual({ stageKey: "queued" });
  });

  it("returns offline when status is queued and agent is offline", () => {
    expect(pickStageKeys("queued", [], "offline")).toEqual({
      stageKey: "offline",
      static: true,
    });
  });

  it("returns waiting_for_directory_release on the daemon-emitted hold status", () => {
    // Daemon publishes this when it dequeues a task but another task owns the
    // local_directory's lock. The pill becomes static (no shimmer) because
    // nothing is actively happening from the user's point of view.
    expect(pickStageKeys("waiting_for_directory_release", [], "online")).toEqual({
      stageKey: "waiting_for_directory_release",
      static: true,
    });
  });

  it("waiting_for_directory_release wins over availability hints", () => {
    // Even if availability says reconnecting/offline, the directory-release
    // status is the more specific signal — surface it.
    expect(
      pickStageKeys("waiting_for_directory_release", [], "unstable"),
    ).toEqual({ stageKey: "waiting_for_directory_release", static: true });
    expect(
      pickStageKeys("waiting_for_directory_release", [], "offline"),
    ).toEqual({ stageKey: "waiting_for_directory_release", static: true });
  });

  it("returns thinking for running with no messages", () => {
    expect(pickStageKeys("running", [], "online")).toEqual({ stageKey: "thinking" });
  });
});
