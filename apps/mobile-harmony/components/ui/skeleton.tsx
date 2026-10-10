/**
 * HarmonyOS port of apps/mobile/components/ui/skeleton.tsx. The iOS
 * version is a View with bg-accent + NativeWind's animate-pulse (a 2s CSS
 * opacity keyframe); NativeWind className styling regressed on RNOH 0.82,
 * so the pulse is reproduced with RN's core Animated loop
 * (opacity 1 ↔ 0.5, 1s each way) and the color comes from the theme accent
 * token. No Reanimated — it is not wired into this app's Babel pipeline.
 */
import React, { useEffect, useRef } from "react";
import {
  Animated,
  Easing,
  StyleSheet,
  View,
  type ViewProps,
} from "react-native";
import { useThemeColors } from "@/lib/use-theme-colors";

function Skeleton({ style, ...props }: ViewProps & React.RefAttributes<View>) {
  const c = useThemeColors();
  const opacity = useRef(new Animated.Value(1)).current;

  useEffect(() => {
    const pulse = Animated.sequence([
      Animated.timing(opacity, {
        toValue: 0.5,
        duration: 1000,
        easing: Easing.inOut(Easing.quad),
        useNativeDriver: true,
      }),
      Animated.timing(opacity, {
        toValue: 1,
        duration: 1000,
        easing: Easing.inOut(Easing.quad),
        useNativeDriver: true,
      }),
    ]);
    const loop = Animated.loop(pulse);
    loop.start();
    return () => loop.stop();
  }, [opacity]);

  const animatedStyle = { opacity };

  return (
    <Animated.View
      style={[styles.base, { backgroundColor: c.accent }, animatedStyle, style]}
      {...props}
    />
  );
}

const styles = StyleSheet.create({
  base: { borderRadius: 6 },
});

export { Skeleton };
