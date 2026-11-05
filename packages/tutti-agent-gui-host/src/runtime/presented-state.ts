import type { ActivationModel } from "@multica/conversation-client/models";
import type {
AgentSessionActivationInput,
PendingActivationIntentRecord
} from "@tutti-os/agent-activity-core";
type ActivationSettings = NonNullable<AgentSessionActivationInput["settings"]>;
export function normalizeActivation(
  input: AgentSessionActivationInput
): AgentSessionActivationInput | null {
  const agentSessionId = input.agentSessionId.trim();
  const requestId = input.requestId.trim();
  const agentTargetId = input.agentTargetId?.trim() ?? "";
  const shared = {
    agentSessionId,
    requestId,
    title: input.title?.trim() || undefined,
    initialDisplayPrompt: input.initialDisplayPrompt?.trim() || undefined
  };
  if (input.mode === "existing")
    return { ...input, ...shared, agentTargetId: agentTargetId || undefined };
  const clientSubmitId = input.clientSubmitId.trim();
  if (!agentTargetId || !clientSubmitId) return null;
  return {
    ...input,
    ...shared,
    agentTargetId,
    clientSubmitId,
    optimisticTitle: input.optimisticTitle?.trim() || undefined
  };
}

export function presentActivation(
  record: ActivationModel<AgentSessionActivationInput, ActivationSettings>,
  workspaceId: string
): PendingActivationIntentRecord {
  const { request } = record;
  const content = request.initialContent ?? [];
  const shared = {
    agentSessionId: request.agentSessionId,
    requestId: request.requestId,
    workspaceId,
    content,
    cwd: request.cwd?.trim() ?? "",
    title: request.title ?? null,
    ...(request.initialDisplayPrompt
      ? { displayPrompt: request.initialDisplayPrompt }
      : {}),
    ...(request.railSectionKey?.trim()
      ? { railSectionKey: request.railSectionKey.trim() }
      : {}),
    ...(request.railPlacement ? { railPlacement: request.railPlacement } : {}),
    ...(record.settings ? { settings: record.settings } : {}),
    ...(record.settingsStatus
      ? { settingsUpdateStatus: record.settingsStatus }
      : {}),
    status: record.status,
    errorCode: record.errorCode,
    errorMessage: record.errorMessage,
    requestedAtUnixMs: record.requestedAtUnixMs,
    expiresAtUnixMs: record.expiresAtUnixMs,
    initialTurnExpected:
      request.initialTurnExpected ??
      (request.runtimeContent ?? content).length > 0,
    initialPromptRetracted: false
  };
  return request.mode === "new"
    ? {
        ...shared,
        mode: "new",
        agentTargetId: request.agentTargetId,
        clientSubmitId: request.clientSubmitId,
        ...(request.optimisticTitle
          ? { optimisticTitle: request.optimisticTitle }
          : {})
      }
    : {
        ...shared,
        mode: "existing",
        agentTargetId: request.agentTargetId ?? null,
        clientSubmitId: null
      };
}
