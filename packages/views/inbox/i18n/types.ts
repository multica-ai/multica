import type { InboxItemType } from "@multica/core/types";

export type InboxDict = {
  page: {
    title: string;
    backToInbox: string;
    emptyListTitle: string;
    emptyDetailWithItems: string;
    emptyDetailNoItems: string;
  };
  actions: {
    markAllRead: string;
    archiveAll: string;
    archiveAllRead: string;
    archiveCompleted: string;
    archive: string;
  };
  errors: {
    markRead: string;
    archive: string;
    markAllRead: string;
    archiveAll: string;
    archiveRead: string;
    archiveCompleted: string;
  };
  timeAgo: {
    justNow: string;
    minutes: (n: number) => string;
    hours: (n: number) => string;
    days: (n: number) => string;
  };
  types: Record<InboxItemType, string>;
  detail: {
    setStatusTo: string;
    setPriorityTo: string;
    assignedTo: (name: string) => string;
    removedAssignee: string;
    setDueDateTo: (date: string) => string;
    removedDueDate: string;
    reactedToComment: (emoji: string) => string;
  };
};
