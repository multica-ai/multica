"use client";

import { Inbox, Search, Filter, MailOpen } from "lucide-react";
import { useT } from "../../i18n";

export type EmptyStateType = "empty" | "no_unread" | "no_search_results" | "no_filter_results";

export function InboxEmptyState({ type }: { type: EmptyStateType }) {
  const { t } = useT("inbox");

  const config: Record<EmptyStateType, { icon: typeof Inbox; message: string; submessage?: string }> = {
    empty: {
      icon: Inbox,
      message: t(($) => $.empty.inbox_empty),
      submessage: t(($) => $.empty.inbox_empty_sub),
    },
    no_unread: {
      icon: MailOpen,
      message: t(($) => $.empty.all_caught_up),
      submessage: t(($) => $.empty.all_caught_up_sub),
    },
    no_search_results: {
      icon: Search,
      message: t(($) => $.empty.no_search_results),
    },
    no_filter_results: {
      icon: Filter,
      message: t(($) => $.empty.no_filter_results),
    },
  };

  const { icon: Icon, message, submessage } = config[type];

  return (
    <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
      <Icon className="mb-3 h-8 w-8 text-muted-foreground/50" />
      <p className="text-sm font-medium">{message}</p>
      {submessage && (
        <p className="mt-1 text-xs text-muted-foreground/60">{submessage}</p>
      )}
    </div>
  );
}
