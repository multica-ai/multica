import { describe, expect, it, vi } from "vitest";
import type { LocalStackState } from "../shared/local-stack";
import type { CommandRunner } from "./local-stack";
import { bringUpLocalStack } from "./local-stack";

const config = {
  repoDir: "/repo",
  composeFile: "docker-compose.selfhost.yml",
  backendPort: 8081,
};

function okRunner() {
  return vi.fn<CommandRunner>(async () => ({ ok: true, stdout: "", stderr: "" }));
}

describe("bringUpLocalStack", () => {
  it("takes the fast path when the backend already answers", async () => {
    const run = okRunner();
    const states: LocalStackState[] = [];

    const result = await bringUpLocalStack({
      config,
      run,
      probeBackend: async () => true,
      onState: (s) => states.push(s),
      sleep: async () => {},
    });

    expect(result).toEqual({ phase: "ready" });
    expect(run).not.toHaveBeenCalled();
    expect(states.at(-1)).toEqual({ phase: "ready" });
  });

  it("starts colima only when it is not already running", async () => {
    const run = vi.fn(async (bin: string, args: string[]) => {
      if (bin === "colima" && args[0] === "status") {
        return { ok: true, stdout: "colima is running", stderr: "" };
      }
      return { ok: true, stdout: "", stderr: "" };
    });
    let probes = 0;

    await bringUpLocalStack({
      config,
      run,
      probeBackend: async () => ++probes > 1,
      onState: () => {},
      sleep: async () => {},
    });

    const calls = run.mock.calls.map(([bin, args]) => [bin, ...args].join(" "));
    expect(calls).not.toContain("colima start --cpu 2 --memory 4");
    expect(calls).toContain(
      "docker compose -f docker-compose.selfhost.yml up -d",
    );
  });

  it("starts colima when status reports it is stopped", async () => {
    const run = vi.fn(async (bin: string, args: string[]) => {
      if (bin === "colima" && args[0] === "status") {
        return { ok: false, stdout: "", stderr: "colima is not running" };
      }
      return { ok: true, stdout: "", stderr: "" };
    });
    let probes = 0;

    await bringUpLocalStack({
      config,
      run,
      probeBackend: async () => ++probes > 1,
      onState: () => {},
      sleep: async () => {},
    });

    const calls = run.mock.calls.map(([bin, args]) => [bin, ...args].join(" "));
    expect(calls).toContain("colima start --cpu 2 --memory 4");
  });

  it("emits steps in order", async () => {
    let probes = 0;
    const states: LocalStackState[] = [];

    await bringUpLocalStack({
      config,
      run: vi.fn(async (bin: string, args: string[]) =>
        bin === "colima" && args[0] === "status"
          ? { ok: false, stdout: "", stderr: "" }
          : { ok: true, stdout: "", stderr: "" },
      ),
      probeBackend: async () => ++probes > 1,
      onState: (s) => states.push(s),
      sleep: async () => {},
    });

    const steps = states
      .filter((s) => s.phase === "running")
      .map((s) => (s as { step: string }).step);
    expect(steps).toEqual(["probe", "engine", "containers", "backend"]);
  });

  it("fails with the engine step and stderr when colima start fails", async () => {
    const result = await bringUpLocalStack({
      config,
      run: vi.fn(async (bin: string, args: string[]) => {
        if (bin === "colima" && args[0] === "status") {
          return { ok: false, stdout: "", stderr: "" };
        }
        return { ok: false, stdout: "", stderr: "no space left on device" };
      }),
      probeBackend: async () => false,
      onState: () => {},
      sleep: async () => {},
    });

    expect(result).toEqual({
      phase: "failed",
      step: "engine",
      message: "no space left on device",
    });
  });

  it("fails with the containers step when compose up fails", async () => {
    const result = await bringUpLocalStack({
      config,
      run: vi.fn(async (bin: string, _args: string[]) => {
        if (bin === "colima") return { ok: true, stdout: "running", stderr: "" };
        return { ok: false, stdout: "", stderr: "compose exploded" };
      }),
      probeBackend: async () => false,
      onState: () => {},
      sleep: async () => {},
    });

    expect(result).toEqual({
      phase: "failed",
      step: "containers",
      message: "compose exploded",
    });
  });

  it("times out waiting for the backend instead of hanging", async () => {
    const result = await bringUpLocalStack({
      config,
      run: okRunner(),
      probeBackend: async () => false,
      onState: () => {},
      sleep: async () => {},
      backendTimeoutMs: 30,
    });

    expect(result.phase).toBe("failed");
    expect((result as { step: string }).step).toBe("backend");
    expect((result as { message: string }).message).toMatch(/did not respond/i);
  });

  it("uses the configured compose file rather than a hardcoded one", async () => {
    const run = okRunner();
    let probes = 0;
    await bringUpLocalStack({
      config: { ...config, composeFile: "docker-compose.custom.yml" },
      run,
      probeBackend: async () => ++probes > 1,
      onState: () => {},
      sleep: async () => {},
    });

    const calls = run.mock.calls.map(([bin, args]) => [bin, ...args].join(" "));
    expect(calls).toContain("docker compose -f docker-compose.custom.yml up -d");
  });

  it("never pulls images — the backend tag is a local shadow tag", async () => {
    const run = okRunner();
    let probes = 0;
    await bringUpLocalStack({
      config,
      run,
      probeBackend: async () => ++probes > 1,
      onState: () => {},
      sleep: async () => {},
    });

    const calls = run.mock.calls.map(([bin, args]) => [bin, ...args].join(" "));
    expect(calls.some((c) => c.includes("pull"))).toBe(false);
  });
});
