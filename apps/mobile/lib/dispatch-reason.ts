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
 * User-facing sentence for a refused send. `invocation_not_allowed` is the
 * revoked-permission case (MUL-4525): the session was created while the user
 * could run the agent and the server now refuses, so it must not read as a
 * transient failure the user should retry.
 */
export function sendFailureMessage(err: unknown): string {
  switch (dispatchReasonCode(err)) {
    case "invocation_not_allowed":
      return "You no longer have permission to run this agent, so the message was not sent.";
    case "agent_runtime_required":
      return "Bind a runtime to this agent before sending a message.";
    default:
      return "Your message could not be sent. Please try again.";
  }
}
