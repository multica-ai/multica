// @vitest-environment node
import { EventEmitter } from "node:events";
import { afterEach, describe, expect, it, vi } from "vitest";

const startup = vi.hoisted(() => ({
  stopped: new Error("Stop before Electron startup"),
}));

// Stop at the first Electron import so this test cannot start the app.
vi.mock("electron", () => {
  throw startup.stopped;
});

describe("main-process stdio errors", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it("handles broken stdout and stderr before main-process startup", async () => {
    const stdout = new EventEmitter();
    const stderr = new EventEmitter();
    vi.spyOn(process, "stdout", "get").mockReturnValue(
      stdout as typeof process.stdout,
    );
    vi.spyOn(process, "stderr", "get").mockReturnValue(
      stderr as typeof process.stderr,
    );

    await expect(import("./index")).rejects.toMatchObject({
      cause: startup.stopped,
    });

    for (const stream of [stdout, stderr]) {
      for (const code of ["EPIPE", "EIO"]) {
        const error = Object.assign(new Error(`write ${code}`), { code });
        expect(() => stream.emit("error", error)).not.toThrow();
      }
    }
  });
});
