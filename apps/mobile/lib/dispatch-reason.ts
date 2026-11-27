/**
 * Mobile-owned mirror of `packages/core/api/client.ts:dispatchReasonCode`.
 *
 * Why mirror instead of import: the core version narrows on core's own
 * `ApiError` class, and mobile throws `apps/mobile/data/api.ts:ApiError` — a
 * different constructor, so `instanceof` never matches across the two. The
 * extraction itself is identical: read the stable `reason_code` the admission
 * gate puts on a structured rejection body.
 */
export function dispatchReasonCode(err: unknown): string | undefined {
  const body = (err as { body?: unknown } | null)?.body;
  if (body && typeof body === "object") {
    const code = (body as { reason_code?: unknown }).reason_code;
    if (typeof code === "string" && code.length > 0) return code;
  }
  return undefined;
}

/**
 * User-facing sentence for a refused send, from the `chat` namespace's
 * `failure.*` keys. `invocation_not_allowed` is the revoked-permission case
 * (MUL-4525): the session was created while the user could run the agent and
 * the server now refuses, so it must not read as a transient failure the user
 * should retry.
 */
export function sendFailureMessage(
  err: unknown,
  t: (key: string, options?: Record<string, unknown>) => string,
): string {
  switch (dispatchReasonCode(err)) {
    case "invocation_not_allowed":
      return t("failure.invocation_not_allowed");
    case "agent_runtime_required":
      return t("failure.agent_runtime_required");
    case "runtime_access_denied":
      // The agent's owner cannot execute it on the selected private runtime.
      // Retrying never fixes this — the fix is making that runtime public or
      // rebinding/copying the agent to a runtime its owner can use.
      return t("failure.runtime_access_denied", {
        detail: t("failure.runtime_access_recovery"),
      });
    default:
      return t("failure.default");
  }
}
