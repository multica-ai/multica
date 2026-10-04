/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/more/agents.tsx.
 * The iOS source is itself a 12-line placeholder ("Agents coming soon."),
 * so this port stays one — the real agents surface lands with the rest of
 * the roadmap (see apps/mobile-harmony/README.md).
 */
import { StyleSheet, View } from "react-native";
import { ScreenHeader } from "@/src/screens/screen-header";
import { Text } from "@/components/ui/text";
import { useNav } from "@/src/navigation/navigator";
import { useThemeColors } from "@/lib/use-theme-colors";

export function MoreAgentsScreen() {
  const nav = useNav<{ pop: () => void }>();
  const c = useThemeColors();
  return (
    <View style={[styles.screen, { backgroundColor: c.background }]}>
      <ScreenHeader title="Agents" onBack={() => nav.pop()} />
      <View style={styles.center}>
        <Text style={[styles.message, { color: c.mutedForeground }]}>
          Agents coming soon.
        </Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  // flex-1 items-center justify-center px-6
  center: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  // text-sm text-center
  message: { fontSize: 14, textAlign: "center" },
});
