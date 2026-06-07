/**
 * Mutates request headers for a single outgoing browser request:
 * - Strips Origin from WebSocket upgrade requests.
 * - Injects `Authorization: Bearer <token>` for API requests when a token is
 *   set and the request doesn't already carry an Authorization header.
 */
export function applyAuthRequestHeaders(
  headers: Record<string, string>,
  url: string,
  authToken: string | null,
  apiBaseUrl: string | null,
): Record<string, string> {
  const result = { ...headers };

  if (url.startsWith("wss://") || url.startsWith("ws://")) {
    delete result["Origin"];
  }

  if (
    authToken &&
    apiBaseUrl &&
    url.startsWith(apiBaseUrl) &&
    !result["Authorization"]
  ) {
    result["Authorization"] = `Bearer ${authToken}`;
  }

  return result;
}
