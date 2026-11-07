"use client";

import type { ChatSessionList } from "@multica/core/chat/use-session-list";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

export function ChatSessionListStatus({ query }: { query: ChatSessionList }) {
  const { t } = useT("chat");
  if (query.isLoading || query.isFetchingNextPage) {
    return <div role="status" className="shrink-0 p-2 text-caption text-muted-foreground">{t(($) => $.list.loading)}</div>;
  }
  if (query.isError) {
    return (
      <div role="alert" className="shrink-0 p-2 text-caption text-muted-foreground">
        {t(($) => $.list.load_failed)}
        <Button size="xs" variant="ghost" onClick={() => query.isFetchNextPageError ? query.fetchNextPage() : query.refetch()}>{t(($) => $.list.retry)}</Button>
      </div>
    );
  }
  if (!query.hasNextPage) return null;
  return <Button size="xs" variant="ghost" className="shrink-0" disabled={query.isFetching} onClick={() => query.fetchNextPage()}>{t(($) => $.list.load_more)}</Button>;
}
