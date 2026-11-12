import { useEffect, useMemo, useRef } from "react";
import { AppState, type AppStateStatus } from "react-native";
import { useQueryClient } from "@tanstack/react-query";

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
    const subscription = AppState.addEventListener(
      "change",
      (next: AppStateStatus) => {
        const previous = previousAppState.current;
        previousAppState.current = next;
        if (
          shouldRefreshOnAppStateChange(previous, next) &&
          !isOffline(connectivity)
        ) {
          refresh();
        }
      },
    );
    return () => subscription.remove();
  }, [connectivity, refresh]);

  return null;
}
