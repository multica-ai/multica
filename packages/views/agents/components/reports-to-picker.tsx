"use client";

import { useState } from "react";
import type { Agent } from "@multica/core/types";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { User } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";

export function ReportsToPicker({
  agents,
  value,
  onChange,
  disabled = false,
  excludeAgentIds = [],
  disabledEmptyLabel = "Reports to: N/A (Root)",
  chooseLabel = "Reports to...",
}: {
  agents: Agent[];
  value: string | null;
  onChange: (id: string | null) => void;
  disabled?: boolean;
  excludeAgentIds?: string[];
  disabledEmptyLabel?: string;
  chooseLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const exclude = new Set(excludeAgentIds);
  const rows = agents.filter(
    (a) => !a.archived_at && !exclude.has(a.id),
  );
  const current = value ? agents.find((a) => a.id === value) : null;
  const archivedManager = current?.archived_at != null;
  const unknownManager = Boolean(value && !current);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        className={cn(
          "inline-flex max-w-full min-w-0 items-center gap-1.5 overflow-hidden rounded-md border border-border px-2 py-1 text-xs hover:bg-accent/50 transition-colors",
          archivedManager && "border-amber-600/45 bg-amber-500/5",
          disabled && "opacity-60 cursor-not-allowed",
        )}
      >
        {unknownManager ? (
          <>
            <User className="h-3 w-3 shrink-0 text-muted-foreground" />
            <span className="min-w-0 truncate text-muted-foreground">Unknown manager (stale ID)</span>
          </>
        ) : current ? (
          <>
            <ActorAvatar actorType="agent" actorId={current.id} size={14} className="shrink-0 rounded-sm" />
            <span
              className={cn(
                "min-w-0 truncate",
                archivedManager && "text-amber-900 dark:text-amber-200",
              )}
            >
              {`Reports to ${current.name}${archivedManager ? " (archived)" : ""}`}
            </span>
          </>
        ) : (
          <>
            <User className="h-3 w-3 shrink-0 text-muted-foreground" />
            <span className="min-w-0 truncate">
              {disabled ? disabledEmptyLabel : chooseLabel}
            </span>
          </>
        )}
      </PopoverTrigger>
      <PopoverContent className="w-56 p-1" align="start">
        <button
          type="button"
          className={cn(
            "flex items-center gap-2 w-full px-2 py-1.5 text-xs rounded hover:bg-accent/50",
            value === null && "bg-accent",
          )}
          onClick={() => {
            onChange(null);
            setOpen(false);
          }}
        >
          No manager
        </button>
        {archivedManager && (
          <div className="flex min-w-0 items-center gap-2 overflow-hidden px-2 py-1.5 text-xs text-muted-foreground border-b border-border mb-0.5">
            <ActorAvatar actorType="agent" actorId={current.id} size={14} className="shrink-0 rounded-sm" />
            <span className="min-w-0 truncate">
              Current: {current.name} (archived)
            </span>
          </div>
        )}
        {unknownManager && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground border-b border-border mb-0.5">
            Saved manager is missing from this workspace. Choose a new manager or clear.
          </div>
        )}
        {rows.map((a) => (
          <button
            type="button"
            key={a.id}
            className={cn(
              "flex items-center gap-2 w-full min-w-0 px-2 py-1.5 text-xs rounded hover:bg-accent/50 overflow-hidden",
              a.id === value && "bg-accent",
            )}
            onClick={() => {
              onChange(a.id);
              setOpen(false);
            }}
          >
            <ActorAvatar actorType="agent" actorId={a.id} size={14} className="shrink-0 rounded-sm" />
            <span className="min-w-0 truncate">{a.name}</span>
            <span className="text-muted-foreground ml-auto shrink-0">{a.status}</span>
          </button>
        ))}
      </PopoverContent>
    </Popover>
  );
}
