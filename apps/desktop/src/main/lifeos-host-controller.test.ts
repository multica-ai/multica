import { describe, expect, it, vi } from "vitest";
import { LifeOSHostController } from "./lifeos-host-controller";

describe("LifeOSHostController", () => {
  it("reports a ready backend", async () => {
    const controller = new LifeOSHostController({
      fetchImpl: vi.fn(async () =>
        new Response(JSON.stringify({ status: "ok" }), { status: 200 }),
      ) as typeof fetch,
      now: () => 42,
    });

    await expect(controller.probe()).resolves.toEqual({
      state: "ready",
      checkedAt: 42,
    });
  });

  it("runs only the fixed LifeOS ensure command", async () => {
    const execFileImpl = vi.fn(async () => ({ stdout: "", stderr: "" }));
    const controller = new LifeOSHostController({
      homeDirectory: "/Users/example",
      accessImpl: vi.fn(async () => undefined),
      execFileImpl,
      fetchImpl: vi.fn(async () =>
        new Response(JSON.stringify({ status: "ok" }), { status: 200 }),
      ) as typeof fetch,
    });

    await expect(controller.ensure()).resolves.toMatchObject({ state: "ready" });
    expect(execFileImpl).toHaveBeenCalledWith(
      "python3",
      [
        "/Users/example/code/lifeos-workbench/scripts/lifeos_workbench.py",
        "ensure",
      ],
      expect.objectContaining({ timeout: 120_000 }),
    );
  });

  it("returns an actionable error without executing when the launcher is missing", async () => {
    const execFileImpl = vi.fn();
    const controller = new LifeOSHostController({
      accessImpl: vi.fn(async () => {
        throw new Error("missing");
      }),
      execFileImpl,
    });

    await expect(controller.ensure()).resolves.toMatchObject({
      state: "error",
      message: expect.stringContaining("~/code/lifeos-workbench"),
    });
    expect(execFileImpl).not.toHaveBeenCalled();
  });

  it("deduplicates concurrent recovery requests", async () => {
    let resolveCommand: (() => void) | undefined;
    const execFileImpl = vi.fn(
      () =>
        new Promise<{ stdout: string; stderr: string }>((resolve) => {
          resolveCommand = () => resolve({ stdout: "", stderr: "" });
        }),
    );
    const controller = new LifeOSHostController({
      accessImpl: vi.fn(async () => undefined),
      execFileImpl,
      fetchImpl: vi.fn(async () =>
        new Response(JSON.stringify({ status: "ok" }), { status: 200 }),
      ) as typeof fetch,
    });

    const first = controller.ensure();
    const second = controller.ensure();
    await vi.waitFor(() => expect(resolveCommand).toBeDefined());
    resolveCommand?.();
    await Promise.all([first, second]);
    expect(execFileImpl).toHaveBeenCalledTimes(1);
  });

  it("opens immediately when the existing local host is already ready", async () => {
    const execFileImpl = vi.fn();
    const controller = new LifeOSHostController({
      fetchImpl: vi.fn(async () =>
        new Response(JSON.stringify({ status: "ok" }), { status: 200 }),
      ) as typeof fetch,
      execFileImpl,
    });

    await expect(controller.ensureIfNeeded()).resolves.toMatchObject({
      state: "ready",
    });
    expect(execFileImpl).not.toHaveBeenCalled();
  });
});
