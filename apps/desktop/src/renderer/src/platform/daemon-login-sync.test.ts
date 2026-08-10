import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  syncDaemonOnLogin,
  type DaemonLoginSyncAPI,
} from "./daemon-login-sync";

const calls: string[] = [];

function makeApi(overrides: Partial<DaemonLoginSyncAPI> = {}): DaemonLoginSyncAPI {
  return {
    setTargetApiUrl: vi.fn(async () => {
      calls.push("setTargetApiUrl");
    }),
    syncToken: vi.fn(async () => {
      calls.push("syncToken");
    }),
    autoStart: vi.fn(async () => {
      calls.push("autoStart");
    }),
    ...overrides,
  };
}

beforeEach(() => {
  calls.length = 0;
  vi.clearAllMocks();
});

describe("syncDaemonOnLogin", () => {
  // Regression: syncToken used to race a separate effect's setTargetApiUrl.
  // Arriving first meant main had no resolved profile and wrote the token to
  // the user's default CLI profile. #6399.
  it("pushes the target URL before syncing the token", async () => {
    const api = makeApi();
    await syncDaemonOnLogin(api, "https://api.example.com", "tok", "user-1");

    expect(calls).toEqual(["setTargetApiUrl", "syncToken", "autoStart"]);
    expect(api.setTargetApiUrl).toHaveBeenCalledWith("https://api.example.com");
    expect(api.syncToken).toHaveBeenCalledWith("tok", "user-1");
  });

  it("awaits the target URL rather than firing it off", async () => {
    let released: (() => void) | undefined;
    const api = makeApi({
      setTargetApiUrl: vi.fn(
        () =>
          new Promise<void>((resolve) => {
            released = () => {
              calls.push("setTargetApiUrl");
              resolve();
            };
          }),
      ),
    });

    const pending = syncDaemonOnLogin(api, "https://api.example.com", "t", "u");
    await Promise.resolve();
    expect(api.syncToken).not.toHaveBeenCalled();

    released?.();
    await pending;
    expect(calls).toEqual(["setTargetApiUrl", "syncToken", "autoStart"]);
  });

  it("does not start the daemon when the token sync fails", async () => {
    const api = makeApi({
      syncToken: vi.fn(async () => {
        throw new Error("daemon profile is not resolved yet");
      }),
    });

    await expect(
      syncDaemonOnLogin(api, "https://api.example.com", "t", "u"),
    ).rejects.toThrow(/not resolved/);
    expect(api.autoStart).not.toHaveBeenCalled();
  });
});
