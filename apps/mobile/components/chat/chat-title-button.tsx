/**
 * Centred title region for a chat screen's native Stack header — shows
 * the current agent's avatar + name and the session title/subtitle.
 * `onPress` is optional: `chat/[id]` and `chat/new` render this
 * non-interactively (there's no sheet left to open — the native back
 * button already returns to the session list).
 */
import { Pressable, View } from "react-native";
import { useTranslation } from "react-i18next";
import type { Agent, ChatSession } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";

interface Props {
  currentSession: ChatSession | null;
  currentAgent: Agent | null;
  onPress?: () => void;
}

export function ChatTitleButton({
  currentSession,
  currentAgent,
  onPress,
}: Props) {
  const { t } = useTranslation("chat");
  const agentName = currentAgent?.name ?? t("title_button.default_agent_name");
  const subtitle = currentSession?.title || t("title_button.new_chat_subtitle");

  const content = (
    // -ml-2: react-native-screens' native-stack has no JS-exposed prop for
    // the Toolbar's default content-inset-after-back-button on Android (the
    // remaining gap once headerTitleAlign is "left" — see _layout.tsx). This
    // pulls our own content left to visually close roughly half of it.
    <View className="flex-row items-center gap-2 py-1 pr-2 -ml-2 rounded-lg">
      <ActorAvatar
        type={currentAgent ? "agent" : null}
        id={currentAgent?.id ?? null}
        size={40}
        showPresence
      />
      <View>
        <Text
          className="text-base font-semibold text-foreground"
          numberOfLines={1}
        >
          {agentName}
        </Text>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {subtitle}
        </Text>
      </View>
    </View>
  );

  if (!onPress) return content;

  return (
    <Pressable
      onPress={onPress}
      hitSlop={4}
      className="active:bg-secondary rounded-lg"
      accessibilityRole="button"
      accessibilityLabel={t("title_button.accessibility_label")}
    >
      {content}
    </Pressable>
  );
}
