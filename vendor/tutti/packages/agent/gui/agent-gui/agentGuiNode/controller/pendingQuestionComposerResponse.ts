import type { AgentActivityInteraction } from "@tutti-os/agent-activity-core";
import type { AgentInteractionResponseInput } from "../../../shared/agentConversation/contracts/agentConversationVM";
import { normalizeAskUserQuestions } from "../../../shared/agentConversation/askUserQuestions";
import {
  buildAskUserAnswerPayload,
  writeOwnAnswer,
  type InteractiveAnswerPayload
} from "../../../shared/agentConversation/interactiveAnswerPayload";
import type { AgentPromptContentBlock } from "../../../shared/contracts/dto";
import type { AgentComposerSubmitOptions } from "../composer/AgentComposer.types";
import { agentPromptContentDisplayText } from "../model/agentComposerDraft";

export interface PendingQuestionComposerTarget {
  agentSessionId: string;
  questionIds: readonly string[];
  requestId: string;
  turnId: string;
}

export function resolvePendingQuestionComposerTarget(input: {
  activeTurnId: string;
  agentSessionId: string;
  pendingInteractions: readonly AgentActivityInteraction[];
}): PendingQuestionComposerTarget | null {
  const activeTurnId = input.activeTurnId.trim();
  const agentSessionId = input.agentSessionId.trim();
  if (!activeTurnId || !agentSessionId) return null;

  const matching = input.pendingInteractions.filter(
    (interaction) =>
      interaction.status === "pending" &&
      interaction.kind === "question" &&
      interaction.agentSessionId.trim() === agentSessionId &&
      interaction.turnId.trim() === activeTurnId
  );
  if (matching.length !== 1) return null;

  const interaction = matching[0]!;
  const questions = normalizeAskUserQuestions(interaction.input?.questions);
  if (
    questions.length === 0 ||
    questions.some((question) => question.allowFreeText === false)
  ) {
    return null;
  }
  return {
    agentSessionId,
    questionIds: questions.map((question) => question.id),
    requestId: interaction.requestId,
    turnId: activeTurnId
  };
}

export function resolvePendingQuestionComposerResponse(input: {
  activeTurnId: string;
  agentSessionId: string;
  content: readonly AgentPromptContentBlock[];
  pendingInteractions: readonly AgentActivityInteraction[];
  submitOptions?: Pick<
    AgentComposerSubmitOptions,
    "capabilityRefs" | "requiredSettingsPatch"
  >;
}): AgentInteractionResponseInput | null {
  if (
    input.submitOptions?.requiredSettingsPatch !== undefined ||
    Boolean(input.submitOptions?.capabilityRefs?.length) ||
    input.content.length === 0 ||
    input.content.some((block) => block.type !== "text")
  ) {
    return null;
  }
  const target = resolvePendingQuestionComposerTarget(input);
  if (!target) return null;
  const payload = buildAskUserComposerAnswerPayload(
    target.questionIds,
    agentPromptContentDisplayText(input.content)
  );
  if (!payload) return null;
  return {
    action: "submit",
    agentSessionId: target.agentSessionId,
    payload: { ...payload },
    requestId: target.requestId,
    turnId: target.turnId
  };
}

/**
 * The ordinary Composer is a task-level escape hatch, not a second per-question
 * form. Attach its one instruction to each normalized question while keeping
 * this AgentGUI-only policy out of the public interactive-answer wire module.
 */
function buildAskUserComposerAnswerPayload(
  rawQuestionIds: readonly string[],
  rawAnswer: string
): InteractiveAnswerPayload | null {
  const answer = rawAnswer.trim();
  if (!answer) return null;

  const answersByQuestionId: Record<string, string> = {};
  for (const rawQuestionId of rawQuestionIds) {
    const questionId = rawQuestionId.trim();
    if (!questionId || Object.hasOwn(answersByQuestionId, questionId)) {
      continue;
    }
    writeOwnAnswer(answersByQuestionId, questionId, answer);
  }
  return Object.keys(answersByQuestionId).length > 0
    ? buildAskUserAnswerPayload(answersByQuestionId)
    : null;
}
