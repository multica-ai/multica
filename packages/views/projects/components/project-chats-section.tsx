"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { chatSessionsOptions, projectChatSessions } from "@multica/core/chat/queries";
import { AppLink } from "../../navigation";
import { useLocale, useT } from "../../i18n";
import { formatChatTime, toPreview } from "../../chat/components/chat-session-preview";

export function ProjectChatsSection({ projectId }: { projectId: string }) {
  const { t } = useT("chat");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const [open, setOpen] = useState(true);
  const query = useQuery(chatSessionsOptions(wsId));
  const sessions = projectChatSessions(query.data ?? [], wsId, projectId);

  return (
    <section aria-label={t(($) => $.project_chats.title)}>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70"
      >
        {t(($) => $.project_chats.title)}
        <ChevronRight className={`size-3 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2">
          {query.isLoading ? (
            <p role="status" className="px-2 py-1.5 text-caption text-muted-foreground">{t(($) => $.project_chats.loading)}</p>
          ) : query.isError ? (
            <div role="alert" className="px-2 text-caption text-muted-foreground">
              <p>{t(($) => $.project_chats.error)}</p>
              <Button variant="ghost" size="xs" onClick={() => void query.refetch()}>{t(($) => $.project_chats.retry)}</Button>
            </div>
          ) : sessions.length === 0 ? (
            <p className="px-2 py-1.5 text-caption text-muted-foreground">{t(($) => $.project_chats.empty)}</p>
          ) : (
            <ul className="max-h-72 overflow-y-auto">
              {sessions.map((session) => {
                const title = session.title.trim() || t(($) => $.window.untitled);
                const last = session.last_message;
                const date = last?.created_at ?? session.updated_at;
                const preview = last?.failure_reason
                  ? t(($) => $.list.failed)
                  : last?.message_kind === "no_response"
                    ? t(($) => $.list.no_response_preview)
                    : last ? toPreview(last.content) : t(($) => $.list.no_messages);
                return (
                  <li key={session.id}>
                    <AppLink
                      href={`${wsPaths.chat()}?session=${encodeURIComponent(session.id)}`}
                      newTabTitle={title}
                      className="flex min-w-0 flex-col gap-0.5 rounded-md px-2 py-2 outline-none hover:bg-accent/50 focus-visible:ring-1 focus-visible:ring-ring"
                    >
                      <div className="flex min-w-0 items-baseline gap-2">
                        <span className="min-w-0 flex-1 truncate text-caption font-medium">{title}</span>
                        <time dateTime={date} className="shrink-0 text-micro text-muted-foreground">{formatChatTime(date, locale)}</time>
                      </div>
                      <span className="truncate text-caption text-muted-foreground">{preview}</span>
                    </AppLink>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </section>
  );
}
