import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ACTIVE_TOKEN_KEY,
  tokenStorageKey,
} from "../../../shared/server-session";

const mocks = vi.hoisted(() => ({
  switchServer: vi.fn(),
  reload: vi.fn(),
}));

import { switchDesktopServer } from "./server-switch";

describe("switchDesktopServer", () => {
  beforeEach(() => {
    localStorage.clear();
    mocks.switchServer.mockReset();
    mocks.reload.mockReset();

    Object.defineProperty(window, "desktopAPI", {
      configurable: true,
      value: {
        runtimeConfig: {
          ok: true,
          config: {
            schemaVersion: 1,
            apiUrl: "https://api.multica.ai",
            wsUrl: "wss://api.multica.ai/ws",
            appUrl: "https://multica.ai",
          },
          servers: {
            editable: true,
            activeServerId: "cloud",
            servers: [],
          },
        },
        switchServer: mocks.switchServer,
      },
    });

    Object.defineProperty(window, "location", {
      configurable: true,
      value: { reload: mocks.reload },
    });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("snapshots the current session, restores the target, and reloads", async () => {
    localStorage.setItem(ACTIVE_TOKEN_KEY, "cloud-token");
    mocks.switchServer.mockResolvedValue({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: "http://127.0.0.1:28443",
        wsUrl: "ws://127.0.0.1:28443/ws",
        appUrl: "http://127.0.0.1:28443",
      },
      servers: {
        editable: true,
        activeServerId: "personal",
        servers: [],
      },
    });
    // Pre-seed personal session so restore does not clear live token via
    // the "already has namespaced tokens" path incorrectly for personal.
    localStorage.setItem(
      tokenStorageKey("http://127.0.0.1:28443"),
      "personal-token",
    );

    await switchDesktopServer("personal");

    expect(localStorage.getItem(tokenStorageKey("https://api.multica.ai"))).toBe(
      "cloud-token",
    );
    expect(localStorage.getItem(ACTIVE_TOKEN_KEY)).toBe("personal-token");
    expect(mocks.switchServer).toHaveBeenCalledWith("personal");
    expect(mocks.reload).toHaveBeenCalledTimes(1);
  });

  it("clears the live token when the target has no saved session", async () => {
    localStorage.setItem(ACTIVE_TOKEN_KEY, "cloud-token");
    mocks.switchServer.mockResolvedValue({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: "https://company.example",
        wsUrl: "wss://company.example/ws",
        appUrl: "https://company.example",
      },
      servers: {
        editable: true,
        activeServerId: "company",
        servers: [],
      },
    });

    await switchDesktopServer("company");

    expect(localStorage.getItem(tokenStorageKey("https://api.multica.ai"))).toBe(
      "cloud-token",
    );
    expect(localStorage.getItem(ACTIVE_TOKEN_KEY)).toBeNull();
    expect(mocks.reload).toHaveBeenCalledTimes(1);
  });

  it("throws when runtime config is invalid", async () => {
    Object.defineProperty(window, "desktopAPI", {
      configurable: true,
      value: {
        runtimeConfig: { ok: false, error: { message: "bad desktop.json" } },
        switchServer: mocks.switchServer,
      },
    });

    await expect(switchDesktopServer("personal")).rejects.toThrow(
      /bad desktop\.json/,
    );
    expect(mocks.switchServer).not.toHaveBeenCalled();
    expect(mocks.reload).not.toHaveBeenCalled();
  });

  it("throws when switchServer fails and does not reload", async () => {
    mocks.switchServer.mockResolvedValue({
      ok: false,
      error: "not editable in dev",
    });

    await expect(switchDesktopServer("personal")).rejects.toThrow(
      /not editable in dev/,
    );
    expect(mocks.reload).not.toHaveBeenCalled();
  });
});
