import { describe, expect, it } from "vitest";
import type { DiagnosticsPayload } from "./daemon-diagnostics";
import { decideAutomaticRestart } from "./restart-decision";

const diagnostics: DiagnosticsPayload = {
  status: "running",
  os: "darwin",
  pid: 123,
  uptime: "1m",
  daemon_id: "daemon-1",
  device_name: "test",
  server_url: "https://example.test",
  cli_version: "v1.0.0",
  active_task_count: 0,
  agents: ["codex"],
  workspaces: [{ id: "workspace-1", runtimes: ["codex"] }],
};

describe("decideAutomaticRestart", () => {
  it("fails closed when daemon liveness cannot be proven", () => {
    expect(decideAutomaticRestart(null, null)).toBe("defer");
  });

  it("requires a running daemon and authenticated idle evidence", () => {
    expect(
      decideAutomaticRestart({ status: "starting", os: "darwin" }, diagnostics),
    ).toBe("defer");
    expect(
      decideAutomaticRestart({ status: "running", os: "darwin" }, null),
    ).toBe("defer");
    expect(
      decideAutomaticRestart(
        { status: "running", os: "darwin" },
        { ...diagnostics, active_task_count: 1 },
      ),
    ).toBe("defer");
  });

  it("allows an automatic restart only with explicit zero active tasks", () => {
    expect(
      decideAutomaticRestart(
        { status: "running", os: "darwin" },
        diagnostics,
      ),
    ).toBe("restart");
  });
});
