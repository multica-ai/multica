/**
 * Pure transition predicates for the AppState / NetInfo bridge.
 *
 * Kept free of React Native imports so the transition contract is testable in
 * the Node Vitest lane. The caller owns the subscriptions and passes the
 * previous and next values in.
 */
import type { AppStateStatus } from "react-native";

export interface ConnectivitySnapshot {
  isConnected: boolean | null;
  isInternetReachable: boolean | null;
}

export function isOffline(snapshot: ConnectivitySnapshot): boolean {
  // `isConnected` is the same signal TanStack's onlineManager uses to pause
  // queries. Keep the banner aligned with it: treating a false
  // `isInternetReachable` probe as "offline" can strand the banner on a
  // network that works (captive portal, VPN, or a blocked reachability URL),
  // and would contradict a UI that is still receiving data.
  return snapshot.isConnected === false;
}

export function shouldRefreshOnAppStateChange(
  previous: AppStateStatus | null | undefined,
  next: AppStateStatus,
): boolean {
  // No previous state means this is the initial AppState read, not a
  // foreground transition; the first requests are already in flight.
  if (!previous) return false;
  return previous !== "active" && next === "active";
}

export function shouldRefreshOnConnectivityChange(
  previous: ConnectivitySnapshot | null,
  next: ConnectivitySnapshot,
): boolean {
  // NetInfo's first event is the initial state, not an offline → online edge.
  if (!previous) return false;
  return (
    isOffline(previous) &&
    next.isConnected === true &&
    next.isInternetReachable !== false
  );
}
