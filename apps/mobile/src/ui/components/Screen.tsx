import type { ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useTheme } from "../theme";

/** A screen's surface. Side insets keep content clear of a vertical bar, such as iPhone Duo's. */
export function Screen({ children }: { children: ReactNode }) {
  const palette = useTheme();
  const insets = useSafeAreaInsets();
  return <View style={[styles.screen, { backgroundColor: palette.background, paddingLeft: insets.left, paddingRight: insets.right }]}>{children}</View>;
}

/** Room below scrolling content for the floating bottom controls. */
export const BOTTOM_CLEARANCE = 132;

const styles = StyleSheet.create({
  screen: { flex: 1 },
});
