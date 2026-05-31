"use client";

import { useMemo, useState } from "react";
import { Check } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { MemberWithUser } from "@multica/core/types";
import { memberListOptions } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";

/**
 * Compact workspace-member picker. `mode="single"` returns the chosen user id
 * (or null when the same row is clicked again, to clear); `mode="multi"`
 * toggles ids in a set. Built on the shared Popover + ActorAvatar so it matches
 * the look of the issue assignee picker without re-exporting upstream internals.
 */
export function MemberPicker({
  wsId,
  mode,
  selectedIds,
  exclude = [],
  onToggle,
  trigger,
  align = "start",
}: {
  wsId: string;
  mode: "single" | "multi";
  selectedIds: string[];
  exclude?: string[];
  onToggle: (userId: string) => void;
  trigger: React.ReactNode;
  align?: "start" | "center" | "end";
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const query = filter.trim().toLowerCase();
  const filtered = useMemo<MemberWithUser[]>(
    () =>
      members.filter(
        (m) =>
          !exclude.includes(m.user_id) &&
          m.name.toLowerCase().includes(query),
      ),
    [members, exclude, query],
  );

  return (
    <Popover
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) setFilter("");
      }}
    >
      <PopoverTrigger render={trigger as React.ReactElement} />
      <PopoverContent align={align} className="w-64 gap-0 p-0">
        <div className="border-b px-2 py-1.5">
          <input
            autoFocus
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Søg medlem…"
            className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>
        <div className="max-h-72 overflow-y-auto p-1">
          {filtered.length === 0 ? (
            <div className="px-2 py-3 text-center text-sm text-muted-foreground">
              Ingen medlemmer.
            </div>
          ) : (
            filtered.map((m) => {
              const selected = selectedIds.includes(m.user_id);
              return (
                <button
                  key={m.user_id}
                  type="button"
                  onClick={() => {
                    onToggle(m.user_id);
                    if (mode === "single") setOpen(false);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent"
                >
                  <ActorAvatar
                    name={m.name}
                    initials={m.name.slice(0, 2).toUpperCase()}
                    avatarUrl={resolvePublicFileUrl(m.avatar_url)}
                    size={18}
                  />
                  <span className="min-w-0 flex-1 truncate">{m.name}</span>
                  <Check
                    className={`h-3.5 w-3.5 shrink-0 text-muted-foreground ${
                      selected ? "" : "invisible"
                    }`}
                  />
                </button>
              );
            })
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
