/**
 * Right-side actions for the Chat tab header. Two buttons:
 *   - ⋯ (session menu): only when an active session exists.
 *   - + (new chat): always shown.
 *
 * Both are RNR `<Button variant="ghost" size="icon">` via IconButton, so
 * touch feedback / sizing / dark-mode tinting are all consistent with the
 * rest of the header toolbar.
 */
import { useTranslation } from "react-i18next";
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
  const { t } = useTranslation("chat");
  return (
    <>
      {showMore ? (
        <IconButton
          name="ellipsis-horizontal"
          onPress={onMorePress}
          accessibilityLabel={t("session_actions.session_actions_label")}
        />
      ) : null}
      <IconButton
        name="add"
        iconSize={24}
        onPress={onNewPress}
        accessibilityLabel={t("session_actions.new_chat_label")}
      />
    </>
  );
}
