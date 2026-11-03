// @vitest-environment node
import { expect, it, vi } from "vitest";
const updateMobilePush = vi.hoisted(() => vi.fn());
vi.mock("expo-secure-store", () => ({ getItemAsync: async () => null, setItemAsync: async () => {}, deleteItemAsync: async () => {} }));
vi.mock("./api", () => ({ api: { updateMobilePush } }));
import { registerPushToken, unregisterPushToken } from "./push-registration";
import { currentSessionEpoch, invalidateSessionEpoch } from "./session-epoch";

it("deletes after an in-flight registration and refuses stale registration", async () => {
  let finish!: () => void;
  updateMobilePush.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
  const epoch = currentSessionEpoch();
  const registration = registerPushToken("ExpoPushToken[token]", epoch);
  await vi.waitFor(() => expect(updateMobilePush).toHaveBeenCalledTimes(1));
  invalidateSessionEpoch();
  const cleanup = unregisterPushToken();
  expect(updateMobilePush).toHaveBeenCalledTimes(1);
  finish();
  await registration;
  await cleanup;
  expect(updateMobilePush.mock.calls).toEqual([["ExpoPushToken[token]", true], ["ExpoPushToken[token]", false]]);
  await registerPushToken("ExpoPushToken[stale]", epoch);
  expect(updateMobilePush).toHaveBeenCalledTimes(2);
});
