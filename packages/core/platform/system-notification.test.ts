import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getWebNotificationPermission,
  isWebNotificationSupported,
  registerSystemNotificationClickHandler,
  requestWebNotificationPermission,
  showWebNotification,
  type SystemNotificationPayload,
} from "./system-notification";

// The core test environment is Node — `window`/`Notification` don't exist.
// These tests install a minimal `window.Notification` stub on `globalThis`
// (the same surface `getNotificationCtor` reads) to exercise the browser path.

type Created = { title: string; options?: NotificationOptions; instance: FakeNotification };

class FakeNotification {
  static permission: NotificationPermission = "granted";
  static requestPermission = vi.fn(async () => FakeNotification.permission);
  onclick: (() => void) | null = null;
  close = vi.fn();
  constructor(
    public title: string,
    public options?: NotificationOptions,
  ) {
    created.push({ title, options, instance: this });
  }
}

let created: Created[] = [];

function installWindow(
  ctor: unknown,
  focus: () => void = () => {},
): void {
  (globalThis as Record<string, unknown>).window = { Notification: ctor, focus };
}

function payload(
  overrides: Partial<SystemNotificationPayload> = {},
): SystemNotificationPayload {
  return {
    slug: "workspace-a",
    itemId: "item-1",
    issueKey: "issue-1",
    title: "Mentioned you",
    body: "in a comment",
    ...overrides,
  };
}

afterEach(() => {
  created = [];
  FakeNotification.permission = "granted";
  FakeNotification.requestPermission.mockClear();
  registerSystemNotificationClickHandler(null);
  delete (globalThis as Record<string, unknown>).window;
});

describe("isWebNotificationSupported / getWebNotificationPermission", () => {
  it("reports unsupported when there is no window", () => {
    expect(isWebNotificationSupported()).toBe(false);
    expect(getWebNotificationPermission()).toBe("unsupported");
  });

  it("reflects the browser's current permission when supported", () => {
    installWindow(FakeNotification);
    FakeNotification.permission = "denied";
    expect(isWebNotificationSupported()).toBe(true);
    expect(getWebNotificationPermission()).toBe("denied");
  });
});

describe("requestWebNotificationPermission", () => {
  it("returns 'unsupported' without the API", async () => {
    await expect(requestWebNotificationPermission()).resolves.toBe("unsupported");
  });

  it("prompts only when permission is 'default'", async () => {
    installWindow(FakeNotification);
    FakeNotification.permission = "default";
    FakeNotification.requestPermission.mockResolvedValueOnce("granted");

    await expect(requestWebNotificationPermission()).resolves.toBe("granted");
    expect(FakeNotification.requestPermission).toHaveBeenCalledTimes(1);
  });

  it("does not re-prompt once already decided", async () => {
    installWindow(FakeNotification);
    FakeNotification.permission = "denied";

    await expect(requestWebNotificationPermission()).resolves.toBe("denied");
    expect(FakeNotification.requestPermission).not.toHaveBeenCalled();
  });
});

describe("showWebNotification", () => {
  it("does nothing when the API is unavailable", () => {
    expect(() => showWebNotification(payload())).not.toThrow();
    expect(created).toHaveLength(0);
  });

  it("does nothing unless permission is granted", () => {
    installWindow(FakeNotification);
    FakeNotification.permission = "default";
    showWebNotification(payload());
    expect(created).toHaveLength(0);
  });

  it("shows a banner with body + a dedup tag when granted", () => {
    installWindow(FakeNotification);
    showWebNotification(payload());
    expect(created).toHaveLength(1);
    expect(created[0]?.title).toBe("Mentioned you");
    expect(created[0]?.options).toMatchObject({ body: "in a comment", tag: "item-1" });
  });

  it("routes a click to the registered handler and closes the banner", () => {
    const focus = vi.fn();
    installWindow(FakeNotification, focus);
    const onClick = vi.fn();
    registerSystemNotificationClickHandler(onClick);

    showWebNotification(payload());
    created[0]?.instance.onclick?.();

    expect(focus).toHaveBeenCalledTimes(1);
    expect(created[0]?.instance.close).toHaveBeenCalledTimes(1);
    expect(onClick).toHaveBeenCalledWith(payload());
  });

  it("swallows constructors that throw (e.g. service-worker-only engines)", () => {
    class ThrowingNotification {
      static permission: NotificationPermission = "granted";
      constructor() {
        throw new Error("requires a ServiceWorkerRegistration");
      }
    }
    installWindow(ThrowingNotification);
    expect(() => showWebNotification(payload())).not.toThrow();
  });
});
