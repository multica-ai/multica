export const AUTH_SESSION_STATE_CHANNEL = "auth:session-state";

export type AuthSessionUserId = string | null;

export function parseAuthSessionUserId(
  value: unknown,
): AuthSessionUserId | undefined {
  if (value === null) return null;
  if (typeof value !== "string") return undefined;
  const userId = value.trim();
  if (!userId || userId.length > 256) return undefined;
  return userId;
}
