import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "@multica/core/types";
import {
  buildRuntimeMachines,
  filterRuntimeMachines,
  runtimeMachineCounts,
  runtimeRowLabel,
  sharedCustomName,
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
      { now: NOW },
    );

    expect(machines).toHaveLength(1);
    expect(machines[0]).toMatchObject({
      id: "local:daemon-1",
      title: "dev.local",
      section: "remote",
      onlineCount: 2,
      issueCount: 0,
      providerNames: ["claude", "codex"],
    });
  });

  it("uses the online daemon CLI version instead of a stale offline report", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({
          id: "rt-online",
          provider: "claude",
          metadata: { cli_version: "0.4.0" },
        }),
        makeRuntime({
          id: "rt-stale",
          provider: "copilot",
          status: "offline",
          last_seen_at: new Date(NOW - 4 * 24 * 60 * 60_000).toISOString(),
          metadata: { cli_version: "0.3.17" },
        }),
      ],
      { now: NOW },
    );

    expect(machines).toHaveLength(1);
    expect(machines[0]?.cliVersion).toBe("0.4.0");
  });

  it("uses a machine-wide custom name as the machine title", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({ id: "rt-claude", provider: "claude", custom_name: "Bohan's MacBook" }),
        makeRuntime({ id: "rt-codex", provider: "codex", custom_name: "Bohan's MacBook" }),
      ],
      { now: NOW },
    );

    expect(machines).toHaveLength(1);
    expect(machines[0]?.title).toBe("Bohan's MacBook");
  });

  it("ignores a one-off per-runtime custom name for the machine title", () => {
    const machines = buildRuntimeMachines(
      [
        makeRuntime({ id: "rt-claude", provider: "claude", custom_name: "just this one" }),
        makeRuntime({ id: "rt-codex", provider: "codex" }),
      ],
      { now: NOW },
    );

    // Not shared across all runtimes on the machine, so it must not stand in
    // for the whole machine's name — the device name wins instead.
    expect(machines[0]?.title).toBe("dev-machine.local");
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
      { now: NOW },
    );

    expect(machines).toHaveLength(1);
    const subtitle = machines[0]?.subtitle ?? "";
    expect(subtitle.toLowerCase()).not.toContain("claude code");
    expect(subtitle.toLowerCase()).not.toContain("codex-cli");
    // Falls back to the daemon-id descriptor — at minimum it must not be
    // the runtime CLI's marketing string.
    expect(subtitle).toMatch(/^daemon /);
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

describe("sharedCustomName", () => {
  it("returns the name when every runtime shares one non-empty custom_name", () => {
    expect(
      sharedCustomName([
        makeRuntime({ id: "a", custom_name: "Bohan's MacBook" }),
        makeRuntime({ id: "b", custom_name: "Bohan's MacBook" }),
      ]),
    ).toBe("Bohan's MacBook");
  });

  it("returns null when only some runtimes are named (a lone per-runtime name is not the machine name)", () => {
    expect(
      sharedCustomName([
        makeRuntime({ id: "a", custom_name: "just this one" }),
        makeRuntime({ id: "b", custom_name: null }),
      ]),
    ).toBeNull();
  });

  it("returns null when the names disagree, or the set is empty", () => {
    expect(
      sharedCustomName([
        makeRuntime({ id: "a", custom_name: "Air" }),
        makeRuntime({ id: "b", custom_name: "Pro" }),
      ]),
    ).toBeNull();
    expect(sharedCustomName([])).toBeNull();
  });
});

describe("runtimeRowLabel", () => {
  it("falls back to the provider base when no alias is set", () => {
    expect(
      runtimeRowLabel(
        makeRuntime({ name: "Codex (dev.local)", custom_name: null }),
        "dev.local",
      ),
    ).toBe("Codex");
  });

  it("collapses a machine-level alias (shared with the title) to the base", () => {
    // A machine rename stamps the same custom_name on every runtime, so the
    // title already shows it — repeating it per row would be noise.
    expect(
      runtimeRowLabel(
        makeRuntime({ name: "Codex (dev.local)", custom_name: "Dev Box" }),
        "Dev Box",
      ),
    ).toBe("Codex");
  });

  it("shows a per-runtime alias that differs from the machine title", () => {
    expect(
      runtimeRowLabel(
        makeRuntime({ name: "Codex (dev.local)", custom_name: "just this one" }),
        "Dev Box",
      ),
    ).toBe("just this one");
  });
});
