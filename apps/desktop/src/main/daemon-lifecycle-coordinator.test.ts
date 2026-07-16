import { describe, expect, it, vi } from "vitest";
import {
  DaemonLifecycleCoordinator,
  daemonStopArgs,
} from "./daemon-lifecycle-coordinator";

describe("daemonStopArgs", () => {
  it("requires an atomic idle shutdown for automatic restarts", () => {
    expect(daemonStopArgs("desktop-host", true)).toEqual([
      "daemon",
      "stop",
      "--require-idle",
      "--profile",
      "desktop-host",
    ]);
  });

  it("keeps explicit operator stops unconditional", () => {
    expect(daemonStopArgs("", false)).toEqual(["daemon", "stop"]);
  });
});

describe("DaemonLifecycleCoordinator", () => {
  it("serializes automatic and manual lifecycle operations", async () => {
    const coordinator = new DaemonLifecycleCoordinator();
    let releaseFirst: (() => void) | undefined;
    const first = coordinator.run(
      () =>
        new Promise<{ success: true }>((resolve) => {
          releaseFirst = () => resolve({ success: true });
        }),
    );
    const blockedOperations = ["polling", "version", "manual"].map(() => {
      const operation = vi.fn(async () => ({ success: true as const }));
      return { operation, result: coordinator.run(operation) };
    });

    for (const blocked of blockedOperations) {
      await expect(blocked.result).resolves.toEqual({
        success: false,
        error: "Another daemon operation is in progress",
      });
      expect(blocked.operation).not.toHaveBeenCalled();
    }

    releaseFirst?.();
    await expect(first).resolves.toEqual({ success: true });
    await expect(
      coordinator.run(async () => ({ success: true as const })),
    ).resolves.toEqual({ success: true });
  });

  it("releases the lifecycle slot after an operation throws", async () => {
    const coordinator = new DaemonLifecycleCoordinator();

    await expect(
      coordinator.run(async () => {
        throw new Error("failed");
      }),
    ).rejects.toThrow("failed");
    await expect(
      coordinator.run(async () => ({ success: true as const })),
    ).resolves.toEqual({ success: true });
  });
});
