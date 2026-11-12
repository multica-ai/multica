// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  isOffline,
  shouldRefreshOnAppStateChange,
  shouldRefreshOnConnectivityChange,
} from "./network-transitions";

const online = { isConnected: true, isInternetReachable: true };
const offline = { isConnected: false, isInternetReachable: false };
const noInternet = { isConnected: true, isInternetReachable: false };
const unknown = { isConnected: null, isInternetReachable: null };

describe("shouldRefreshOnAppStateChange", () => {
  it("refreshes on background → active", () => {
    expect(shouldRefreshOnAppStateChange("background", "active")).toBe(true);
  });

  it("does not refresh on the initial state or while already active", () => {
    expect(shouldRefreshOnAppStateChange(null, "active")).toBe(false);
    expect(shouldRefreshOnAppStateChange("active", "active")).toBe(false);
    expect(shouldRefreshOnAppStateChange("active", "background")).toBe(false);
  });
});

describe("shouldRefreshOnConnectivityChange", () => {
  it("refreshes only on the offline → online edge", () => {
    expect(shouldRefreshOnConnectivityChange(offline, online)).toBe(true);
    expect(shouldRefreshOnConnectivityChange(noInternet, online)).toBe(true);
    expect(shouldRefreshOnConnectivityChange(online, offline)).toBe(false);
    expect(shouldRefreshOnConnectivityChange(online, online)).toBe(false);
  });

  it("ignores the first NetInfo event", () => {
    expect(shouldRefreshOnConnectivityChange(null, online)).toBe(false);
    expect(shouldRefreshOnConnectivityChange(null, offline)).toBe(false);
  });

  it("waits through an unresolved state instead of refreshing early", () => {
    expect(shouldRefreshOnConnectivityChange(offline, unknown)).toBe(false);
  });
});

describe("isOffline", () => {
  it("treats a missing internet route as offline, but not an unknown state", () => {
    expect(isOffline(offline)).toBe(true);
    expect(isOffline(noInternet)).toBe(true);
    expect(isOffline(online)).toBe(false);
    expect(isOffline(unknown)).toBe(false);
  });
});
