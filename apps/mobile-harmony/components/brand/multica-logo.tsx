/**
 * HarmonyOS port of apps/mobile/components/brand/multica-logo.tsx.
 * Multica wordmark / sigil — keep the silhouette in sync with
 * docs/assets/logo-light.svg (an 8-armed asterisk: four 10×91-unit bars
 * crossing at the center on the 80-unit viewBox, rotated 45° apart).
 *
 * react-native-svg is not wired on this platform (renders blank), so the
 * polygon is redrawn as four crossed rounded-end-free View bars sized from
 * the same geometry (bar width ≈ 0.11×size, length ≈ 0.99×size). The
 * original polygon is a hair off-center (vertical arm midpoint y 35.5 in
 * the transformed viewBox); the port centers it for simplicity — invisible
 * at login-screen sizes.
 *
 * Callers may pass `color` explicitly; the default is the theme foreground
 * so dark mode flips automatically (useThemeColors instead of the iOS
 * file's useColorScheme + THEME combo — same resolved value).
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import { useThemeColors } from "@/lib/use-theme-colors";

interface MulticaLogoProps {
  size?: number;
  color?: string;
}

// Bar proportions derived from the source polygon: 10 wide, 91 long on the
// 80-unit viewBox after its translate(5, 5.5) scale(0.87).
const BAR_WIDTH_RATIO = 0.11;
const BAR_LENGTH_RATIO = 0.99;
const ARM_ROTATIONS = [0, 45, 90, 135];

export function MulticaLogo({ size = 48, color }: MulticaLogoProps) {
  const c = useThemeColors();
  const resolvedColor = color ?? c.foreground;

  return (
    <View style={[styles.canvas, { width: size, height: size }]}>
      {ARM_ROTATIONS.map((deg) => (
        <View
          key={deg}
          style={[
            styles.arm,
            {
              width: size * BAR_WIDTH_RATIO,
              height: size * BAR_LENGTH_RATIO,
              backgroundColor: resolvedColor,
              transform: [{ rotate: `${deg}deg` }],
            },
          ]}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  canvas: { position: "relative", alignItems: "center", justifyContent: "center" },
  arm: { position: "absolute" },
});
