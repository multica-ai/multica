// @vitest-environment node
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SetupInspector, type ProbeResult } from "./inspect.js";

const available = (output: string): ProbeResult => ({
  kind: "available",
  output,
});

const roots: string[] = [];
afterEach(async () => {
  for (const root of roots.splice(0))
    await rm(root, { recursive: true, force: true });
});
async function fixture(node = ">=22") {
  const root = await mkdtemp(join(tmpdir(), "verification-inspect-"));
  roots.push(root);
  await mkdir(join(root, "server"));
  await writeFile(
    join(root, "package.json"),
    JSON.stringify({
      name: "test-project",
      engines: { node },
      packageManager: "pnpm@10.28.2",
    }),
  );
  await writeFile(join(root, "server", "go.mod"), "module test\n\ngo 1.25.0\n");
  return root;
}
describe("checkout toolchain inspection", () => {
  it("uses checkout requirements without claiming application readiness or resolving agent CLIs", async () => {
    const root = await fixture();
    const probe = vi.fn(async (command: string, args: string[]) =>
      available(
        command === process.execPath
          ? "v24.0.0"
          : command === "pnpm"
            ? "10.28.2"
            : command === "go"
              ? "go version go1.25.2 darwin/arm64"
              : args.includes("HEAD")
                ? "a".repeat(40)
                : "git version 2.50.0",
      ),
    );
    const result = await new SetupInspector(root, probe).inspect("desktop");
    expect(result).toMatchObject({
      inspectionScope: "toolchain",
      toolchainStatus: "ready",
      applicationReady: "notChecked",
      project: { commit: "a".repeat(40) },
    });
    expect(result.notChecked).toContain("applicationHealth");
    expect(probe.mock.calls.map(([command]) => command)).toEqual([
      process.execPath,
      "pnpm",
      "go",
      "git",
      "git",
    ]);
  });

  it("reports missing and incompatible tools; unsupported version ranges stay unknown", async () => {
    const root = await fixture();
    const probe = async (command: string): Promise<ProbeResult> =>
      command === process.execPath
        ? available("v20.0.0")
        : command === "pnpm"
          ? available("9.0.0")
          : { kind: "missing" };
    const result = await new SetupInspector(root, probe).inspect("desktop");
    expect(result.toolchainStatus).toBe("blocked");
    expect(result.checks.map((check) => check.status)).toEqual([
      "incompatible",
      "incompatible",
      "missing",
      "missing",
    ]);
    const unsupported = await fixture("^22 || ^24");
    const other = await new SetupInspector(unsupported, async (command) =>
      command === process.execPath
        ? available("v24.0.0")
        : command === "pnpm"
          ? available("10.28.2")
          : command === "git"
            ? available("git version 2.50.0")
            : command === "go"
              ? available("go version go1.25.2 darwin/arm64")
              : { kind: "missing" },
    ).inspect("renderer");
    expect(other.toolchainStatus).toBe("unknown");
    expect(other.checks.find((check) => check.name === "go")?.required).toBe(
      true,
    );
  });

  it("does not run probes after cancellation", async () => {
    const root = await fixture();
    const probe = vi.fn(
      async (): Promise<ProbeResult> => ({ kind: "missing" }),
    );
    const controller = new AbortController();
    controller.abort();
    await expect(
      new SetupInspector(root, probe).inspect("desktop", controller.signal),
    ).rejects.toThrow();
    expect(probe).not.toHaveBeenCalled();
  });

  it("keeps failed probes unknown and compares minor and patch requirements", async () => {
    const root = await fixture(">=24.1.2");
    const result = await new SetupInspector(root, async (command) =>
      command === process.execPath
        ? available("v24.1.1")
        : { kind: "unavailable" },
    ).inspect("desktop");
    expect(result.checks[0]?.status).toBe("incompatible");
    expect(result.checks[1]?.status).toBe("unknown");
    const unknown = await new SetupInspector(root, async () => ({
      kind: "unavailable",
    })).inspect("desktop");
    expect(unknown.toolchainStatus).toBe("unknown");
  });
});
