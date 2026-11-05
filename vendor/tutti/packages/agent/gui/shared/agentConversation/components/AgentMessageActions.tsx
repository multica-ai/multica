import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  useCallback,
  useEffect,
  useState,
  type JSX,
  type ReactNode
} from "react";
import { CheckIcon, CopyIcon } from "@tutti-os/ui-system/icons";
import { Pencil } from "lucide-react";
import { formatAgentMessageTimestamp } from "../../../app/renderer/shell/utils/format";
import { translate } from "../../../i18n";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import type { AgentMessageRowVM } from "../contracts/agentMessageRowVM";
import { CanvasNodeGhostIconButton } from "../../../contexts/workspace/presentation/renderer/components/shared/CanvasNodeGhostIconButton";
import { observer } from "mobx-react-lite";
export const AgentCopyableMessageGroup = observer(
  AgentCopyableMessageGroupRender
);
const AgentMessageEditButton = observer(AgentMessageEditButtonRender);
const AgentMessageCopyButton = observer(AgentMessageCopyButtonRender);

const MESSAGE_COPY_FEEDBACK_MS = 1400;

function AgentCopyableMessageGroupRender({
  children,
  copyText,
  editAction,
  footerAction,
  occurredAtUnixMs,
  onCopyMessageText,
  speaker
}: AgentCopyableMessageGroupProps): JSX.Element {
  "use memo";
  const timestamp = formatAgentMessageTimestamp(occurredAtUnixMs);
  const hasFooter = Boolean(
    timestamp || copyText || editAction || footerAction
  );

  return (
    <div
      className={styles.messageGroup}
      data-agent-message-footer={hasFooter ? "true" : undefined}
      data-agent-message-speaker={speaker}
    >
      {children}
      {hasFooter ? (
        <div className={styles.messageFooter}>
          {timestamp ? (
            <span className={styles.messageTimestamp}>{timestamp}</span>
          ) : null}
          {copyText ? (
            <AgentMessageCopyButton
              copyText={copyText}
              onCopyMessageText={onCopyMessageText}
            />
          ) : null}
          {editAction ? (
            <AgentMessageEditButton
              disabled={editAction.disabled}
              label={editAction.label}
              onClick={editAction.onClick}
            />
          ) : null}
          {footerAction}
        </div>
      ) : null}
    </div>
  );
}

function AgentMessageEditButtonRender({
  disabled,
  label,
  onClick
}: {
  disabled: boolean;
  label: string;
  onClick: () => void;
}): JSX.Element {
  "use memo";
  return (
    <CanvasNodeGhostIconButton
      className={styles.messageCopyButton}
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
    >
      <Pencil
        size={14}
        aria-hidden="true"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2}
      />
    </CanvasNodeGhostIconButton>
  );
}

function AgentMessageCopyButtonRender({
  copyText,
  onCopyMessageText
}: {
  copyText: string;
  onCopyMessageText: (text: string) => Promise<boolean>;
}): JSX.Element {
  "use memo";
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) {
      return;
    }
    // timing: clear transient copy confirmation after the visible feedback window
    const reset = window.setTimeout(() => {
      setCopied(false);
    }, MESSAGE_COPY_FEEDBACK_MS);
    return () => window.clearTimeout(reset);
  }, [copied]);
  const handleClick = useCallback(async () => {
    if (await onCopyMessageText(copyText)) {
      setCopied(true);
    }
  }, [copyText, onCopyMessageText]);
  const handleClickCallback = usePersistCallback(handleClick);
  const label = copied
    ? translate("agentHost.agentGui.messageCopied")
    : translate("agentHost.agentGui.copyMessage");

  return (
    <CanvasNodeGhostIconButton
      className={styles.messageCopyButton}
      aria-label={label}
      data-copied={copied ? "true" : "false"}
      onClick={handleClickCallback}
    >
      {copied ? (
        <CheckIcon width={14} height={14} aria-hidden="true" />
      ) : (
        <CopyIcon width={14} height={14} aria-hidden="true" />
      )}
    </CanvasNodeGhostIconButton>
  );
}

type AgentCopyableMessageGroupProps = {
  children: ReactNode;
  copyText: string | null;
  editAction: {
    disabled: boolean;
    label: string;
    onClick: () => void;
  } | null;
  footerAction?: ReactNode;
  occurredAtUnixMs: number | null;
  onCopyMessageText: (text: string) => Promise<boolean>;
  speaker: AgentMessageRowVM["speaker"];
};
