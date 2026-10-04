/**
 * HarmonyOS port of apps/mobile/components/ui/pulse-dot.tsx — slow
 * brand-coloured pulse, opacity oscillating on a 2-second cycle (1s in +
 * 1s out). The iOS version drives it with Reanimated's withRepeat;
 * Reanimated is not wired into this app's Babel pipeline, so the identical
 * animation runs on RN's core Animated (see bottom-sheet.tsx for the same
 * call shape).
 *
 * Used by the in-card "Working" row and the ambient header badge on the
 * iOS side; screens land here as they are ported.
 *
 * Colour is the workspace `brand` token, matching the "in-progress / live"
 * semantic used everywhere else. **DO NOT** use the `success` token here —
 * green means "completed", not "running" (Apple HIG / shadcn convention).
 */
import React, { useEffect, useRef } from "react";
import { Animated, Easing } from "react-native";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  /** Diameter in pt. Default 8 (matches the in-card row). */
  size?: number;
}

export function PulseDot({ size = 8 }: Props) {
  const c = useThemeColors();
  const opacity = useRef(new Animated.Value(0.3)).current;

  useEffect(() => {
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(opacity, {
          toValue: 1,
          duration: 1000,
          easing: Easing.inOut(Easing.quad),
          useNativeDriver: true,
        }),
        Animated.timing(opacity, {
          toValue: 0.3,
          duration: 1000,
          easing: Easing.inOut(Easing.quad),
          useNativeDriver: true,
        }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [opacity]);

  return (
    <Animated.View
      style={[
        {
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor: c.brand,
        },
        { opacity },
      ]}
    />
  );
}
