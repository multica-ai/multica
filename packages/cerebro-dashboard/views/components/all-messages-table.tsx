"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { useNavigation } from "@multica/views/navigation";
import { allMessagesOptions } from "../../core/queries";
import { useDashboardStore } from "../../core/store";
import type { ActorMessage } from "../../core/api";
import { MessageDetailSheet } from "./message-detail-sheet";

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      day: "2-digit",
      month: "short",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return "";
  }
}

export function AllMessagesTable({ wsId, workspaceSlug }: { wsId: string; workspaceSlug: string }) {
  const range = useDashboardStore((s) => s.range);
  const scope = useDashboardStore((s) => s.scope);
  const actorId = useDashboardStore((s) => s.actorId);
  const openMessagePanel = useDashboardStore((s) => s.openMessagePanel);
  const { data, isLoading } = useQuery(allMessagesOptions(wsId, range, scope, actorId));
  const [selectedMessage, setSelectedMessage] = useState<ActorMessage | null>(null);

  const messages = data?.messages ?? [];

  return (
    <>
      <div className="rounded-lg border bg-card">
        <div className="flex items-center justify-between border-b px-4 py-2.5">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            All messages in period
          </h3>
          {!isLoading && (
            <span className="text-xs text-muted-foreground">{messages.length} messages</span>
          )}
        </div>

        {isLoading ? (
          <div className="space-y-px p-2">
            {[0, 1, 2, 3, 4].map((i) => (
              <div key={i} className="h-8 animate-pulse rounded bg-muted" />
            ))}
          </div>
        ) : messages.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No messages in the selected period.
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b text-left text-[11px] text-muted-foreground">
                  <th className="px-3 py-2 font-medium">Time</th>
                  <th className="px-3 py-2 font-medium">From</th>
                  <th className="px-3 py-2 font-medium">To (agent)</th>
                  <th className="px-3 py-2 font-medium">Issue</th>
                  <th className="px-3 py-2 font-medium">Message</th>
                  <th className="w-8 px-2 py-2" />
                </tr>
              </thead>
              <tbody>
                {messages.map((msg) => (
                  <MessageRow
                    key={msg.id}
                    msg={msg}
                    workspaceSlug={workspaceSlug}
                    onRowClick={() => setSelectedMessage(msg)}
                    onSenderClick={() => {
                      if (msg.sender_id && msg.sender_name) {
                        openMessagePanel(msg.sender_id, msg.sender_name);
                      }
                    }}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <MessageDetailSheet
        message={selectedMessage}
        wsId={wsId}
        workspaceSlug={workspaceSlug}
        onClose={() => setSelectedMessage(null)}
      />
    </>
  );
}

function MessageRow({
  msg,
  workspaceSlug,
  onRowClick,
  onSenderClick,
}: {
  msg: ActorMessage;
  workspaceSlug: string;
  onRowClick: () => void;
  onSenderClick: () => void;
}) {
  const { push } = useNavigation();

  return (
    <tr
      className="border-b last:border-0 hover:bg-accent/30 transition-colors cursor-pointer"
      onClick={onRowClick}
    >
      <td className="whitespace-nowrap px-3 py-2 tabular-nums text-muted-foreground">
        {formatTime(msg.created_at)}
      </td>
      <td className="px-3 py-2">
        {msg.sender_name ? (
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); onSenderClick(); }}
            className="font-medium text-foreground hover:underline"
          >
            {msg.sender_name}
          </button>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-muted-foreground">{msg.agent_name}</td>
      <td className="px-3 py-2 text-muted-foreground">
        {msg.issue_title ? (
          <span className="truncate max-w-[160px] inline-block" title={msg.issue_title}>
            #{msg.issue_number} {msg.issue_title}
          </span>
        ) : (
          <span>—</span>
        )}
      </td>
      <td className="px-3 py-2 max-w-xs">
        <p className="line-clamp-2 text-foreground/80">{msg.content}</p>
      </td>
      <td className="px-2 py-2">
        <button
          type="button"
          title="Open full thread"
          onClick={(e) => { e.stopPropagation(); push(`/${workspaceSlug}/inbox?chat=${msg.session_id}`); }}
          className="rounded p-1 text-muted-foreground hover:text-foreground"
        >
          <ExternalLink className="size-3" />
        </button>
      </td>
    </tr>
  );
}
