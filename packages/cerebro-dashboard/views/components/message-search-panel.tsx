// CEREBRO-PATCH(channel-message-search-admin): FIR-407 — workspace-admin search
// across every channel and DM in the workspace, for agent coaching. Lives on
// the dashboard (the admin coaching surface); the endpoint enforces the
// admin/owner gate server-side, so a non-admin who reaches the panel still gets
// nothing back.
"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Hash, MessageSquare, Search } from "lucide-react";
import { api } from "@multica/core/api";
import { useActorName } from "@multica/core/workspace/hooks";
import { useNavigation } from "@multica/views/navigation";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

const DEBOUNCE_MS = 250;

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

export function MessageSearchPanel({ workspaceSlug }: { workspaceSlug: string }) {
  const enabled = useFeatureFlag("cerebro_channel_message_search");
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const { getActorName } = useActorName();
  const { push } = useNavigation();

  useEffect(() => {
    const handle = setTimeout(() => setDebounced(query.trim()), DEBOUNCE_MS);
    return () => clearTimeout(handle);
  }, [query]);

  const { data, isFetching } = useQuery({
    queryKey: ["cerebro", "admin-channel-message-search", debounced],
    queryFn: () => api.searchAllChannelMessages(debounced),
    enabled: enabled && debounced.length > 0,
    staleTime: 30_000,
  });

  if (!enabled) return null;

  const messages = data?.messages ?? [];
  const total = data?.total ?? 0;

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex items-center justify-between gap-3 border-b px-4 py-2.5">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Søg i alle samtaler
        </h3>
        {debounced.length > 0 && !isFetching && (
          <span className="text-xs text-muted-foreground tabular-nums">
            {total} {total === 1 ? "resultat" : "resultater"}
          </span>
        )}
      </div>

      <div className="p-3">
        <div className="flex items-center gap-2 rounded-md border bg-background px-3 py-1.5">
          <Search className="size-3.5 shrink-0 text-muted-foreground" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Søg i beskeder på tværs af alle channels og DMs…"
            aria-label="Søg i alle samtaler"
            className="w-full border-0 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>

        {debounced.length === 0 ? (
          <p className="px-1 py-6 text-center text-xs text-muted-foreground">
            Skriv et søgeord for at finde beskeder i alle samtaler — inkl. private DMs (admin-adgang).
          </p>
        ) : isFetching && messages.length === 0 ? (
          <div className="space-y-px py-2">
            {[0, 1, 2, 3].map((i) => (
              <div key={i} className="h-8 animate-pulse rounded bg-muted" />
            ))}
          </div>
        ) : messages.length === 0 ? (
          <p className="px-1 py-6 text-center text-sm text-muted-foreground">
            Ingen beskeder matcher “{debounced}”.
          </p>
        ) : (
          <ul className="mt-2 divide-y">
            {messages.map((m) => {
              const isDM = m.channel_kind === "dm";
              const channelLabel = m.channel_title || (isDM ? "Direkte besked" : "Kanal");
              return (
                <li key={m.id}>
                  <button
                    type="button"
                    onClick={() =>
                      push(`/${workspaceSlug}/inbox?issue=${encodeURIComponent(m.channel_id)}`)
                    }
                    className="flex w-full flex-col gap-1 px-1 py-2.5 text-left hover:bg-accent/30 transition-colors"
                  >
                    <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                      {isDM ? (
                        <MessageSquare className="size-3 shrink-0" />
                      ) : (
                        <Hash className="size-3 shrink-0" />
                      )}
                      <span className="truncate font-medium text-foreground">{channelLabel}</span>
                      <span className="shrink-0">·</span>
                      <span className="truncate">{getActorName(m.author_type, m.author_id)}</span>
                      <span className="ml-auto shrink-0 tabular-nums">{formatTime(m.created_at)}</span>
                    </div>
                    <p className="line-clamp-2 text-xs text-foreground/80">{m.snippet || m.content}</p>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
