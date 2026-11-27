/**
 * Display title for a chat session. Channel-created sessions (`/new`,
 * `/clear`) store an explicitly empty title, so the fallback is the
 * `chat` namespace's `sessions.new_chat`.
 */
export function chatSessionDisplayTitle(
  title: string | null | undefined,
  t: (key: string) => string,
): string {
  return title || t("sessions.new_chat");
}
