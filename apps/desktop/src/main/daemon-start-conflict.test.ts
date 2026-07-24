import { describe, expect, it } from "vitest";
import { legacyDaemonConflict } from "./daemon-start-conflict";

describe("legacyDaemonConflict", () => {
  it("blocks profiled Desktop startup beside a legacy daemon with the same identity", () => {
    expect(
      legacyDaemonConflict("machine-1", "https://api.multica.ai", {
        status: "running",
        daemon_id: "machine-1",
        server_url: "https://api.multica.ai/",
        pid: 14683,
        active_task_count: 1,
      }),
    ).toEqual({ pid: 14683, activeTaskCount: 1 });
  });

  it("also blocks a matching daemon that is still starting", () => {
    expect(
      legacyDaemonConflict("machine-1", "https://api.multica.ai", {
        status: "starting",
        daemon_id: "machine-1",
        server_url: "https://api.multica.ai",
      }),
    ).toEqual({ pid: undefined, activeTaskCount: 0 });
  });

  it.each([
    ["reused port with another daemon", "machine-2", "https://api.multica.ai"],
    ["same machine on another backend", "machine-1", "https://staging.multica.ai"],
  ])("ignores %s", (_name, daemonId, serverUrl) => {
    expect(
      legacyDaemonConflict("machine-1", "https://api.multica.ai", {
        status: "running",
        daemon_id: daemonId,
        server_url: serverUrl,
        pid: 42,
      }),
    ).toBeNull();
  });

  it("fails open when identity cannot be confirmed", () => {
    expect(
      legacyDaemonConflict(null, "https://api.multica.ai", {
        status: "running",
        daemon_id: "machine-1",
        server_url: "https://api.multica.ai",
      }),
    ).toBeNull();
  });
});
