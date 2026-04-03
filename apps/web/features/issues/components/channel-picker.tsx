"use client";

import { useState, useEffect, useCallback } from "react";
import { Hash, Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { api } from "@/shared/api";
import type { Channel, IssueChannel } from "@/shared/types";

interface ChannelPickerProps {
  issueId: string;
}

export function ChannelPicker({ issueId }: ChannelPickerProps) {
  const [issueChannels, setIssueChannels] = useState<IssueChannel[]>([]);
  const [allChannels, setAllChannels] = useState<Channel[]>([]);
  const [open, setOpen] = useState(false);

  const load = useCallback(async () => {
    try {
      const [ic, all] = await Promise.all([
        api.listIssueChannels(issueId),
        api.listChannels(),
      ]);
      setIssueChannels(ic);
      setAllChannels(all);
    } catch {
      // ignore
    }
  }, [issueId]);

  useEffect(() => { load(); }, [load]);

  const assignedIds = new Set(issueChannels.map((ic) => ic.channel_id));
  const available = allChannels.filter((c) => !assignedIds.has(c.id));

  const handleAssign = async (channelId: string) => {
    try {
      await api.assignChannel(issueId, channelId);
      await load();
      setOpen(false);
    } catch {
      // ignore
    }
  };

  const handleUnassign = async (channelId: string) => {
    try {
      await api.unassignChannel(issueId, channelId);
      setIssueChannels((prev) => prev.filter((ic) => ic.channel_id !== channelId));
    } catch {
      // ignore
    }
  };

  if (allChannels.length === 0 && issueChannels.length === 0) {
    return <span className="text-muted-foreground">None</span>;
  }

  return (
    <div className="flex flex-wrap items-center gap-1">
      {issueChannels.map((ic) => (
        <span
          key={ic.id}
          className="inline-flex items-center gap-0.5 rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium group"
        >
          <Hash className="h-3 w-3 text-muted-foreground" />
          {ic.name}
          <button
            onClick={() => handleUnassign(ic.channel_id)}
            className="ml-0.5 opacity-0 group-hover:opacity-100 transition-opacity"
          >
            <X className="h-3 w-3 text-muted-foreground hover:text-foreground" />
          </button>
        </span>
      ))}

      {available.length > 0 && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger className="inline-flex h-5 w-5 items-center justify-center rounded hover:bg-accent transition-colors">
            <Plus className="h-3 w-3" />
          </PopoverTrigger>
          <PopoverContent align="start" className="w-48 p-1">
            {available.map((ch) => (
              <button
                key={ch.id}
                onClick={() => handleAssign(ch.id)}
                className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-xs hover:bg-accent transition-colors"
              >
                <Hash className="h-3 w-3 text-muted-foreground" />
                {ch.name}
              </button>
            ))}
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}
