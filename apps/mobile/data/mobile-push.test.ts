// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import { invalidateSessionEpoch } from "./session-epoch";

const native = vi.hoisted(() => ({
  getPermissionsAsync: vi.fn(), requestPermissionsAsync: vi.fn(),
  getExpoPushTokenAsync: vi.fn(), setNotificationHandler: vi.fn(),
}));
const registration = vi.hoisted(() => ({ registerPushToken: vi.fn(), unregisterPushToken: vi.fn() }));
vi.mock("react-native", () => ({ Platform: { OS: "ios" } }));
vi.mock("expo-constants", () => ({ default: { expoConfig: { extra: { eas: { projectId: "project" } } } } }));
vi.mock("expo-notifications", () => ({ ...native, IosAuthorizationStatus: { PROVISIONAL: 3 } }));
vi.mock("./push-registration", () => registration);
import { registerMobilePush, pushWorkspace } from "./mobile-push";

beforeEach(() => {
  vi.clearAllMocks();
  native.getPermissionsAsync.mockResolvedValue({ granted: true });
  native.getExpoPushTokenAsync.mockResolvedValue({ data: "ExpoPushToken[token]" });
  registration.registerPushToken.mockResolvedValue(undefined);
});

describe("mobile push", () => {
  it("registers granted permissions without another prompt", async () => {
    expect(await registerMobilePush()).toBe(true);
    expect(native.requestPermissionsAsync).not.toHaveBeenCalled();
    expect(native.getExpoPushTokenAsync).toHaveBeenCalledWith({ projectId: "project" });
    expect(registration.registerPushToken).toHaveBeenCalledWith("ExpoPushToken[token]", expect.any(Number));
  });
  it("only prompts from an explicit settings action", async () => {
    native.getPermissionsAsync.mockResolvedValue({ granted: false });
    expect(await registerMobilePush()).toBe(false);
    expect(native.requestPermissionsAsync).not.toHaveBeenCalled();
    native.requestPermissionsAsync.mockResolvedValue({ granted: true });
    expect(await registerMobilePush(true)).toBe(true);
  });
  it("does not register when sign-out starts during native token acquisition", async () => {
    native.getExpoPushTokenAsync.mockImplementation(async () => {
      invalidateSessionEpoch();
      return { data: "ExpoPushToken[stale]" };
    });
    expect(await registerMobilePush()).toBe(false);
    expect(registration.registerPushToken).not.toHaveBeenCalled();
  });
  it("cleans up denied permissions and accepts provisional authorization", async () => {
    native.getPermissionsAsync.mockResolvedValue({ granted: false });
    expect(await registerMobilePush()).toBe(false);
    expect(registration.unregisterPushToken).toHaveBeenCalled();
    native.getPermissionsAsync.mockResolvedValue({ granted: false, ios: { status: 3 } });
    expect(await registerMobilePush()).toBe(true);
  });
  it.each([null, {}, {workspace: "https://evil.test"}, {workspace: "../login"}, {workspace: 3}])("rejects untrusted navigation data %j", (value) => {
    expect(pushWorkspace(value)).toBeNull();
  });
  it("opens only a workspace inbox", () => expect(pushWorkspace({workspace:"my-team", url:"https://evil.test"})).toBe("my-team"));
});
