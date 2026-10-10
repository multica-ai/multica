/**
 * HarmonyOS port of apps/mobile/components/chat/runtime-required-banner.tsx
 * — amber notice shown in place of the offline banner when the active agent
 * has no runtime bound at all. @expo/vector-icons → <Icon>; NativeWind
 * classes → StyleSheet styles.
 */
import { StyleSheet, View } from "react-native";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";

export function RuntimeRequiredBanner({ agentName }: { agentName?: string }) {
  const c = useThemeColors();
  const name = agentName?.trim() || "This agent";
  return (
    <View
      style={[
        styles.wrap,
        { backgroundColor: withAlpha(c.warning, 0.15) },
      ]}
    >
      <Icon name="server-outline" size={14} color={c.warning} />
      <Text style={[styles.text, { color: c.warning }]}>
        {name} needs a runtime before it can run. Bind one on web or desktop.
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  // mx-3 mb-1.5 flex-row items-center gap-1.5 rounded-md bg-warning/15 px-2.5 py-1.5
  wrap: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    marginHorizontal: 12,
    marginBottom: 6,
    borderRadius: 6,
    paddingHorizontal: 10,
    paddingVertical: 6,
  },
  text: { flex: 1, fontSize: 12 },
});
