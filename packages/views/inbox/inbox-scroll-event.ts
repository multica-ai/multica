export const INBOX_SCROLL_TO_LATEST_UNREAD_EVENT =
  "multica:inbox-scroll-to-latest-unread";

export function requestLatestUnreadInboxScroll(): void {
  window.dispatchEvent(new Event(INBOX_SCROLL_TO_LATEST_UNREAD_EVENT));
}
