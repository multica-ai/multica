/**
 * Temporary stand-in for screens that have not been ported from apps/mobile
 * yet. Every placeholder is expected to disappear as the corresponding
 * screen lands (see apps/mobile-harmony/README.md roadmap).
 */
import React from "react";
import { StyleSheet, Text, View } from "react-native";
import { useSafeAreaInsets } from "@/lib/safe-area";
import { Icon } from "@/components/ui/icon";
import { useThemeColors } from "@/lib/use-theme-colors";

export function PlaceholderScreen({
  title,
  icon = "sparkles-outline",
}: {
  title: string;
  icon?: string;
}) {
  const c = useThemeColors();
  const insets = useSafeAreaInsets();
  return (
    <View
      style={[
        styles.screen,
        { backgroundColor: c.background, paddingTop: insets.top },
      ]}
    >
      <View style={styles.center}>
        <Icon name={icon} size={40} color={c.mutedForeground} />
        <Text style={[styles.title, { color: c.foreground }]}>{title}</Text>
        <Text style={[styles.subtitle, { color: c.mutedForeground }]}>
          Not ported from apps/mobile yet
        </Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  center: { flex: 1, alignItems: "center", justifyContent: "center", gap: 8 },
  title: { fontSize: 17, fontWeight: "600" },
  subtitle: { fontSize: 13 },
});
