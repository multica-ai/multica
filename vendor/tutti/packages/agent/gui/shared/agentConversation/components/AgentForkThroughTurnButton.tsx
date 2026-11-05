import { GitFork, LoaderCircle } from "lucide-react";
import { observer } from "mobx-react-lite";
import type { JSX } from "react";
import styles from "../../../agent-gui/agentGuiNode/AgentGUIConversation.styles";
import { CanvasNodeGhostIconButton } from "../../../contexts/workspace/presentation/renderer/components/shared/CanvasNodeGhostIconButton";
import { useOptionalConversationServices } from "../../../conversationServices";
import { translate } from "../../../i18n/index";

export const AgentForkThroughTurnButton = observer(
  function AgentForkThroughTurnButton({
    agentSessionId,
    turnId,
    revealOnMessageHover = true,
    onFork
  }: {
    agentSessionId: string;
    turnId: string;
    revealOnMessageHover?: boolean;
    onFork: () => void;
  }): JSX.Element {
    const services = useOptionalConversationServices();
    const pending =
      services?.mutations.pendingForkTurnIds(agentSessionId).includes(turnId) ??
      false;
    const label = translate(
      pending
        ? "agentHost.agentGui.forkThroughTurnPending"
        : "agentHost.agentGui.forkThroughTurn"
    );
    return (
      <CanvasNodeGhostIconButton
        className={
          revealOnMessageHover
            ? styles.messageCopyButton
            : "static h-[22px] min-h-[22px] w-[22px] min-w-[22px] rounded-[5px]"
        }
        aria-busy={pending || undefined}
        aria-label={label}
        aria-live="polite"
        data-pending={pending || undefined}
        disabled={pending}
        onClick={onFork}
      >
        {pending ? (
          <LoaderCircle
            width={14}
            height={14}
            className="animate-spin motion-reduce:animate-none"
            aria-hidden="true"
          />
        ) : (
          <GitFork width={14} height={14} aria-hidden="true" />
        )}
      </CanvasNodeGhostIconButton>
    );
  }
);

export function AgentForkThroughTurnFooter({
  agentSessionId,
  turnId,
  onFork
}: {
  agentSessionId: string;
  turnId: string;
  onFork: () => void;
}): JSX.Element {
  return (
    <div
      className="mt-1 flex min-h-[26px] items-center"
      data-agent-turn-footer="true"
    >
      <AgentForkThroughTurnButton
        agentSessionId={agentSessionId}
        turnId={turnId}
        revealOnMessageHover={false}
        onFork={onFork}
      />
    </div>
  );
}
