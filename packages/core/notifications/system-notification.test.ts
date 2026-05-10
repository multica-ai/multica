import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  detectWebNotificationSupport,
  isDesktopApp,
  showSystemNotification,
} from "./system-notification";

interface NotificationMock {
  title: string;
  options?: NotificationOptions;
  listeners: Map<string, EventListener>;
  close: () => void;
}

const notificationInstances: NotificationMock[] = [];

class FakeNotification {
  static permission: NotificationPermission = "default";
  static requestPermission = vi.fn();
  title: string;
  options?: NotificationOptions;
  listeners = new Map<string, EventListener>();
  close = vi.fn();

  constructor(title: string, options?: NotificationOptions) {
    this.title = title;
    this.options = options;
    notificationInstances.push(this as unknown as NotificationMock);
  }

  addEventListener(type: string, listener: EventListener) {
    this.listeners.set(type, listener);
  }
}

const originalNotification = (globalThis as { Notification?: unknown }).Notification;
const originalWindow = (globalThis as { window?: unknown }).window;

beforeEach(() => {
  notificationInstances.length = 0;
  FakeNotification.permission = "default";
  const win: Record<string, unknown> = {
    focus: vi.fn(),
    location: { assign: vi.fn() },
  };
  (globalThis as { window?: unknown }).window = win;
  (globalThis as { Notification?: unknown }).Notification = FakeNotification;
});

afterEach(() => {
  if (originalWindow === undefined) {
    delete (globalThis as { window?: unknown }).window;
  } else {
    (globalThis as { window?: unknown }).window = originalWindow;
  }
  if (originalNotification === undefined) {
    delete (globalThis as { Notification?: unknown }).Notification;
  } else {
    (globalThis as { Notification?: unknown }).Notification = originalNotification;
  }
});

describe("detectWebNotificationSupport", () => {
  it("reports api_unavailable when Notification is missing", () => {
    delete (globalThis as { Notification?: unknown }).Notification;
    expect(detectWebNotificationSupport()).toBe("api_unavailable");
  });

  it("reports permission_default when permission has not been asked", () => {
    FakeNotification.permission = "default";
    expect(detectWebNotificationSupport()).toBe("permission_default");
  });

  it("reports permission_denied when permission is denied", () => {
    FakeNotification.permission = "denied";
    expect(detectWebNotificationSupport()).toBe("permission_denied");
  });

  it("reports supported when permission is granted", () => {
    FakeNotification.permission = "granted";
    expect(detectWebNotificationSupport()).toBe("supported");
  });
});

describe("showSystemNotification", () => {
  it("uses desktopAPI when available", () => {
    const showNotification = vi.fn();
    (globalThis as { window?: { desktopAPI?: unknown } }).window = {
      desktopAPI: { showNotification },
    };

    const result = showSystemNotification({
      slug: "acme",
      itemId: "item-1",
      issueKey: "issue-1",
      title: "Hello",
      body: "World",
      inboxPath: "/acme/inbox?issue=issue-1",
    });

    expect(result).toBe("delivered_desktop");
    expect(showNotification).toHaveBeenCalledWith({
      slug: "acme",
      itemId: "item-1",
      issueKey: "issue-1",
      title: "Hello",
      body: "World",
    });
  });

  it("creates a web Notification when permission is granted", () => {
    FakeNotification.permission = "granted";

    const result = showSystemNotification({
      slug: "acme",
      itemId: "item-1",
      issueKey: "issue-1",
      title: "Hello",
      body: "World",
      inboxPath: "/acme/inbox?issue=issue-1",
    });

    expect(result).toBe("supported");
    expect(notificationInstances).toHaveLength(1);
    expect(notificationInstances[0]?.title).toBe("Hello");
    expect(notificationInstances[0]?.options).toMatchObject({
      body: "World",
      tag: "item-1",
    });
  });

  it("skips when permission is denied", () => {
    FakeNotification.permission = "denied";

    const result = showSystemNotification({
      slug: "acme",
      itemId: "item-1",
      issueKey: "issue-1",
      title: "Hello",
      body: "World",
      inboxPath: "/acme/inbox?issue=issue-1",
    });

    expect(result).toBe("permission_denied");
    expect(notificationInstances).toHaveLength(0);
  });

  it("navigates to inbox path on click", () => {
    FakeNotification.permission = "granted";
    const assign = vi.fn();
    (globalThis as { window?: unknown }).window = {
      focus: vi.fn(),
      location: { assign },
    };

    showSystemNotification({
      slug: "acme",
      itemId: "item-1",
      issueKey: "issue-1",
      title: "Hello",
      body: "World",
      inboxPath: "/acme/inbox?issue=issue-1",
    });

    const click = notificationInstances[0]?.listeners.get("click");
    expect(click).toBeTypeOf("function");
    click?.(new Event("click"));
    expect(assign).toHaveBeenCalledWith("/acme/inbox?issue=issue-1");
  });
});

describe("isDesktopApp", () => {
  it("is false when desktopAPI is missing", () => {
    expect(isDesktopApp()).toBe(false);
  });

  it("is true when desktopAPI is injected", () => {
    (globalThis as { window?: unknown }).window = {
      desktopAPI: { showNotification: vi.fn() },
    };
    expect(isDesktopApp()).toBe(true);
  });
});
