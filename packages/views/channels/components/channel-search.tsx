"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Search, Hash, Lock, MessageCircle } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelSearchOptions } from "@multica/core/channels";
import { useNavigation } from "../../navigation";
import { useRequiredWorkspaceSlug, paths } from "@multica/core/paths";
import type { ChannelSearchHit } from "@multica/core/types";

interface ChannelSearchProps {
  /** When set, search is scoped to this channel only. Header passes
   * the active channel id; the global "Channels" page passes null. */
  channelId: string | null;
  enabled: boolean;
}

/**
 * ChannelSearch — popover-style search bar. Opens on focus, debounces
 * the input by 200ms, and lists hits with a "#channel" prefix + a
 * snippet of the matched body. Clicking a result navigates to that
 * channel; the user can refine the term in-place without losing focus.
 *
 * Phase 5c v1: text-only ranking via ts_rank (server-side). Highlighting
 * matched terms in the snippet is a follow-up — the spec doesn't
 * require it and the bare snippet is readable enough.
 */
export function ChannelSearch({ channelId, enabled }: ChannelSearchProps) {
  const wsId = useWorkspaceId();
  const slug = useRequiredWorkspaceSlug();
  const navigation = useNavigation();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [debouncedQ, setDebouncedQ] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);

  // Debounce the q → debouncedQ pipe so we don't fire a query per
  // keystroke. 200ms feels live enough without flooding the server.
  useEffect(() => {
    const id = window.setTimeout(() => setDebouncedQ(q.trim()), 200);
    return () => window.clearTimeout(id);
  }, [q]);

  const { data: hits = [], isFetching } = useQuery(
    channelSearchOptions(wsId, debouncedQ, channelId, enabled && open),
  );

  const placeholder = useMemo(() => {
    if (channelId) return "Search this channel…";
    return "Search channels…";
  }, [channelId]);

  const handleNavigate = (hit: ChannelSearchHit) => {
    setOpen(false);
    setQ("");
    setDebouncedQ("");
    navigation.push(paths.workspace(slug).channelDetail(hit.channel_id));
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            size="sm"
            variant="outline"
            aria-label="Search messages"
            className="gap-2"
            onClick={() => {
              setOpen(true);
              // Focus the input on next paint when the popover mounts.
              setTimeout(() => inputRef.current?.focus(), 0);
            }}
          >
            <Search className="h-3.5 w-3.5" />
            <span className="hidden sm:inline">Search</span>
          </Button>
        }
      />
      <PopoverContent className="w-[480px] max-w-[80vw] p-0" align="end">
        <div className="border-b border-border p-2">
          <Input
            ref={inputRef}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={placeholder}
            className="h-8"
          />
        </div>
        <div className="max-h-96 overflow-y-auto">
          {!debouncedQ ? (
            <p className="p-4 text-sm text-muted-foreground">
              Start typing to search.
            </p>
          ) : isFetching && hits.length === 0 ? (
            <p className="p-4 text-sm text-muted-foreground">Searching…</p>
          ) : hits.length === 0 ? (
            <p className="p-4 text-sm text-muted-foreground">No matches.</p>
          ) : (
            <ul role="listbox" aria-label="Search results">
              {hits.map((hit) => (
                <li key={hit.id}>
                  <button
                    type="button"
                    role="option"
                    aria-selected={false}
                    onClick={() => handleNavigate(hit)}
                    className="block w-full px-3 py-2 text-left text-sm hover:bg-muted/60"
                  >
                    <div className="flex items-center gap-1 text-xs text-muted-foreground">
                      {hit.channel_kind === "dm" ? (
                        <MessageCircle className="h-3 w-3" />
                      ) : hit.channel_display_name ? (
                        <Hash className="h-3 w-3" />
                      ) : (
                        <Lock className="h-3 w-3" />
                      )}
                      <span>
                        {hit.channel_kind === "dm"
                          ? "Direct message"
                          : hit.channel_display_name || hit.channel_name}
                      </span>
                    </div>
                    <div className="mt-1 line-clamp-2 text-foreground">
                      {hit.content}
                    </div>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
