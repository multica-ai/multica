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
  return (
    snapshot.isConnected === false ||
    snapshot.isInternetReachable === false
  );
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
