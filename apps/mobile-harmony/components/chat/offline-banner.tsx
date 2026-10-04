/**
 * Inline notice rendered above the chat input when the active agent's
 * runtime isn't reachable. HarmonyOS port of
 * apps/mobile/components/chat/offline-banner.tsx (itself a mirror of
 * packages/views/chat/components/offline-banner.tsx).
 *
 * Two states render copy:
 *   - `unstable` (runtime offline < 5 min) → amber, "may reconnect"
 *   - `offline`  (runtime offline ≥ 5 min) → muted, "won't run until back"
 *
 * Loading silence: `undefined` and the implicit "online" case render nothing.
 * The chat composer never sees a speculative offline flash during the cold
 * fetch window — copy only appears when there's a real-world implication for
 * the message the user is about to send.
 *
 * Platform deltas: NativeWind classes → StyleSheet styles (translucent
 * shades go through withAlpha because THEME tokens are opaque hsl strings);
 * @expo/vector-icons → the ported <Icon> font glyph.
 */
import { StyleSheet, View } from "react-native";
import type { AgentAvailability } from "@multica/core/agents";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";

interface Props {
  /** Display name for the copy. */
  agentName?: string;
  /**
   * Resolved presence availability. Pass `undefined` to suppress the banner
   * — we only surface known offline / unstable states, never speculative
   * copy during loading.
   */
  availability: AgentAvailability | undefined;
}

export function OfflineBanner({ agentName, availability }: Props) {
  const c = useThemeColors();
  if (availability !== "offline" && availability !== "unstable") return null;
  const name = agentName?.trim() || "This agent";
  const s = styles(c);

  if (availability === "unstable") {
    return (
      <View style={s.unstableWrap}>
        <Icon name="alert-circle-outline" size={14} color={c.warning} />
        <Text style={s.unstableText} numberOfLines={1}>
          {name} may have just disconnected — your message will queue.
        </Text>
      </View>
    );
  }

  return (
    <View style={s.offlineWrap}>
      <Icon name="cloud-offline-outline" size={14} color={c.mutedForeground} />
      <Text style={s.offlineText} numberOfLines={1}>
        {name} is offline. Messages will wait until its runtime is back.
      </Text>
    </View>
  );
}

const styles = (c: ReturnType<typeof useThemeColors>) =>
  StyleSheet.create({
    // mx-3 mb-1.5 flex-row items-center gap-1.5 rounded-md bg-warning/15 px-2.5 py-1.5
    unstableWrap: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      marginHorizontal: 12,
      marginBottom: 6,
      borderRadius: 6,
      backgroundColor: withAlpha(c.warning, 0.15),
      paddingHorizontal: 10,
      paddingVertical: 6,
    },
    unstableText: { flex: 1, fontSize: 12, color: c.warning },
    // mx-3 mb-1.5 flex-row items-center gap-1.5 rounded-md bg-muted px-2.5 py-1.5
    offlineWrap: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      marginHorizontal: 12,
      marginBottom: 6,
      borderRadius: 6,
      backgroundColor: c.muted,
      paddingHorizontal: 10,
      paddingVertical: 6,
    },
    offlineText: { flex: 1, fontSize: 12, color: c.mutedForeground },
  });
