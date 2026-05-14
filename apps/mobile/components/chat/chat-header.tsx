/**
 * Chat screen top bar.
 *
 * Layout (left-to-right):
 *   - Tappable centre region: agent avatar + agent name + session title
 *     subtitle (▼ indicator). Tap → opens session sheet.
 *   - Right-side actions: ⋯ (current-session menu, only when there IS an
 *     active session — Delete in v1), + (new chat).
 *
 * Differs from ScreenHeader (`@/components/ui/screen-header`): the latter
 * is left-aligned and doesn't have a press handler on the title. Chat
 * needs a centred / tappable title-as-affordance, so this is its own
 * component rather than a ScreenHeader variant.
 *
 * Empty-state copy: when `currentSession === null` (new chat) the
 * subtitle reads "New chat" so the title region never looks broken.
 */
import { Pressable, View } from "react-native";
import type { Agent, ChatSession } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";

interface Props {
  /** Active session — `null` when on the new-chat blank state. */
  currentSession: ChatSession | null;
  /** Currently selected agent. May differ from `currentSession.agent_id` for
   *  one render between agent switch and session reset; the screen reconciles. */
  currentAgent: Agent | null;
  onTitlePress: () => void;
  onMorePress: () => void;
  onNewPress: () => void;
}

export function ChatHeader({
  currentSession,
  currentAgent,
  onTitlePress,
  onMorePress,
  onNewPress,
}: Props) {
  const agentName = currentAgent?.name ?? "Chat";
  const subtitle = currentSession?.title || "New chat";
  const showMore = !!currentSession;

  return (
    <View className="flex-row items-center px-3 pt-2 pb-2 border-b border-border bg-background">
      <Pressable
        onPress={onTitlePress}
        hitSlop={4}
        className="flex-1 flex-row items-center gap-2 px-1 py-1 rounded-lg active:bg-secondary"
        accessibilityRole="button"
        accessibilityLabel="Sessions and agent picker"
      >
        <ActorAvatar
          type={currentAgent ? "agent" : null}
          id={currentAgent?.id ?? null}
          size={28}
        />
        <View className="flex-1">
          <View className="flex-row items-center gap-1">
            <Text
              className="text-base font-semibold text-foreground"
              numberOfLines={1}
            >
              {agentName}
            </Text>
            <Text className="text-xs text-muted-foreground">▼</Text>
          </View>
          <Text
            className="text-xs text-muted-foreground mt-0.5"
            numberOfLines={1}
          >
            {subtitle}
          </Text>
        </View>
      </Pressable>

      <View className="flex-row items-center">
        {showMore ? (
          <Pressable
            onPress={onMorePress}
            hitSlop={8}
            className="h-9 w-9 items-center justify-center rounded-full active:bg-secondary"
            accessibilityLabel="Session actions"
          >
            <Text className="text-base text-foreground">⋯</Text>
          </Pressable>
        ) : null}
        <Pressable
          onPress={onNewPress}
          hitSlop={8}
          className="h-9 w-9 items-center justify-center rounded-full active:bg-secondary"
          accessibilityLabel="New chat"
        >
          <Text className="text-xl text-foreground leading-none">+</Text>
        </Pressable>
      </View>
    </View>
  );
}
