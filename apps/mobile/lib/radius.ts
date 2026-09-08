import type { ViewStyle } from "react-native";

/** Mirrors the compact product radius ladder in tailwind.config.js. */
export const MOBILE_RADIUS = {
  xs: 3,
  sm: 4,
  md: 6,
  lg: 8,
  xl: 10,
  "2xl": 12,
  "3xl": 16,
} as const;

/** iOS-style continuous curves for mobile surfaces with large corner radii. */
export const continuousCorners = {
  borderCurve: "continuous",
} satisfies ViewStyle;

/** Entity avatars are proportional soft squares on every client and size. */
export function entityAvatarStyle(size: number) {
  return {
    width: size,
    height: size,
    borderRadius: Math.round(size / 4),
    borderCurve: "continuous",
  } satisfies ViewStyle;
}
