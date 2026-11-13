import NetInfo from "@react-native-community/netinfo";
import { focusManager, onlineManager } from "@tanstack/react-query";
import { AppState } from "react-native";

/**
 * Calls `onActivity` each time the app returns to the foreground. Core uses it
 * to renew the session and to reconnect realtime without waiting out its
 * backoff; iOS suspends the socket while the app is in the background.
 */
export function subscribeAppActivity(onActivity: () => void): () => void {
  let previous = AppState.currentState;
  const subscription = AppState.addEventListener("change", (next) => {
    if (next === "active" && previous !== "active") onActivity();
    previous = next;
  });
  return () => subscription.remove();
}

/**
 * React Query's defaults listen for browser focus and online events, which
 * React Native never fires. Without this, paused mutations would not resume
 * when the connection comes back.
 */
export function installQueryLifecycle(): void {
  focusManager.setEventListener((setFocused) => {
    const subscription = AppState.addEventListener("change", (state) =>
      setFocused(state === "active"),
    );
    return () => subscription.remove();
  });
  // `isConnected` is null until the first probe; treat unknown as online so
  // nothing pauses at launch.
  onlineManager.setEventListener((setOnline) =>
    NetInfo.addEventListener((state) => setOnline(state.isConnected !== false)),
  );
}
