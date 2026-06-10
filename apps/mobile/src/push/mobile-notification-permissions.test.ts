import { describe, expect, it, vi } from "vitest";

vi.mock("expo-notifications", () => ({
  getPermissionsAsync: vi.fn(),
  requestPermissionsAsync: vi.fn(),
}));

vi.mock("react-native", () => ({
  Linking: { openSettings: vi.fn() },
  Platform: { OS: "ios" },
}));

import { normalizeMobileNotificationPermissionStatus } from "./mobile-notification-permissions";

describe("mobile-notification-permissions", () => {
  it("normalizes granted notification permissions", () => {
    expect(
      normalizeMobileNotificationPermissionStatus("android", {
        canAskAgain: false,
        granted: true,
        status: "granted",
      }),
    ).toEqual({
      canRequest: false,
      granted: true,
      platform: "android",
      status: "granted",
    });
  });

  it("allows requesting an undetermined notification permission", () => {
    expect(
      normalizeMobileNotificationPermissionStatus("ios", {
        canAskAgain: true,
        granted: false,
        status: "undetermined",
      }),
    ).toEqual({
      canRequest: true,
      granted: false,
      platform: "ios",
      status: "undetermined",
    });
  });

  it("marks denied notification permissions as settings-only when they cannot be requested again", () => {
    expect(
      normalizeMobileNotificationPermissionStatus("ios", {
        canAskAgain: false,
        granted: false,
        status: "denied",
      }),
    ).toEqual({
      canRequest: false,
      granted: false,
      platform: "ios",
      status: "denied",
    });
  });
});
