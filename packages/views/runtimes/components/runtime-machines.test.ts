import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "@multica/core/types";
import {
  buildRuntimeMachines,
  filterRuntimeMachines,
  runtimeMachineCounts,
  splitRuntimeName,
} from "./runtime-machines";

const NOW = new Date("2026-05-17T12:00:00Z").getTime();

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (dev-machine.local)",
    display_name: null,
    machine_alias: null,
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "dev-machine.local · claude 1.0.0",
    metadata: { cli_version: "0.3.0" },
    owner_id: "user-1",
    visibility: "private",
    timezone: "UTC",
    last_seen_at: new Date(NOW - 10_000).toISOString(),
    created_at: "2026-05-17T11:00:00Z",
    updated_at: "2026-05-17T11:00:00Z",
    ...overrides,
  };
}

describe("runtime machine grouping", () => {
  it("groups multiple provider runtimes by daemon id", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({ id: "rt-claude", provider: "claude", name: "Claude (dev.local)" }),
        makeRuntime({ id: "rt-codex", provider: "codex", name: "Codex (dev.local)" }),
      ],
      { now: NOW, localDaemonId: "daemon-1" },
    );

    expect(machines).toHaveLength(1);
    expect(machines[0]).toMatchObject({
      id: "local:daemon-1",
      title: "dev.local",
      section: "local",
      isCurrent: true,
      onlineCount: 2,
      issueCount: 0,
      providerNames: ["claude", "codex"],
    });
  });

  it("counts machines with any offline runtime as issues", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({ id: "rt-online", provider: "claude" }),
        makeRuntime({
          id: "rt-offline",
          provider: "codex",
          status: "offline",
          last_seen_at: new Date(NOW - 10 * 60_000).toISOString(),
        }),
      ],
      { now: NOW },
    );

    expect(runtimeMachineCounts(machines)).toEqual({
      all: 1,
      online: 1,
      issues: 1,
    });
    expect(filterRuntimeMachines(machines, "", "issues")).toHaveLength(1);
  });

  it("does not surface agent CLI version branding as the machine subtitle", () => {
    // Reproduces the bug where every machine row's subtitle read
    // "Claude Code …" because compactDeviceInfo flipped the parenthetical
    // of the version string "2.1.5 (Claude Code)" into the description.
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          id: "rt-claude",
          provider: "claude",
          name: "Claude (dev.local)",
          device_info: "dev.local · 2.1.5 (Claude Code)",
        }),
        makeRuntime({
          id: "rt-codex",
          provider: "codex",
          name: "Codex (dev.local)",
          device_info: "dev.local · codex-cli 0.118.0",
        }),
      ],
      { now: NOW, localDaemonId: "daemon-1" },
    );

    expect(machines).toHaveLength(1);
    const subtitle = machines[0]?.subtitle ?? "";
    expect(subtitle.toLowerCase()).not.toContain("claude code");
    expect(subtitle.toLowerCase()).not.toContain("codex-cli");
    // Falls back to the daemon-id descriptor — at minimum it must not be
    // the runtime CLI's marketing string.
    expect(subtitle).toMatch(/^daemon /);
  });

  it("synthesizes a placeholder local machine when ensureLocalMachine is set and no runtime matches", () => {
    // Reproduces the "Start button disappears after stopping the daemon"
    // bug: the daemon is stopped (localDaemonId is null) and the server
    // has already GC'd the local runtime, so no machine ends up flagged
    // isCurrent. Without synthesis the local row vanishes and the
    // Start button has nowhere to render.
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          id: "rt-remote",
          daemon_id: "daemon-remote",
          name: "Claude (remote.box)",
          device_info: "remote.box",
        }),
      ],
      {
        now: NOW,
        localDaemonId: null,
        localMachineName: "My Laptop",
        ensureLocalMachine: true,
      },
    );

    expect(machines).toHaveLength(2);
    const local = machines.find((m) => m.isCurrent);
    expect(local).toMatchObject({
      title: "My Laptop",
      section: "local",
      isCurrent: true,
      runtimes: [],
    });
  });

  it("does not synthesize a placeholder when a real local runtime exists", () => {
    const machines = buildRuntimeMachines(
      [makeRuntime({ daemon_id: "daemon-1" })],
      {
        now: NOW,
        localDaemonId: "daemon-1",
        ensureLocalMachine: true,
      },
    );

    expect(machines).toHaveLength(1);
    expect(machines[0]).toMatchObject({
      isCurrent: true,
      runtimes: expect.arrayContaining([
        expect.objectContaining({ daemon_id: "daemon-1" }),
      ]),
    });
  });

  it("keeps cloud runtimes as cloud workers when they have no daemon", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          id: "cloud-1",
          daemon_id: null,
          runtime_mode: "cloud",
          provider: "codex",
          name: "Codex cloud",
          device_info: "",
        }),
      ],
      { now: NOW },
    );

    expect(machines[0]).toMatchObject({
      id: "cloud:runtime:cloud-1",
      title: "Codex cloud",
      subtitle: "Cloud worker",
      section: "cloud",
    });
  });
});

describe("splitRuntimeName", () => {
  it("separates daemon host suffix from provider name", () => {
    expect(splitRuntimeName("Claude (build-server-01)")).toEqual({
      base: "Claude",
      hostname: "build-server-01",
    });
  });

  it("falls back to the full name when no host suffix exists", () => {
    expect(splitRuntimeName("Codex cloud")).toEqual({
      base: "Codex cloud",
      hostname: null,
    });
  });
});

describe("display_name", () => {
  it("uses display_name as machine title when set", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          display_name: "My MacBook",
          name: "Claude (dev-machine.local)",
        }),
      ],
      { now: NOW },
    );

    expect(machines[0]).toMatchObject({
      title: "My MacBook",
    });
  });

  it("falls back to device name when display_name is null", () => {
    const machines = buildRuntimeMachines(
      [makeRuntime({ display_name: null })],
      { now: NOW },
    );

    expect(machines[0]).toMatchObject({
      title: "dev-machine.local",
    });
  });

  it("searches by display_name in filterRuntimeMachines", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          id: "rt-1",
          daemon_id: "daemon-1",
          display_name: "Production Server",
          name: "Claude (prod.local)",
        }),
        makeRuntime({
          id: "rt-2",
          daemon_id: "daemon-2",
          display_name: null,
          name: "Claude (dev.local)",
        }),
      ],
      { now: NOW },
    );

    expect(filterRuntimeMachines(machines, "Production", "all")).toHaveLength(1);
    expect(filterRuntimeMachines(machines, "Production", "all")[0]).toMatchObject({
      title: "Production Server",
    });
    expect(filterRuntimeMachines(machines, "dev.local", "all")).toHaveLength(1);
  });
});
