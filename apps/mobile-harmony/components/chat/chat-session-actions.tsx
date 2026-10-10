/**
 * Right-side actions for the Chat tab header. Two buttons:
 *   - ⋯ (session menu): only when an active session exists.
 *   - + (new chat): always shown.
 *
 * Both are the ported ghost `<IconButton>`, so touch feedback / sizing /
 * dark-mode tinting stay consistent with the rest of the header toolbar.
 * HarmonyOS port of apps/mobile/components/chat/chat-session-actions.tsx.
 */
import { IconButton } from "@/components/ui/icon-button";

interface Props {
  showMore: boolean;
  onMorePress: () => void;
  onNewPress: () => void;
}

export function ChatSessionActions({
  showMore,
  onMorePress,
  onNewPress,
}: Props) {
  return (
    <>
      {showMore ? (
        <IconButton
          name="ellipsis-horizontal"
          onPress={onMorePress}
          accessibilityLabel="Session actions"
        />
      ) : null}
      <IconButton
        name="add"
        iconSize={24}
        onPress={onNewPress}
        accessibilityLabel="New chat"
      />
    </>
  );
}
