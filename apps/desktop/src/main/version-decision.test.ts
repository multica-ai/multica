import { describe, it, expect } from "vitest";
import { decideVersionAction } from "./version-decision";

describe("decideVersionAction", () => {
  it("returns not_running when health payload is null", () => {
    expect(decideVersionAction("v1.0.0", null)).toBe("not_running");
  });

  it("returns not_running when status is not 'running'", () => {
    expect(
      decideVersionAction("v1.0.0", { status: "stopped", cli_version: "v1.0.0" }),
    ).toBe("not_running");
  });

  it("returns ok when bundled version is unknown (fail safe)", () => {
    expect(
      decideVersionAction(null, {
        status: "running",
        cli_version: "v1.0.0",
        active_task_count: 0,
      }),
    ).toBe("ok");
  });

  it("returns ok when running daemon does not report cli_version (older daemon)", () => {
    expect(
      decideVersionAction("v1.0.0", {
        status: "running",
        active_task_count: 0,
      }),
    ).toBe("ok");
  });

  it("returns ok when versions match exactly", () => {
    expect(
      decideVersionAction("v1.2.3", {
        status: "running",
        cli_version: "v1.2.3",
        active_task_count: 5,
      }),
    ).toBe("ok");
  });

  it("returns restart when versions differ and daemon is idle", () => {
    expect(
      decideVersionAction("v1.2.3", {
        status: "running",
        cli_version: "v1.2.2",
        active_task_count: 0,
      }),
    ).toBe("restart");
  });

  it("treats missing active_task_count as 0 (old daemon that still reports cli_version)", () => {
    expect(
      decideVersionAction("v1.2.3", {
        status: "running",
        cli_version: "v1.2.2",
      }),
    ).toBe("restart");
  });

  it("returns defer when versions differ but daemon is busy", () => {
    expect(
      decideVersionAction("v1.2.3", {
        status: "running",
        cli_version: "v1.2.2",
        active_task_count: 2,
      }),
    ).toBe("defer");
  });

  it("transitions defer → restart as tasks drain", () => {
    // Same bundled version across three observations while the daemon ages.
    const bundled = "v2.0.0";
    const base = { status: "running", cli_version: "v1.9.0" } as const;

    expect(
      decideVersionAction(bundled, { ...base, active_task_count: 3 }),
    ).toBe("defer");
    expect(
      decideVersionAction(bundled, { ...base, active_task_count: 1 }),
    ).toBe("defer");
    expect(
      decideVersionAction(bundled, { ...base, active_task_count: 0 }),
    ).toBe("restart");
  });
});
