import type { ReactNode } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { floatShadow, radius, useTheme } from "../theme";
import { Txt } from "./Text";

/** Grey pill for one issue property, inside a ChipGroup. */
export function Chip({ icon, label, onPress }: { icon?: ReactNode; label?: string; onPress?: () => void }) {
  const palette = useTheme();
  return (
    <Pressable
      accessibilityRole={onPress ? "button" : undefined}
      onPress={onPress}
      disabled={!onPress}
      style={({ pressed }) => [
        styles.chip,
        label ? styles.chipLabelled : styles.chipIcon,
        { backgroundColor: palette.fill, opacity: pressed ? 0.6 : 1 },
      ]}
    >
      {icon}
      {label ? (
        <Txt variant="chip" numberOfLines={1}>
          {label}
        </Txt>
      ) : null}
    </Pressable>
  );
}

/** The light grey tray that holds an issue's properties. */
export function ChipGroup({ children }: { children: ReactNode }) {
  const palette = useTheme();
  return <View style={[styles.group, { backgroundColor: palette.grouped }]}>{children}</View>;
}

interface PillProps {
  icon?: ReactNode;
  children: ReactNode;
  onPress?: () => void;
  floating?: boolean;
  accessibilityLabel?: string;
}

/** Action pill: stop a run, open a PR, quick replies. */
export function Pill({ icon, children, onPress, floating = false, accessibilityLabel }: PillProps) {
  const palette = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      onPress={onPress}
      style={({ pressed }) => [
        styles.pill,
        floating
          ? [{ backgroundColor: palette.float, height: 36, borderRadius: 18, paddingHorizontal: 14 }, floatShadow(palette)]
          : { backgroundColor: palette.fill },
        { opacity: pressed ? 0.6 : 1 },
      ]}
    >
      {icon}
      {typeof children === "string" ? <Txt variant="chip">{children}</Txt> : children}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  chip: { height: 30, borderRadius: radius.chip, flexDirection: "row", alignItems: "center", gap: 6 },
  chipLabelled: { paddingLeft: 8, paddingRight: 10 },
  chipIcon: { width: 30, justifyContent: "center" },
  group: { borderRadius: radius.group, padding: 8, flexDirection: "row", flexWrap: "wrap", gap: 5, alignItems: "center" },
  pill: { height: 30, borderRadius: 15, flexDirection: "row", alignItems: "center", gap: 6, paddingHorizontal: 12, alignSelf: "flex-start" },
});
