import { describe, expect, it, vi } from "vitest";

import {
  FURTHERREF_BUNDLE_ID,
  buildMacInstallScript,
  findContainingAppBundle,
  isVersionNewer,
  scheduleMacFallbackInstall,
} from "./mac-update-installer";

describe("findContainingAppBundle", () => {
  it("finds the outer .app bundle from the executable path", () => {
    expect(
      findContainingAppBundle(
        "/Applications/Multica.app/Contents/MacOS/Multica",
      ),
    ).toBe("/Applications/Multica.app");
  });

  it("handles app names with spaces", () => {
    expect(
      findContainingAppBundle(
        "/Applications/Multica Canary.app/Contents/MacOS/Multica",
      ),
    ).toBe("/Applications/Multica Canary.app");
  });

  it("returns null when the executable is not inside an app bundle", () => {
    expect(findContainingAppBundle("/usr/local/bin/multica")).toBeNull();
  });
});

describe("isVersionNewer", () => {
  it("compares dotted numeric versions", () => {
    expect(isVersionNewer("0.2.24", "0.2.23")).toBe(true);
    expect(isVersionNewer("0.2.23", "0.2.24")).toBe(false);
    expect(isVersionNewer("v0.3.0", "0.2.99")).toBe(true);
    expect(isVersionNewer("0.2.24", "0.2.24")).toBe(false);
  });
});

describe("buildMacInstallScript", () => {
  it("creates a rollback-capable full app replacement script", () => {
    const script = buildMacInstallScript({
      appBundlePath: "/Applications/Multica.app",
      backupBundlePath: "/Applications/Multica.app.backup-123",
      newBundlePath: "/tmp/multica-update/Multica.app",
      currentPid: 12345,
      logPath: "/tmp/multica-install.log",
    });

    expect(script).toContain("while kill -0 12345");
    expect(script).toContain(
      'mv "/Applications/Multica.app" "/Applications/Multica.app.backup-123"',
    );
    expect(script).toContain(
      'mv "/tmp/multica-update/Multica.app" "/Applications/Multica.app"',
    );
    expect(script).toContain(
      'mv "/Applications/Multica.app.backup-123" "/Applications/Multica.app"',
    );
    expect(script).toContain('open "/Applications/Multica.app"');
    expect(script).not.toContain("rm -rf /Applications/Multica.app");
  });

  it("quotes shell paths that contain spaces", () => {
    const script = buildMacInstallScript({
      appBundlePath: "/Applications/Multica Canary.app",
      backupBundlePath: "/Applications/Multica Canary.app.backup-123",
      newBundlePath: "/tmp/multica update/Multica Canary.app",
      currentPid: 12345,
      logPath: "/tmp/multica install.log",
    });

    expect(script).toContain('"/Applications/Multica Canary.app"');
    expect(script).toContain('"/tmp/multica update/Multica Canary.app"');
    expect(script).toContain('"/tmp/multica install.log"');
  });
});

describe("FURTHERREF_BUNDLE_ID", () => {
  it("uses the FurtherRef bundle identifier", () => {
    expect(FURTHERREF_BUNDLE_ID).toBe("com.furtherref.multica");
  });
});

describe("scheduleMacFallbackInstall", () => {
  it("cancels the fallback installer when the app starts quitting", () => {
    const timers = new Map<ReturnType<typeof setTimeout>, () => void>();
    const clearTimeoutCalls: ReturnType<typeof setTimeout>[] = [];
    let willQuitHandler: (() => void) | null = null;
    const fallback = vi.fn();
    const setTimer = vi.fn((fn: () => void) => {
      const id = Symbol("timer") as unknown as ReturnType<typeof setTimeout>;
      timers.set(id, fn);
      return id;
    });
    const clearTimer = vi.fn((id: ReturnType<typeof setTimeout>) => {
      clearTimeoutCalls.push(id);
      timers.delete(id);
    });

    scheduleMacFallbackInstall({
      app: {
        once: (_event, handler) => {
          willQuitHandler = handler;
        },
      },
      delayMs: 10_000,
      setTimer,
      clearTimer,
      fallback,
    });

    const triggerWillQuit = () => {
      if (!willQuitHandler) throw new Error("will-quit handler was not set");
      willQuitHandler();
    };

    triggerWillQuit();

    expect(clearTimer).toHaveBeenCalledTimes(1);
    expect(clearTimeoutCalls).toHaveLength(1);
    for (const fn of timers.values()) fn();
    expect(fallback).not.toHaveBeenCalled();
  });
});
