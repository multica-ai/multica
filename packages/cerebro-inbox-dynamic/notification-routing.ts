import type { InboxItem } from "@multica/core/types";

/** Notifications that are read as messages inside the Dynamic Inbox pane. */
export function opensInDynamicInboxPane(item: Pick<InboxItem, "type">): boolean {
  return (
    item.type === "skill_change_request_created" ||
    item.type === "skill_change_request_reviewed"
  );
}
