import type { ViewStyle } from "react-native";

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
