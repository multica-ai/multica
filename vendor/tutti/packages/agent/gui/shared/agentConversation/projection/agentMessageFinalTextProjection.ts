import { projectMessageImageCopies } from "@multica/tutti-agent-gui-host/message-image-clipboard";
import type { WorkspaceAgentSessionDetailViewModel } from "../../workspaceAgentSessionDetailViewModel";
import type {
  AgentMessageContentVM,
  AgentMessageRowVM
} from "../contracts/agentMessageRowVM";
import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";

export function projectAgentMessageFinalText(
  rows: readonly AgentTranscriptRowVM[],
  detail: WorkspaceAgentSessionDetailViewModel
): AgentTranscriptRowVM[] {
  const finalTextTargetKeys = findLatestAssistantFinalTextTargetKeys(
    rows,
    buildAssistantFinalTextEligibleTurnIds(detail)
  );
  return rows.map((row) => {
    if (row.kind !== "message") {
      return row;
    }

    let changed = false;
    const messages = row.messages.map((message) => {
      const isTurnFinalText =
        row.speaker === "assistant" &&
        finalTextTargetKeys.has(messagePresentationTargetKey(row, message));
      const copyText =
        row.speaker === "user"
          ? copyTextForUserMessage(message)
          : isTurnFinalText
            ? message.body
            : null;
      if (
        (message.copyText ?? null) === copyText &&
        message.isTurnFinalText === (isTurnFinalText ? true : undefined)
      ) {
        return message;
      }
      changed = true;
      const nextMessage = isTurnFinalText
        ? { ...message, isTurnFinalText: true as const }
        : omitTurnFinalText(message);
      if (copyText) {
        return { ...nextMessage, copyText };
      }
      const { copyText: _copyText, ...withoutCopyText } = nextMessage;
      return withoutCopyText;
    });

    return projectMessageImageCopies(changed ? { ...row, messages } : row);
  });
}

function omitTurnFinalText(
  message: AgentMessageContentVM
): AgentMessageContentVM {
  const { isTurnFinalText: _isTurnFinalText, ...withoutTurnFinalText } =
    message;
  return withoutTurnFinalText;
}

function buildAssistantFinalTextEligibleTurnIds(
  detail: WorkspaceAgentSessionDetailViewModel
): ReadonlySet<string> {
  const ids = new Set<string>();
  detail.turns.forEach((turn, index) => {
    if (
      index < detail.turns.length - 1 ||
      isLatestTranscriptTurnSettled(detail)
    ) {
      ids.add(turn.id);
    }
  });
  return ids;
}

function isLatestTranscriptTurnSettled(
  detail: WorkspaceAgentSessionDetailViewModel
): boolean {
  const latestTranscriptTurnId = detail.turns.at(-1)?.id;
  const canonicalTurn = detail.sessionTurns?.find(
    (turn) => turn.turnId === latestTranscriptTurnId
  );
  const activeTurn = detail.session.activeTurn;
  if (
    activeTurn &&
    activeTurn.turnId === latestTranscriptTurnId &&
    activeTurn.phase !== "settled"
  ) {
    return false;
  }
  if (canonicalTurn) {
    return (
      canonicalTurn.phase === "settled" &&
      detail.showProcessingIndicator !== true
    );
  }
  const activePhase = activeTurn?.phase ?? "";
  return (
    detail.showProcessingIndicator !== true &&
    !["submitted", "running", "waiting", "settling"].includes(activePhase)
  );
}

function findLatestAssistantFinalTextTargetKeys(
  rows: readonly AgentTranscriptRowVM[],
  eligibleTurnIds: ReadonlySet<string>
): ReadonlySet<string> {
  const targetKeys = new Set<string>();
  const coveredTurnIds = new Set<string>();
  for (let rowIndex = rows.length - 1; rowIndex >= 0; rowIndex -= 1) {
    const row = rows[rowIndex];
    if (
      row?.kind !== "message" ||
      row.speaker !== "assistant" ||
      coveredTurnIds.has(row.turnId) ||
      !eligibleTurnIds.has(row.turnId)
    ) {
      continue;
    }
    for (
      let messageIndex = row.messages.length - 1;
      messageIndex >= 0;
      messageIndex -= 1
    ) {
      const message = row.messages[messageIndex];
      if (message && isSettledAssistantFinalTextCandidate(message)) {
        targetKeys.add(messagePresentationTargetKey(row, message));
        coveredTurnIds.add(row.turnId);
        break;
      }
    }
  }
  return targetKeys;
}

function messagePresentationTargetKey(
  row: AgentMessageRowVM,
  message: AgentMessageContentVM
): string {
  return `${row.id}\u0000${message.id}`;
}

function copyTextForUserMessage(message: AgentMessageContentVM): string | null {
  return isVisibleTextMessage(message) ? message.body : null;
}

function isSettledAssistantFinalTextCandidate(
  message: AgentMessageContentVM
): boolean {
  return (
    isVisibleTextMessage(message) &&
    message.statusKind !== "working" &&
    message.statusKind !== "waiting"
  );
}

function isVisibleTextMessage(message: AgentMessageContentVM): boolean {
  return (
    message.body.trim() !== "" &&
    message.contentKind !== "image-grid" &&
    message.contentKind !== "collaboration" &&
    !message.visibleError &&
    !message.systemNotice
  );
}
