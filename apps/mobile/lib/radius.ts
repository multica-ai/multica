import type { ViewStyle } from "react-native";
import radiusTokens from "./radius-tokens.json";

/** Shared by NativeWind utilities and inline React Native styles. */
export const MOBILE_RADIUS = radiusTokens;

/** iOS-style continuous curves for mobile surfaces with large corner radii. */
export const continuousCorners = {
  borderCurve: "continuous",
} satisfies ViewStyle;
