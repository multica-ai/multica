"use client";

import { useQuery } from "@tanstack/react-query";
import { X, MessageSquare, Bot, ExternalLink } from "lucide-react";
import { useNavigation } from "@multica/views/navigation";
import { useDashboardStore } from "../../core/store";
import { actorMessagesOptions } from "../../core/queries";
import type { ActorMessage } from "../../core/api";

function timeAgo(iso: string): string {
  try {
    const diff = (Date.now() - new Date(iso).getTime()) / 1000;
    if (diff < 60) return "just now";
    if (diff < 3600) return `${Math.floor(diff / 60)} min ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)} h ago`;
    return new Date(iso).toLocaleDateString(undefined, { day: "numeric", month: "short" });
  } catch {
    return "";
  }
}

export function ActorMessagePanel({ wsId, workspaceSlug }: { wsId: string; workspaceSlug: string }) {
  const range = useDashboardStore((s) => s.range);
  const actorId = useDashboardStore((s) => s.messagePanelActorId);
  const actorName = useDashboardStore((s) => s.messagePanelActorName);
  const close = useDashboardStore((s) => s.closeMessagePanel);

  const { data, isLoading } = useQuery(actorMessagesOptions(wsId, actorId, range));

  if (!actorId) return null;

  const messages = data?.messages ?? [];

  return (
    <div className="fixed inset-y-0 right-0 z-40 flex w-full max-w-sm flex-col border-l bg-background shadow-xl">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <MessageSquare className="size-4 text-muted-foreground" />
          <div>
            <p className="text-sm font-semibold">{actorName ?? "Messages"}</p>
            <p className="text-xs text-muted-foreground">
              {isLoading ? "Loading…" : `${messages.length} messages in period`}
            </p>
          </div>
        </div>
        <button
          type="button"
          onClick={close}
          className="rounded-md p-1 text-muted-foreground hover:text-foreground"
        >
          <X className="size-4" />
        </button>
      </div>

      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-3">
        {isLoading ? (
          <div className="space-y-3">
            {[0, 1, 2, 3].map((i) => (
              <div key={i} className="space-y-1">
                <div className="h-3 w-24 animate-pulse rounded bg-muted" />
                <div className="h-10 animate-pulse rounded bg-muted" />
              </div>
            ))}
          </div>
        ) : messages.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No messages in the selected period.
          </p>
        ) : (
          <div className="space-y-2">
            {messages.map((msg) => (
              <MessageRow key={msg.id} message={msg} workspaceSlug={workspaceSlug} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function MessageRow({ message, workspaceSlug }: { message: ActorMessage; workspaceSlug: string }) {
  const { push } = useNavigation();

  return (
    <div className="rounded-lg border bg-card p-3">
      <div className="mb-1.5 flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Bot className="size-3" />
          <span className="font-medium text-foreground">{message.agent_name}</span>
          {message.issue_title && (
            <>
              <span>·</span>
              <span className="truncate max-w-[120px]">{message.issue_title}</span>
            </>
          )}
        </div>
        <div className="flex items-center gap-1.5">
          <span className="shrink-0 text-[11px] text-muted-foreground">
            {timeAgo(message.created_at)}
          </span>
          <button
            type="button"
            title="Open full thread"
            onClick={() => push(`/${workspaceSlug}/inbox?chat=${message.session_id}`)}
            className="rounded p-0.5 text-muted-foreground hover:text-foreground"
          >
            <ExternalLink className="size-3" />
          </button>
        </div>
      </div>
      <p className="line-clamp-3 text-xs text-foreground/90">{message.content}</p>
    </div>
  );
}
