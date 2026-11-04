import type { PendingSubmitIntentRecord } from "@tutti-os/agent-activity-core";
import { translate } from "../../../i18n/index";
import type { AppErrorCode } from "../../../shared/contracts/dto";
import { getAppErrorCode } from "../../../shared/errors/appError";
import { AGENT_PROVIDER_LABEL } from "../../../contexts/settings/domain/agentSettings";

export const AGENT_PROVIDER_SESSION_NOT_FOUND_ERROR =
  "agent.provider_session_not_found";
export const AGENT_RESUME_SESSION_NOT_LOCAL_ERROR =
  "agent.resume_session_not_local";
export const AGENT_SETTINGS_REQUIRE_NEW_SESSION_ERROR =
  "agent.settings_require_new_session";
export const AGENT_CONFIG_DEPENDENCY_UNAVAILABLE_REASON =
  "agent.config_dependency_unavailable";
export const AGENT_PROCESS_CLEANUP_PENDING_REASON =
  "agent.process_cleanup_pending";
export const AGENT_SESSION_TITLE_TOO_LONG_REASON =
  "workspace_agent_session_title_too_long";
export const AGENT_SESSION_NOT_FOUND_ERROR = "session.not_found";
export const AGENT_PROVIDER_SESSION_NOT_FOUND_FALLBACK_MESSAGE =
  "The previous agent session can no longer be restored.";
export const AGENT_RESUME_SESSION_NOT_LOCAL_FALLBACK_MESSAGE =
  "The previous agent session is not available on this machine.";
export const AGENT_GUI_CAUGHT_ERROR_STACK_LIMIT = 4000;

export function agentGUISubmitSettlementError(
  submit: Pick<
    PendingSubmitIntentRecord,
    "errorCode" | "errorMessage" | "errorReason"
  >
): Error {
  return Object.assign(
    new Error(
      submit.errorMessage?.trim() || translate("agentHost.agentGui.sendFailed")
    ),
    {
      ...(submit.errorCode ? { code: submit.errorCode } : {}),
      ...(submit.errorReason ? { reason: submit.errorReason } : {})
    }
  );
}

export function normalizeAgentGUIDiagnosticError(
  error: unknown
): Record<string, unknown> {
  const record =
    error && typeof error === "object"
      ? (error as Record<string, unknown>)
      : null;
  const appErrorCode = getAgentGUIErrorCode(error);
  const configDependency = getAgentGUIConfigDependencyErrorDetails(error);
  const explicitCode = typeof record?.code === "string" ? record.code : null;
  const hasStructuredCode = appErrorCode !== null || explicitCode !== null;
  const nativeRuntimeError =
    error instanceof Error && isNativeRuntimeError(error);
  const base: Record<string, unknown> = {
    ...(error instanceof Error ? { name: error.name } : {}),
    ...(explicitCode ? { code: explicitCode } : {}),
    ...(typeof record?.statusCode === "number"
      ? { statusCode: record.statusCode }
      : {}),
    ...(typeof record?.correlationId === "string"
      ? { correlationId: record.correlationId }
      : {}),
    ...(typeof record?.reason === "string" ? { reason: record.reason } : {}),
    ...(typeof record?.retryable === "boolean"
      ? { retryable: record.retryable }
      : {}),
    ...(configDependency
      ? {
          configKey: configDependency.configKey,
          dependencyPath: configDependency.dependencyPath,
          dependencyProvider: configDependency.provider,
          failureKind: configDependency.failureKind
        }
      : {})
  };
  if (nativeRuntimeError) {
    return {
      ...base,
      message: error.message,
      ...(error.stack ? { stack: limitDiagnosticText(error.stack) } : {})
    };
  }
  if (record) {
    return {
      ...base,
      ...(typeof record.name === "string" && !("name" in base)
        ? { name: record.name }
        : {}),
      ...(typeof record.message === "string"
        ? { messageLength: record.message.length }
        : {}),
      ...(typeof record.debugMessage === "string"
        ? { debugMessageLength: record.debugMessage.length }
        : {})
    };
  }
  const rawMessage = getAgentGUIRawErrorMessage(error);
  return {
    ...(hasStructuredCode ? {} : { messageLength: rawMessage?.length ?? 0 }),
    type: typeof error
  };
}

export function isNativeRuntimeError(error: Error): boolean {
  return (
    error instanceof RangeError ||
    error instanceof ReferenceError ||
    error instanceof SyntaxError ||
    error instanceof TypeError ||
    error instanceof URIError
  );
}

export function limitDiagnosticText(value: string): string {
  return value.length <= AGENT_GUI_CAUGHT_ERROR_STACK_LIMIT
    ? value
    : `${value.slice(0, AGENT_GUI_CAUGHT_ERROR_STACK_LIMIT)}...`;
}

export function getAgentGUIErrorCode(error: unknown): AppErrorCode | null {
  return (
    getAppErrorCode(error) ??
    inferAgentGUIErrorCodeFromReason(getAgentGUIErrorReason(error)) ??
    inferAgentGUIErrorCodeFromMessage(getAgentGUIRawErrorMessage(error))
  );
}

export function getAgentGUIErrorReason(error: unknown): string | null {
  if (!error || typeof error !== "object") return null;
  const reason = (error as { reason?: unknown }).reason;
  return typeof reason === "string" && reason.trim() ? reason.trim() : null;
}

export function inferAgentGUIErrorCodeFromReason(
  reason: string | null
): AppErrorCode | null {
  return reason === AGENT_SETTINGS_REQUIRE_NEW_SESSION_ERROR
    ? (AGENT_SETTINGS_REQUIRE_NEW_SESSION_ERROR as AppErrorCode)
    : null;
}

export function inferAgentGUIErrorCodeFromMessage(
  message: string | null
): AppErrorCode | null {
  switch (message?.trim()) {
    case AGENT_PROVIDER_SESSION_NOT_FOUND_FALLBACK_MESSAGE:
      return AGENT_PROVIDER_SESSION_NOT_FOUND_ERROR as AppErrorCode;
    case AGENT_RESUME_SESSION_NOT_LOCAL_FALLBACK_MESSAGE:
      return AGENT_RESUME_SESSION_NOT_LOCAL_ERROR as AppErrorCode;
    default:
      return null;
  }
}

export function isProviderSessionNotFoundErrorCode(
  code: AppErrorCode | null | undefined
): boolean {
  return code === AGENT_PROVIDER_SESSION_NOT_FOUND_ERROR;
}

export function isResumeSessionNotLocalErrorCode(
  code: AppErrorCode | null | undefined
): boolean {
  return code === AGENT_RESUME_SESSION_NOT_LOCAL_ERROR;
}

export function isNonRetryableResumeErrorCode(
  code: AppErrorCode | null | undefined
): boolean {
  return (
    isProviderSessionNotFoundErrorCode(code) ||
    isResumeSessionNotLocalErrorCode(code)
  );
}

export function isSessionNotFoundErrorCode(
  code: AppErrorCode | null | undefined
): boolean {
  return code === AGENT_SESSION_NOT_FOUND_ERROR;
}

export function isSettingsRequireNewSessionErrorCode(
  code: AppErrorCode | null | undefined
): boolean {
  return code === AGENT_SETTINGS_REQUIRE_NEW_SESSION_ERROR;
}

export function buildProviderSessionNotFoundActivationError(
  message?: string | null
): {
  code: AppErrorCode;
  message: string;
  debugMessage?: string;
} {
  const normalizedMessage = message?.trim() || null;
  return {
    code: AGENT_PROVIDER_SESSION_NOT_FOUND_ERROR,
    message: translate("messages.agentProviderSessionNotFound"),
    ...(normalizedMessage ? { debugMessage: normalizedMessage } : {})
  };
}

export function buildResumeSessionNotLocalActivationError(
  message?: string | null
): {
  code: AppErrorCode;
  message: string;
  debugMessage?: string;
} {
  const normalizedMessage = message?.trim() || null;
  return {
    code: AGENT_RESUME_SESSION_NOT_LOCAL_ERROR,
    message: translate("messages.agentResumeSessionNotLocal"),
    ...(normalizedMessage ? { debugMessage: normalizedMessage } : {})
  };
}

export function getAgentGUIErrorMessage(error: unknown): string {
  const configDependency = getAgentGUIConfigDependencyErrorDetails(error);
  if (configDependency) {
    const provider =
      AGENT_PROVIDER_LABEL[
        configDependency.provider as keyof typeof AGENT_PROVIDER_LABEL
      ] ||
      configDependency.provider ||
      translate("sidebar.fallbackAgentLabel");
    return translate("messages.agentConfigDependencyUnavailable", {
      provider
    });
  }
  if (getAgentGUIErrorReason(error) === AGENT_PROCESS_CLEANUP_PENDING_REASON)
    return translate("messages.agentProcessCleanupPending");
  const code = getAgentGUIErrorCode(error);
  if (isProviderSessionNotFoundErrorCode(code))
    return translate("messages.agentProviderSessionNotFound");
  if (isResumeSessionNotLocalErrorCode(code))
    return translate("messages.agentResumeSessionNotLocal");
  if (isSettingsRequireNewSessionErrorCode(code))
    return translate("messages.agentSettingsRequireNewSession");
  if (getAgentGUIErrorReason(error) === AGENT_SESSION_TITLE_TOO_LONG_REASON) {
    const maxCharacters = getAgentGUIErrorNumberParam(error, "maxCharacters");
    return maxCharacters === null
      ? translate("messages.agentSessionTitleTooLongWithoutLimit")
      : translate("messages.agentSessionTitleTooLong", { maxCharacters });
  }
  if (error && typeof error === "object") {
    const debugMessage = (error as { debugMessage?: unknown }).debugMessage;
    if (typeof debugMessage === "string" && debugMessage.trim())
      return debugMessage.trim();
  }
  return error instanceof Error ? error.message : String(error);
}

function getAgentGUIConfigDependencyErrorDetails(error: unknown): {
  provider: string;
  configKey: string;
  dependencyPath: string;
  failureKind: string;
} | null {
  if (!error || typeof error !== "object") return null;
  const record = error as Record<string, unknown>;
  if (record.reason !== AGENT_CONFIG_DEPENDENCY_UNAVAILABLE_REASON) return null;
  const params =
    record.params && typeof record.params === "object"
      ? (record.params as Record<string, unknown>)
      : {};
  return {
    provider: normalizedString(params.provider),
    configKey: normalizedString(params.configKey),
    dependencyPath: normalizedString(params.dependencyPath),
    failureKind: normalizedString(params.failureKind)
  };
}

function normalizedString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function getAgentGUIErrorNumberParam(
  error: unknown,
  key: string
): number | null {
  if (!error || typeof error !== "object") return null;
  const params = (error as { params?: unknown }).params;
  if (!params || typeof params !== "object") return null;
  const value = (params as Record<string, unknown>)[key];
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

export function getAgentGUIRawErrorMessage(error: unknown): string | null {
  if (error && typeof error === "object") {
    const debugMessage = (error as { debugMessage?: unknown }).debugMessage;
    if (typeof debugMessage === "string" && debugMessage.trim())
      return debugMessage.trim();
  }
  if (error instanceof Error && error.message.trim())
    return error.message.trim();
  if (typeof error === "string" && error.trim()) return error.trim();
  return null;
}
