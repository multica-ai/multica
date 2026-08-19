import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ensureWebPushSubscription,
  getWebPushCapability,
  revokeWebPushSubscription,
} from "./web-push";

const validSubscription = {
  endpoint: "https://push.test/subscription-1",
  expirationTime: null,
  keys: { p256dh: "p256dh", auth: "auth" },
};

function installBrowserPush({
  permission = "granted",
  existing = null,
}: {
  permission?: NotificationPermission;
  existing?: typeof validSubscription | null;
} = {}) {
  const subscribe = vi.fn(async () => ({ toJSON: () => validSubscription }));
  const getSubscription = vi.fn(async () =>
    existing
      ? {
          endpoint: existing.endpoint,
          toJSON: () => existing,
          unsubscribe: vi.fn(async () => true),
        }
      : null,
  );
  const registration = {
    pushManager: { subscribe, getSubscription },
  };
  const register = vi.fn(async () => registration);
  const getRegistration = vi.fn(async () => registration);
  const requestPermission = vi.fn(
    async () => "granted" as NotificationPermission,
  );

  vi.stubGlobal("navigator", {
    serviceWorker: { register, getRegistration },
  });
  vi.stubGlobal("Notification", { permission, requestPermission });
  vi.stubGlobal("PushManager", class {});
  return {
    subscribe,
    getSubscription,
    register,
    getRegistration,
    requestPermission,
  };
}

afterEach(() => vi.unstubAllGlobals());

describe("background Web Push browser adapter", () => {
  it("fails closed when service workers, PushManager, or permission are unavailable", async () => {
    expect(getWebPushCapability()).toBe("unsupported");

    installBrowserPush({ permission: "denied" });
    expect(getWebPushCapability()).toBe("denied");
    await expect(
      ensureWebPushSubscription("BEl6ZmFrZS1wdWJsaWMta2V5", true),
    ).resolves.toBeNull();
  });

  it("reuses an existing background subscription without prompting", async () => {
    const browser = installBrowserPush({ existing: validSubscription });

    await expect(
      ensureWebPushSubscription("BEl6ZmFrZS1wdWJsaWMta2V5", false),
    ).resolves.toEqual(validSubscription);
    expect(browser.requestPermission).not.toHaveBeenCalled();
    expect(browser.subscribe).not.toHaveBeenCalled();
    expect(browser.register).toHaveBeenCalledWith(
      "/vibes-sw.js?v=vibes-feed-v7",
      { scope: "/", updateViaCache: "none" },
    );
  });

  it("requests permission only from an explicit user action and subscribes with the application key", async () => {
    const browser = installBrowserPush({ permission: "default" });

    await expect(
      ensureWebPushSubscription("BEl6ZmFrZS1wdWJsaWMta2V5", false),
    ).resolves.toBeNull();
    expect(browser.requestPermission).not.toHaveBeenCalled();

    await expect(
      ensureWebPushSubscription("BEl6ZmFrZS1wdWJsaWMta2V5", true),
    ).resolves.toEqual(validSubscription);
    expect(browser.requestPermission).toHaveBeenCalledOnce();
    expect(browser.subscribe).toHaveBeenCalledWith(
      expect.objectContaining({ userVisibleOnly: true }),
    );
  });

  it("returns a revoked endpoint for authenticated server cleanup", async () => {
    const browser = installBrowserPush({
      permission: "denied",
      existing: validSubscription,
    });

    await expect(revokeWebPushSubscription()).resolves.toBe(
      validSubscription.endpoint,
    );
    expect(browser.getRegistration).toHaveBeenCalledWith("/");
  });
});
