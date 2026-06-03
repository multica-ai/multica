import { beforeEach, describe, expect, it, vi } from "vitest";

const expoNotifications = vi.hoisted(() => ({
  addNotificationResponseReceivedListener: vi.fn(),
  addPushTokenListener: vi.fn(),
  getDevicePushTokenAsync: vi.fn(),
  requestPermissionsAsync: vi.fn(),
}));

vi.mock("react-native", () => ({
  Platform: { OS: "ios" },
}));

vi.mock("expo-notifications", () => expoNotifications);

import {
  extractNotificationResponseUrl,
  initializeApnsPush,
  normalizeApnsDeviceToken,
} from "./apns-push";

describe("apns-push", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("normalizes iOS device tokens", () => {
    expect(normalizeApnsDeviceToken({ type: "ios", data: " token-1 " })).toBe("token-1");
    expect(normalizeApnsDeviceToken({ type: "android", data: "token-1" })).toBeNull();
    expect(normalizeApnsDeviceToken({ type: "ios", data: "   " })).toBeNull();
    expect(normalizeApnsDeviceToken(null)).toBeNull();
  });

  it("does not fetch a token when notification permission is denied", async () => {
    expoNotifications.requestPermissionsAsync.mockResolvedValue({
      granted: false,
      status: "denied",
    });

    await expect(initializeApnsPush()).resolves.toBeNull();
    expect(expoNotifications.getDevicePushTokenAsync).not.toHaveBeenCalled();
  });

  it("returns the iOS APNs token when permission is granted", async () => {
    expoNotifications.requestPermissionsAsync.mockResolvedValue({
      granted: true,
      status: "granted",
    });
    expoNotifications.getDevicePushTokenAsync.mockResolvedValue({
      type: "ios",
      data: " apns-token ",
    });

    await expect(initializeApnsPush()).resolves.toBe("apns-token");
  });

  it("extracts deep link URLs from notification responses", () => {
    expect(
      extractNotificationResponseUrl({
        notification: {
          request: {
            content: {
              data: {
                url: " wujieai-multicam://issues/issue-1?commentId=comment-1 ",
              },
            },
          },
        },
      }),
    ).toBe("wujieai-multicam://issues/issue-1?commentId=comment-1");

    expect(
      extractNotificationResponseUrl({
        notification: {
          request: {
            content: {
              data: {
                url: "https://example.com/issues/issue-1",
              },
            },
          },
        },
      }),
    ).toBeNull();
  });
});
