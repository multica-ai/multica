import type { RuntimeConfigResolutionStatus } from "@multica/core/types";
import type { useT } from "../i18n";

type AgentsT = ReturnType<typeof useT<"agents">>["t"];

const RUNTIME_CONFIG_RESOLUTION_STATUSES = new Set<RuntimeConfigResolutionStatus>([
  "ready",
  "no_user_runtime",
  "runtime_unavailable",
  "runtime_offline",
  "model_unavailable",
]);

interface RuntimeConfigResolutionPayload {
  code?: unknown;
  error?: unknown;
  resolution_status?: unknown;
  resolution_message?: unknown;
}

export function runtimeConfigResolutionMessage(
  payload: RuntimeConfigResolutionPayload | null | undefined,
  t: AgentsT,
): string | null {
  const status = runtimeConfigResolutionStatus(payload);
  if (!status || status === "ready") return null;

  if (typeof payload?.resolution_message === "string" && payload.resolution_message.trim()) {
    return payload.resolution_message;
  }
  if (typeof payload?.error === "string" && payload.error.trim()) {
    return payload.error;
  }
  return runtimeConfigResolutionStatusLabel(status, t);
}

export function runtimeConfigResolutionMessageFromError(
  err: unknown,
  t: AgentsT,
): string | null {
  const payload = runtimeConfigResolutionPayloadFromError(err);
  return payload ? runtimeConfigResolutionMessage(payload, t) : null;
}

/**
 * Machine-readable resolution status carried by a 409 conflict error, for
 * callers that branch on the refusal kind rather than its copy — e.g. the
 * custom-mode save retry that clears the template model.
 */
export function runtimeConfigResolutionStatusFromError(
  err: unknown,
): RuntimeConfigResolutionStatus | null {
  return runtimeConfigResolutionStatus(
    runtimeConfigResolutionPayloadFromError(err),
  );
}

// The one place that knows the shape of a resolution conflict error, so the
// message and status readers can never disagree about what counts as one.
function runtimeConfigResolutionPayloadFromError(
  err: unknown,
): RuntimeConfigResolutionPayload | null {
  if (!err || typeof err !== "object") return null;
  const candidate = err as { body?: unknown; status?: unknown };
  if (candidate.status !== undefined && candidate.status !== 409) return null;
  if (!candidate.body || typeof candidate.body !== "object") return null;
  return candidate.body as RuntimeConfigResolutionPayload;
}

function runtimeConfigResolutionStatus(
  payload: RuntimeConfigResolutionPayload | null | undefined,
): RuntimeConfigResolutionStatus | null {
  const status = payload?.resolution_status ?? payload?.code;
  return isRuntimeConfigResolutionStatus(status) ? status : null;
}

function isRuntimeConfigResolutionStatus(
  status: unknown,
): status is RuntimeConfigResolutionStatus {
  return (
    typeof status === "string" &&
    RUNTIME_CONFIG_RESOLUTION_STATUSES.has(status as RuntimeConfigResolutionStatus)
  );
}

function runtimeConfigResolutionStatusLabel(
  status: RuntimeConfigResolutionStatus,
  t: AgentsT,
): string {
  switch (status) {
    case "no_user_runtime":
      return t(($) => $.runtime_config.resolution.no_user_runtime);
    case "runtime_unavailable":
      return t(($) => $.runtime_config.resolution.runtime_unavailable);
    case "runtime_offline":
      return t(($) => $.runtime_config.resolution.runtime_offline);
    case "model_unavailable":
      return t(($) => $.runtime_config.resolution.model_unavailable);
    case "ready":
      return "";
  }
}
