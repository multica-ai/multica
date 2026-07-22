/**
 * Empty-state surface shown when the active session has no messages.
 *
 * Two modes mirror web (packages/views/chat/components/chat-window.tsx
 * `EmptyState`):
 *
 *   - first-time (the workspace has never started a chat) → educate. Tell
 *     the user what chat is for; don't surface starter prompts yet, they
 *     presume context the user doesn't have.
 *   - returning (at least one prior session exists) → starter prompts.
 *     Three taps, three common workflows; tapping prefills the composer
 *     draft so the user can edit before sending.
 *
 * Copy mirrors the web `chat.json` namespace 1:1. Mobile doesn't have
 * i18n yet so the strings are inlined in English — when mobile adopts
 * i18n the lookup keys (`empty_state.first_time_title` etc.) are already
 * established on the web side, so the migration is a literal
 * key-by-key swap.
 */
import { View } from "react-native";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";

interface Props {
  hasSessions: boolean;
  agentName?: string;
  onPickPrompt: (text: string) => void;
}

export function ChatEmptyState({ hasSessions, agentName, onPickPrompt }: Props) {
  const { t } = useTranslation("chat");

  const STARTER_PROMPTS: { icon: string; text: string }[] = [
    { icon: "📋", text: t("empty_state.starter_prompts.list_issues") },
    { icon: "📝", text: t("empty_state.starter_prompts.summarize_today") },
    { icon: "💡", text: t("empty_state.starter_prompts.plan_next") },
  ];

  // First-time experience: educate before suggesting actions. Starter
  // prompts here would presume the user already knows what chat is for.
  if (!hasSessions) {
    return (
      <View className="flex-1 items-center justify-center px-6 py-8">
        <View className="max-w-xs items-center gap-3">
          <Text className="text-base font-semibold text-foreground text-center">
            {t("empty_state.first_time.title")}
          </Text>
          <Text className="text-sm text-muted-foreground text-center">
            <Text className="text-sm text-muted-foreground">
              {t("empty_state.first_time.subtitle_prefix")}
            </Text>
            <Text className="text-sm font-medium text-foreground">
              {t("empty_state.first_time.subtitle_highlight")}
            </Text>
            <Text className="text-sm text-muted-foreground">
              {t("empty_state.first_time.subtitle_suffix")}
            </Text>
          </Text>
          <Text className="text-sm text-muted-foreground text-center">
            {t("empty_state.first_time.description")}
          </Text>
        </View>
      </View>
    );
  }

  // Returning user: starter prompts are the fastest path back to action.
  const title = agentName
    ? t("empty_state.returning.title_named", { name: agentName })
    : t("empty_state.returning.title_default");
  return (
    <View className="flex-1 items-center justify-center px-6 py-8 gap-5">
      <View className="items-center gap-1">
        <Text className="text-base font-semibold text-foreground text-center">
          {title}
        </Text>
        <Text className="text-sm text-muted-foreground text-center">
          {t("empty_state.returning.subtitle")}
        </Text>
      </View>
      <View className="w-full max-w-xs gap-2">
        {STARTER_PROMPTS.map((p) => (
          <Button
            key={p.text}
            variant="outline"
            onPress={() => onPickPrompt(p.text)}
            className="h-auto justify-start px-3 py-2.5"
            accessibilityLabel={p.text}
          >
            <Text className="text-sm text-foreground">
              <Text className="text-sm">{p.icon}  </Text>
              {p.text}
            </Text>
          </Button>
        ))}
      </View>
    </View>
  );
}
