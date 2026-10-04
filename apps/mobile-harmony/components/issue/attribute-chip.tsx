/**
 * HarmonyOS port of apps/mobile/components/issue/attribute-chip.tsx.
 * Generic attribute chip used in the issue-detail header. Each chip pairs
 * an icon node (StatusIcon, PriorityIcon, ActorAvatar, emoji, …) with a
 * textual label. Filled = the property has a value; dimmed = empty
 * placeholder ("Label", "Project", …).
 *
 * The chip becomes a Pressable when `onPress` is provided. Without onPress
 * it renders as a plain View (read-only chips).
 *
 * NativeWind className styling is not part of this app (AGENTS.md): the
 * variant union resolves through a StyleSheet lookup keyed on theme colors.
 */
import React from "react";
import { StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { ReactNode } from "react";
import { Text } from "@/components/ui/text";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

export type AttributeChipVariant = "filled" | "dimmed";

interface Props {
  icon: ReactNode;
  label: string;
  variant?: AttributeChipVariant;
  onPress?: () => void;
  style?: StyleProp<ViewStyle>;
}

export function AttributeChip({
  icon,
  label,
  variant = "filled",
  onPress,
  style,
}: Props) {
  const c = useThemeColors();
  const filled = variant === "filled";

  const container: ViewStyle = filled
    ? {
        borderColor: c.border,
        backgroundColor: withAlpha(c.secondary, 0.6),
      }
    : {
        borderStyle: "dashed",
        borderColor: withAlpha(c.mutedForeground, 0.3),
        backgroundColor: "transparent",
      };

  const inner = (
    <>
      {icon}
      <Text
        style={[
          // text-xs
          styles.label,
          { color: filled ? c.foreground : withAlpha(c.mutedForeground, 0.7) },
        ]}
        numberOfLines={1}
      >
        {label}
      </Text>
    </>
  );

  if (onPress) {
    return (
      <Pressable
        onPress={onPress}
        hitSlop={4}
        accessibilityRole="button"
        accessibilityLabel={label}
        style={({ pressed }) => [
          // flex-row items-center gap-1.5 rounded-full border px-2.5 py-1
          styles.chip,
          container,
          pressed && filled ? { backgroundColor: c.secondary } : null,
          style,
        ]}
      >
        {inner}
      </Pressable>
    );
  }
  return (
    <View style={[styles.chip, container, style]}>{inner}</View>
  );
}

const styles = StyleSheet.create({
  // flex-row items-center gap-1.5 rounded-full border px-2.5 py-1
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    borderRadius: 999,
    borderWidth: 1,
    paddingHorizontal: 10,
    paddingVertical: 4,
  },
  // text-xs
  label: { fontSize: 12 },
});
