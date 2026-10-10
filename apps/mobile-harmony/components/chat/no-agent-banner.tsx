/**
 * Banner shown when the workspace has zero usable agents for the current
 * user. HarmonyOS port of apps/mobile/components/chat/no-agent-banner.tsx
 * (mirror of packages/views/chat/components/no-agent-banner.tsx on web).
 *
 * Platform delta: iOS navigated via expo-router (`/${wsSlug}/more/agents`);
 * this screen has no router — the tap flows through the `onOpenAgents`
 * callback the shell wires. Without the callback the banner stays
 * informational (no push target exists in the current navigation graph).
 */
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { Text } from "@/components/ui/text";

interface Props {
  /** Opens the agents list (iOS pushed `/${wsSlug}/more/agents`). */
  onOpenAgents?: () => void;
}

export function NoAgentBanner({ onOpenAgents }: Props) {
  const c = useThemeColors();

  const body = (
    <>
      <Text style={styles.title}>No agents available</Text>
      <Text style={[styles.subtitle, { color: c.mutedForeground }]}>
        Add or enable an agent in More → Agents to start chatting.
      </Text>
    </>
  );

  if (!onOpenAgents) {
    return (
      <View
        style={[
          styles.banner,
          {
            backgroundColor: withAlpha(c.secondary, 0.5),
            borderColor: c.border,
          },
        ]}
      >
        {body}
      </View>
    );
  }

  return (
    <Pressable
      onPress={onOpenAgents}
      style={({ pressed }) => [
        styles.banner,
        {
          backgroundColor: withAlpha(c.secondary, 0.5),
          borderColor: c.border,
          opacity: pressed ? 0.8 : 1,
        },
      ]}
      accessibilityRole="button"
      accessibilityLabel="No agents available, open agents settings"
    >
      {body}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  // mx-3 mt-2 mb-1 rounded-xl border border-border bg-secondary/50 px-3 py-2
  banner: {
    marginHorizontal: 12,
    marginTop: 8,
    marginBottom: 4,
    borderRadius: 12,
    borderWidth: 1,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  title: { fontSize: 14, fontWeight: "500" },
  subtitle: { fontSize: 12, marginTop: 2 },
});
