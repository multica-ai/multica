"use client";

import { useCallback } from "react";
import { useNavigation } from "./context";

/**
 * Leave a deleted subject through known in-app history, or replace with fallback
 * when history is unavailable or cannot be proven safe. Never guess canGoBack or
 * use push: the dead page must not remain as a new back-stack destination.
 */
export function useBackOrReplace(): (fallback: string) => void {
  const { back, replace, canGoBack } = useNavigation();
  return useCallback(
    (fallback: string) => {
      if (canGoBack?.() === true) back();
      else replace(fallback);
    },
    [back, replace, canGoBack],
  );
}
