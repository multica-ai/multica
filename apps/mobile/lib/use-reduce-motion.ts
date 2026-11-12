import { useEffect, useState } from "react";
import { AccessibilityInfo } from "react-native";

/**
 * Tracks the iOS "Reduce Motion" setting. Animations that exist only for
 * polish should render their final frame instead of looping when this is on.
 */
export function useReduceMotion(): boolean {
  const [reduceMotion, setReduceMotion] = useState(false);

  useEffect(() => {
    let mounted = true;

    void AccessibilityInfo.isReduceMotionEnabled()
      .then((enabled) => {
        if (mounted) setReduceMotion(enabled);
      })
      .catch(() => {
        // Accessibility APIs are unavailable on some simulators; keep the
        // animated default.
      });

    const subscription = AccessibilityInfo.addEventListener(
      "reduceMotionChanged",
      setReduceMotion,
    );

    return () => {
      mounted = false;
      subscription.remove();
    };
  }, []);

  return reduceMotion;
}
