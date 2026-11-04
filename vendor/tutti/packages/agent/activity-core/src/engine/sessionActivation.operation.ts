import { selectPendingActivationByRequestId } from "./pendingIntents.selectors.ts";
import type {
  AgentSessionActivationInput,
  AgentSessionEngineStateBase,
  EngineClock,
  EngineIntent
} from "./types.ts";

const SESSION_ACTIVATION_CONFIRMATION_TIMEOUT_MS = 120_000;

interface SessionActivationOperationContext {
  clock: EngineClock;
  dispatch(intent: EngineIntent): void;
  getSnapshot(): AgentSessionEngineStateBase;
  workspaceId: string;
}

export function requestSessionActivation(
  context: SessionActivationOperationContext,
  input: AgentSessionActivationInput
): boolean {
  const agentSessionId = input.agentSessionId.trim();
  const requestId = input.requestId.trim();
  const agentTargetId = input.agentTargetId?.trim() ?? "";
  const clientSubmitId =
    input.mode === "new" ? input.clientSubmitId.trim() : "";
  const initialDisplayPrompt = input.initialDisplayPrompt?.trim() || undefined;
  const title = input.title?.trim() || undefined;
  if (
    !agentSessionId ||
    !requestId ||
    (input.mode === "new" && (!agentTargetId || !clientSubmitId))
  ) {
    return false;
  }
  const requestedAtUnixMs = context.clock.nowUnixMs();
  const sharedIntent = {
    agentSessionId,
    ...(input.capabilityRefs?.length
      ? {
          capabilityRefs: input.capabilityRefs.map((reference) => ({
            ...reference
          }))
        }
      : {}),
    ...(input.initialContent
      ? { content: input.initialContent.map((block) => ({ ...block })) }
      : {}),
    ...(input.cwd !== undefined ? { cwd: input.cwd.trim() } : {}),
    expiresAtUnixMs:
      requestedAtUnixMs + SESSION_ACTIVATION_CONFIRMATION_TIMEOUT_MS,
    ...(initialDisplayPrompt ? { initialDisplayPrompt } : {}),
    ...(input.initialTurnExpected !== undefined
      ? { initialTurnExpected: input.initialTurnExpected }
      : {}),
    ...(input.isolation ? { isolation: input.isolation } : {}),
    ...(input.modelExplicit !== undefined
      ? { modelExplicit: input.modelExplicit }
      : {}),
    ...(input.railPlacement
      ? { railPlacement: { ...input.railPlacement } }
      : {}),
    ...(input.reasoningEffortExplicit !== undefined
      ? { reasoningEffortExplicit: input.reasoningEffortExplicit }
      : {}),
    ...(input.railSectionKey?.trim()
      ? { railSectionKey: input.railSectionKey.trim() }
      : {}),
    requestId,
    requestedAtUnixMs,
    ...(input.runtimeContent
      ? {
          runtimeContent: input.runtimeContent.map((block) => ({ ...block }))
        }
      : {}),
    ...(input.settings ? { settings: { ...input.settings } } : {}),
    ...(input.submitDiagnostics
      ? { submitDiagnostics: { ...input.submitDiagnostics } }
      : {}),
    ...(title ? { title } : {}),
    type: "activation/requested" as const,
    ...(input.visible !== undefined ? { visible: input.visible } : {}),
    workspaceId: context.workspaceId
  };
  const activationBeforeDispatch = selectPendingActivationByRequestId(
    context.getSnapshot(),
    requestId
  );
  context.dispatch(
    input.mode === "new"
      ? {
          ...sharedIntent,
          agentTargetId,
          clientSubmitId,
          mode: "new",
          ...(input.optimisticTitle?.trim()
            ? { optimisticTitle: input.optimisticTitle.trim() }
            : {})
        }
      : {
          ...sharedIntent,
          ...(agentTargetId ? { agentTargetId } : {}),
          mode: "existing"
        }
  );
  const activation = selectPendingActivationByRequestId(
    context.getSnapshot(),
    requestId
  );
  return (
    activation !== activationBeforeDispatch &&
    activation?.agentSessionId === agentSessionId &&
    activation.mode === input.mode
  );
}
