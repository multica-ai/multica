import { useEffect, useMemo, useRef } from "react";
import { AppState, type AppStateStatus } from "react-native";
import { useQueryClient } from "@tanstack/react-query";
import NetInfo from "@react-native-community/netinfo";

import {
  createCoalescedRefresher,
  refreshActiveQueries,
} from "@/data/query-refresh";
import {
  isOffline,
  shouldRefreshOnAppStateChange,
  shouldRefreshOnConnectivityChange,
  type ConnectivitySnapshot,
} from "@/data/network-transitions";
import { useConnectivity } from "./use-connectivity";

/**
 * Turns the two "the world may have changed while we weren't looking" events
 * into one active-query refetch. Renders nothing; mounted under the
 * QueryClientProvider in the root layout.
 */
export function NetworkRefreshBridge() {
  const queryClient = useQueryClient();
  const connectivity = useConnectivity();
  const previousConnectivity = useRef<ConnectivitySnapshot | null>(null);
  const connectivityRef = useRef(connectivity);
  const previousAppState = useRef<AppStateStatus | null>(
    AppState.currentState ?? null,
  );

  const refresh = useMemo(
    () =>
      createCoalescedRefresher({
        refresh: () => {
          void refreshActiveQueries(queryClient);
        },
      }),
    [queryClient],
  );

  useEffect(() => {
    connectivityRef.current = connectivity;
  }, [connectivity]);

  useEffect(() => {
    const previous = previousConnectivity.current;
    previousConnectivity.current = connectivity;
    if (
      shouldRefreshOnConnectivityChange(previous, connectivity) &&
      !isOffline(connectivity)
    ) {
      refresh();
    }
  }, [connectivity, refresh]);

  useEffect(() => {
    // iOS can suspend the app across a network transition without emitting
    // the NetInfo change that would move onlineManager back to online. That
    // leaves queries paused and the offline banner up even though the phone
    // has reconnected, so force a fresh reading on mount and on foreground.
    void NetInfo.refresh().catch(() => {});

    const subscription = AppState.addEventListener(
      "change",
      (next: AppStateStatus) => {
        if (next === "active") {
          void NetInfo.refresh().catch(() => {});
        }
        const previous = previousAppState.current;
        previousAppState.current = next;
        if (
          shouldRefreshOnAppStateChange(previous, next) &&
          !isOffline(connectivityRef.current)
        ) {
          refresh();
        }
      },
    );
    return () => subscription.remove();
  }, [refresh]);

  return null;
}
