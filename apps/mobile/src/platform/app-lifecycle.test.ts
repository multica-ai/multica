import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

type AppStateListener = (state: string) => void;
type NetInfoListener = (state: { isConnected: boolean | null }) => void;

const appState = vi.hoisted(() => ({
  currentState: "active",
  listeners: new Set<AppStateListener>(),
}));
const netInfo = vi.hoisted(() => ({ listeners: new Set<NetInfoListener>() }));

vi.mock("react-native", () => ({
  AppState: {
    get currentState() {
      return appState.currentState;
    },
    addEventListener: (_type: "change", listener: AppStateListener) => {
      appState.listeners.add(listener);
      return { remove: () => appState.listeners.delete(listener) };
    },
  },
}));

vi.mock("@react-native-community/netinfo", () => ({
  default: {
    addEventListener: (listener: NetInfoListener) => {
      netInfo.listeners.add(listener);
      return () => netInfo.listeners.delete(listener);
    },
  },
}));

const { focusManager, onlineManager } = await import("@tanstack/react-query");
const { installQueryLifecycle, subscribeAppActivity } = await import("./app-lifecycle");

function moveTo(state: string) {
  appState.currentState = state;
  for (const listener of appState.listeners) listener(state);
}

beforeEach(() => {
  appState.currentState = "active";
});

afterEach(() => {
  appState.listeners.clear();
  netInfo.listeners.clear();
});

describe("subscribeAppActivity", () => {
  it("fires when the app comes back to the foreground, not while it stays there", () => {
    const onActivity = vi.fn();
    const stop = subscribeAppActivity(onActivity);

    moveTo("active");
    expect(onActivity).not.toHaveBeenCalled();

    moveTo("background");
    moveTo("active");
    expect(onActivity).toHaveBeenCalledTimes(1);

    stop();
    moveTo("background");
    moveTo("active");
    expect(onActivity).toHaveBeenCalledTimes(1);
  });
});

describe("installQueryLifecycle", () => {
  it("drives React Query focus and connectivity from the app", () => {
    installQueryLifecycle();

    moveTo("background");
    expect(focusManager.isFocused()).toBe(false);
    moveTo("active");
    expect(focusManager.isFocused()).toBe(true);

    for (const listener of netInfo.listeners) listener({ isConnected: false });
    expect(onlineManager.isOnline()).toBe(false);
    for (const listener of netInfo.listeners) listener({ isConnected: null });
    expect(onlineManager.isOnline()).toBe(true);
  });
});
