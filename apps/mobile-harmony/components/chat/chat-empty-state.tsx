/**
 * Empty-state surface shown when the active session has no messages.
 * HarmonyOS port of apps/mobile/components/chat/chat-empty-state.tsx.
 *
 * Two modes mirror web (packages/views/chat/components/chat-window.tsx
 * `EmptyState`):
 *
 *   - first-time (the workspace has never started a chat) → educate and
 *     offer conversation starters so the composer is not a blank dead end.
 *   - returning (at least one prior session exists) → lead with starter
 *     starters. Tapping prefills the draft so the user can edit before sending.
 *
 * Copy mirrors the web `chat.json` namespace 1:1; strings stay inlined in
 * English exactly like the iOS file (mobile doesn't have i18n yet).
 *
 * Platform delta: NativeWind classes → StyleSheet styles; the starter
 * buttons keep the outline variant with left-aligned labels via style
 * overrides on the ported <Button>.
 */
import { StyleSheet, View } from "react-native";
import type { Agent, AgentConversationStarter } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { useThemeColors } from "@/lib/use-theme-colors";

const FALLBACK_CONVERSATION_STARTERS: AgentConversationStarter[] = [
  {
    label: "What can you help with?",
    prompt: "What are you best at helping with? Give me a concise overview.",
  },
  {
    label: "Suggest a first task",
    prompt: "Suggest three useful tasks I could delegate to you.",
  },
  {
    label: "Recommend an action",
    prompt:
      "Review what you know about my workspace and recommend a useful first action.",
  },
];

interface Props {
  hasSessions: boolean;
  agent: Agent | null;
  onPickPrompt: (text: string) => void;
}

export function ChatEmptyState({ hasSessions, agent, onPickPrompt }: Props) {
  const c = useThemeColors();
  const title = agent ? `Hi, I'm ${agent.name}` : "Chat with your agents";
  const configured = (agent?.conversation_starters ?? []).filter(
    (item) => item.label.trim() && item.prompt.trim(),
  );
  const starters =
    configured.length > 0 ? configured : FALLBACK_CONVERSATION_STARTERS;
  return (
    <View style={styles.container}>
      <View style={styles.heading}>
        <Text
          style={[styles.title, { color: c.foreground }]}
          numberOfLines={undefined}
        >
          {title}
        </Text>
        {agent?.description ? (
          <Text style={[styles.description, { color: c.mutedForeground }]}>
            {agent.description}
          </Text>
        ) : null}
        {!hasSessions ? (
          <Text style={[styles.description, { color: c.mutedForeground }]}>
            Examples fill the composer without sending.
          </Text>
        ) : null}
      </View>
      {agent ? (
        <View style={styles.starters}>
          {starters.map((item, index) => (
            <Button
              key={index}
              variant="outline"
              onPress={() => onPickPrompt(item.prompt)}
              style={styles.starterButton}
              accessibilityLabel={item.label}
            >
              <Text style={[styles.starterLabel, { color: c.foreground }]}>
                {item.label}
              </Text>
            </Button>
          ))}
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  // flex-1 items-center justify-center px-6 py-8 gap-5
  container: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
    paddingVertical: 32,
    gap: 20,
  },
  // items-center gap-1
  heading: { alignItems: "center", gap: 4 },
  // text-base font-semibold text-center
  title: { fontSize: 16, fontWeight: "600", textAlign: "center" },
  // text-sm text-muted-foreground text-center
  description: { fontSize: 14, textAlign: "center" },
  // w-full max-w-xs gap-2
  starters: { width: "100%", maxWidth: 320, gap: 8 },
  // h-auto justify-start px-3 py-2.5
  starterButton: {
    height: "auto",
    minHeight: 40,
    justifyContent: "flex-start",
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  starterLabel: { fontSize: 14, textAlign: "left" },
});
