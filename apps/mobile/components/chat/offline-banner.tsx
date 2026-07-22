/**
 * Inline notice rendered above the chat input when the active agent's
 * runtime isn't reachable. Mirror of
 * `packages/views/chat/components/offline-banner.tsx`.
 *
 * Two states render copy:
 *   - `unstable` (runtime offline < 5 min) → amber, "may reconnect"
 *   - `offline`  (runtime offline ≥ 5 min) → muted, "won't run until back"
 *
 * Loading silence: `undefined` and the implicit "online" case render nothing.
 * The chat composer never sees a speculative offline flash during the cold
 * fetch window — copy only appears when there's a real-world implication for
 * the message the user is about to send.
 */
import { Ionicons } from "@expo/vector-icons";
import { View } from "react-native";
import { useTranslation } from "react-i18next";
import type { AgentAvailability } from "@multica/core/agents";
import { Text } from "@/components/ui/text";

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
  const { t } = useTranslation("chat");
  if (availability !== "offline" && availability !== "unstable") return null;
  const name = agentName?.trim() || t("offline_banner.default_agent_name");

  if (availability === "unstable") {
    return (
      <View className="mx-3 mb-1.5 flex-row items-center gap-1.5 rounded-md bg-warning/15 px-2.5 py-1.5">
        <Ionicons name="alert-circle-outline" size={14} color="#a16207" />
        <Text
          className="flex-1 text-xs text-warning"
          numberOfLines={1}
        >
          {t("offline_banner.unstable", { name })}
        </Text>
      </View>
    );
  }

  return (
    <View className="mx-3 mb-1.5 flex-row items-center gap-1.5 rounded-md bg-muted px-2.5 py-1.5">
      <Ionicons name="cloud-offline-outline" size={14} color="#71717a" />
      <Text
        className="flex-1 text-xs text-muted-foreground"
        numberOfLines={1}
      >
        {t("offline_banner.offline", { name })}
      </Text>
    </View>
  );
}
