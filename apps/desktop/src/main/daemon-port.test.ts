// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

import { healthPortForProfile } from "./daemon-profile";
import { parseDaemonPortRecord, probeProfileHealth } from "./daemon-port";

const profile = "desktop-api.multica.ai";

describe("daemon port discovery", () => {
  it("accepts only a live matching profile and PID from daemon.port", async () => {
    const probe = vi.fn(async (port: number) =>
      port === 30001
        ? { status: "running", profile, pid: 42 }
        : null,
    );
    const health = await probeProfileHealth(profile, probe, async () => ({
      port: 30001,
      pid: 42,
    }));
    expect(health?.pid).toBe(42);
    expect(probe).toHaveBeenCalledTimes(1);
  });

  it.each([
    { status: "running", profile, pid: 99 },
    { status: "running", profile: "other", pid: 42 },
    { status: "stopped", profile, pid: 42 },
  ])("ignores a stale record and probes the legacy port: %o", async (stale) => {
    const fallback = { status: "running", profile, pid: 7 };
    const probe = vi.fn(async (port: number) =>
      port === 30001 ? stale : fallback,
    );
    expect(
      await probeProfileHealth(profile, probe, async () => ({ port: 30001, pid: 42 })),
    ).toEqual(fallback);
    expect(probe).toHaveBeenLastCalledWith(healthPortForProfile(profile));
  });

  it("reads a changed port for every poll", async () => {
    let port = 30001;
    const probe = vi.fn(async (candidate: number) =>
      candidate === port ? { status: "starting", profile, pid: 42 } : null,
    );
    const readRecord = async () => ({ port, pid: 42 });
    expect(await probeProfileHealth(profile, probe, readRecord)).not.toBeNull();
    port = 30002;
    expect(await probeProfileHealth(profile, probe, readRecord)).not.toBeNull();
    expect(probe.mock.calls.map(([candidate]) => candidate)).toEqual([30001, 30002]);
  });

  it("rejects malformed records", () => {
    for (const raw of ["", "null", "{}", '{"port":0,"pid":1}', '{"port":65536,"pid":1}', '{"port":30001,"pid":0}']) {
      expect(parseDaemonPortRecord(raw)).toBeNull();
    }
    expect(parseDaemonPortRecord('{"port":30001,"pid":42}')).toEqual({ port: 30001, pid: 42 });
  });
});
