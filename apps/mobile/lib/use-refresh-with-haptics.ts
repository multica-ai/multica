import { useCallback, useState } from "react";
import * as Haptics from "expo-haptics";

/**
 * Owns the user-initiated pull-to-refresh state.
 *
 * `RefreshControl.refreshing` must only reflect a gesture the user actually
 * started. Driving it from TanStack's `isRefetching` also lights up the top
 * spinner for WebSocket invalidations and foreground/reconnect refreshes; on
 * iOS a programmatically shown refresh control can sit there without its
 * native animation and then never clear if that background request stalls.
 *
 * The haptic fires when the gesture's request settles, whether or not it
 * succeeded.
 */
export function useRefreshWithHaptics(
  refresh: () => Promise<unknown> | unknown,
): { refreshing: boolean; onRefresh: () => Promise<void> } {
  const [refreshing, setRefreshing] = useState(false);

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await refresh();
    } finally {
      setRefreshing(false);
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(
        () => {},
      );
    }
  }, [refresh]);

  return { refreshing, onRefresh };
}
