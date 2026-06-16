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
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "dev-machine.local · claude 1.0.0",
    metadata: { cli_version: "0.3.0" },
    owner_id: "user-1",
    visibility: "private",
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

  it("treats a runtime with the local device name as the current machine", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          daemon_id: "legacy-hostname",
          name: "Claude (My Laptop)",
          device_info: "My Laptop · claude 1.0.0",
        }),
      ],
      {
        now: NOW,
        localDaemonId: "daemon-uuid",
        localMachineName: "my laptop",
        currentUserId: "user-1",
        ensureLocalMachine: true,
      },
    );

    expect(machines).toHaveLength(1);
    expect(machines[0]).toMatchObject({
      title: "my laptop",
      section: "local",
      isCurrent: true,
      daemonId: "legacy-hostname",
    });
  });

  it("does not treat a cloud runtime with the local device name as current", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          id: "cloud-1",
          daemon_id: null,
          runtime_mode: "cloud",
          provider: "codex",
          name: "Codex (My Laptop)",
          device_info: "My Laptop · codex 1.0.0",
        }),
      ],
      {
        now: NOW,
        localDaemonId: "daemon-uuid",
        localMachineName: "my laptop",
        currentUserId: "user-1",
        ensureLocalMachine: true,
      },
    );

    expect(machines).toHaveLength(2);
    const cloud = machines.find((m) => m.id === "cloud:device:My Laptop");
    expect(cloud).toMatchObject({
      title: "My Laptop",
      section: "cloud",
      isCurrent: false,
    });
    const local = machines.find((m) => m.isCurrent);
    expect(local).toMatchObject({
      title: "my laptop",
      section: "local",
      runtimes: [],
    });
  });

  it("consolidates an out-of-band local daemon (WSL2) by host name and suppresses the placeholder", () => {
    // The desktop doesn't manage this daemon (it runs in WSL2), so
    // localDaemonId never matches. localMachineName falls back to the OS
    // hostname, and the runtime is owned by the viewing user — so it must
    // consolidate into the local section, and no empty placeholder appears.
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          id: "rt-wsl2",
          daemon_id: "wsl2-daemon-uuid",
          name: "Claude (KIKI-PC)",
          device_info: "KIKI-PC · claude 1.0.0",
          owner_id: "user-1",
        }),
      ],
      {
        now: NOW,
        localDaemonId: "desktop-daemon-uuid",
        localMachineName: "KIKI-PC",
        currentUserId: "user-1",
        ensureLocalMachine: true,
      },
    );

    expect(machines).toHaveLength(1);
    expect(machines[0]).toMatchObject({
      title: "KIKI-PC",
      section: "local",
      isCurrent: true,
      daemonId: "wsl2-daemon-uuid",
    });
  });

  it("does not claim another user's identically-named machine as current", () => {
    // Same host name, but the runtime belongs to a different user. Device-name
    // consolidation must NOT fire, so it stays remote and the placeholder for
    // this machine is still synthesized.
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          id: "rt-other",
          daemon_id: "other-daemon-uuid",
          name: "Claude (KIKI-PC)",
          device_info: "KIKI-PC · claude 1.0.0",
          owner_id: "user-2",
        }),
      ],
      {
        now: NOW,
        localDaemonId: "desktop-daemon-uuid",
        localMachineName: "KIKI-PC",
        currentUserId: "user-1",
        ensureLocalMachine: true,
      },
    );

    expect(machines).toHaveLength(2);
    const other = machines.find((m) => m.id === "local:other-daemon-uuid");
    expect(other).toMatchObject({ section: "remote", isCurrent: false });
    const local = machines.find((m) => m.isCurrent);
    expect(local).toMatchObject({ section: "local", runtimes: [] });
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
