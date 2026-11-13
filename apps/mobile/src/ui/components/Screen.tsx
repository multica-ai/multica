import type { ReactNode } from "react";
import { StyleSheet, View } from "react-native";
import { useTheme } from "../theme";

export function Screen({ children }: { children: ReactNode }) {
  const palette = useTheme();
  return <View style={[styles.screen, { backgroundColor: palette.background }]}>{children}</View>;
}

/** Room below scrolling content for the floating bottom controls. */
export const BOTTOM_CLEARANCE = 132;

const styles = StyleSheet.create({
  screen: { flex: 1 },
});
