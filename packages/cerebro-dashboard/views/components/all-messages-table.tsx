"use client";

import { useQuery } from "@tanstack/react-query";
import { allMessagesOptions } from "../../core/queries";
import { useDashboardStore } from "../../core/store";
import type { ActorMessage } from "../../core/api";

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("da-DK", {
      day: "2-digit",
      month: "short",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return "";
  }
}

export function AllMessagesTable({ wsId }: { wsId: string }) {
  const range = useDashboardStore((s) => s.range);
  const openMessagePanel = useDashboardStore((s) => s.openMessagePanel);
  const { data, isLoading } = useQuery(allMessagesOptions(wsId, range));

  const messages = data?.messages ?? [];

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex items-center justify-between border-b px-4 py-2.5">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Alle beskeder i perioden
        </h3>
        {!isLoading && (
          <span className="text-xs text-muted-foreground">{messages.length} beskeder</span>
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
          Ingen beskeder i den valgte periode.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b text-left text-[11px] text-muted-foreground">
                <th className="px-3 py-2 font-medium">Tidspunkt</th>
                <th className="px-3 py-2 font-medium">Fra</th>
                <th className="px-3 py-2 font-medium">Til (agent)</th>
                <th className="px-3 py-2 font-medium">Issue</th>
                <th className="px-3 py-2 font-medium">Besked</th>
              </tr>
            </thead>
            <tbody>
              {messages.map((msg) => (
                <MessageRow
                  key={msg.id}
                  msg={msg}
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
  );
}

function MessageRow({
  msg,
  onSenderClick,
}: {
  msg: ActorMessage;
  onSenderClick: () => void;
}) {
  return (
    <tr className="border-b last:border-0 hover:bg-accent/30 transition-colors">
      <td className="whitespace-nowrap px-3 py-2 tabular-nums text-muted-foreground">
        {formatTime(msg.created_at)}
      </td>
      <td className="px-3 py-2">
        {msg.sender_name ? (
          <button
            type="button"
            onClick={onSenderClick}
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
    </tr>
  );
}
