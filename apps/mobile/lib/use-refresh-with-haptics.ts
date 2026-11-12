import { useCallback } from "react";
import * as Haptics from "expo-haptics";

/**
 * Wraps a pull-to-refresh callback so the gesture ends with the same light
 * impact the rest of the app uses for completed actions. Failures still
 * produce the haptic: the refresh completed even if the request did not.
 */
export function useRefreshWithHaptics(
  refresh: () => Promise<unknown> | unknown,
): () => Promise<void> {
  return useCallback(async () => {
    try {
      await refresh();
    } finally {
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(
        () => {},
      );
    }
  }, [refresh]);
}
