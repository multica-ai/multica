// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  tearDownOnLogout,
  tearDownOnSessionExpiry,
  type SessionTeardown,
} from "./session-teardown";

function makeTeardown(
  overrides: Partial<SessionTeardown> = {},
): SessionTeardown {
  return {
    reportAuthSession: vi.fn(),
    resetTabs: vi.fn(),
    closeOverlay: vi.fn(),
    resetWelcome: vi.fn(),
    clearDaemonToken: vi.fn(async () => {}),
    stopDaemon: vi.fn(async () => {}),
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("tearDownOnLogout", () => {
  it("stops the daemon and wipes window state the next user must not inherit", async () => {
    const t = makeTeardown();
    await tearDownOnLogout(t);

    expect(t.reportAuthSession).toHaveBeenCalledWith(null);
    expect(t.resetTabs).toHaveBeenCalledOnce();
    expect(t.closeOverlay).toHaveBeenCalledOnce();
    expect(t.resetWelcome).toHaveBeenCalledOnce();
    expect(t.clearDaemonToken).toHaveBeenCalledOnce();
    expect(t.stopDaemon).toHaveBeenCalledOnce();
  });

  it("still stops the daemon when clearing its token fails", async () => {
    const t = makeTeardown({
      clearDaemonToken: vi.fn(async () => {
        throw new Error("profile locked");
      }),
    });

    await expect(tearDownOnLogout(t)).resolves.toBeUndefined();
    expect(t.stopDaemon).toHaveBeenCalledOnce();
  });

  it("survives a daemon that is already stopped", async () => {
    const t = makeTeardown({
      stopDaemon: vi.fn(async () => {
        throw new Error("not running");
      }),
    });

    await expect(tearDownOnLogout(t)).resolves.toBeUndefined();
  });
});

describe("tearDownOnSessionExpiry", () => {
  // The decision this file exists to protect: the daemon authenticates with
  // its own PAT and may be running agent work, so a UI credential expiring
  // must not take it down. Only this window has to sign in again (MUL-7028).
  it("leaves the daemon running", () => {
    const t = makeTeardown();
    tearDownOnSessionExpiry(t);

    expect(t.clearDaemonToken).not.toHaveBeenCalled();
    expect(t.stopDaemon).not.toHaveBeenCalled();
  });

  // Tab paths carry workspace slugs and issue ids. Leaving them for the
  // workspace validator to prune catches nothing when the next user shares a
  // workspace with the last one.
  it("clears window state the next user must not inherit", () => {
    const t = makeTeardown();
    tearDownOnSessionExpiry(t);

    expect(t.reportAuthSession).toHaveBeenCalledWith(null);
    expect(t.resetTabs).toHaveBeenCalledOnce();
    expect(t.closeOverlay).toHaveBeenCalledOnce();
    expect(t.resetWelcome).toHaveBeenCalledOnce();
  });
});
