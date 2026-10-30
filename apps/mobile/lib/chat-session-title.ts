export const NEW_CHAT_TITLE = "New chat";

export function chatSessionDisplayTitle(title: string | null | undefined): string {
  return title || NEW_CHAT_TITLE;
}
