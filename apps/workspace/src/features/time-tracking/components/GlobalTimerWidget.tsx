"use client";

import { useState, useRef } from "react";
import { Clock, Square, Play } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";
import { useCurrentTimerQuery, useStartTimerMutation, useStopTimerMutation } from "../hooks/use-time-tracking";
import { LiveDuration } from "./LiveDuration";

/**
 * Compact timer widget shown in the sidebar.
 *
 * Idle → opens a popover where the user types a description and hits Start.
 * Running → shows a live counter with a stop button.
 */
export function GlobalTimerWidget() {
  const [open, setOpen] = useState(false);
  const [description, setDescription] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  const { data: currentEntry } = useCurrentTimerQuery();
  const startMutation = useStartTimerMutation();
  const stopMutation = useStopTimerMutation();

  const isRunning = !!currentEntry;

  const handleStart = () => {
    const now = new Date().toISOString();
    startMutation.mutate(
      { description: description.trim() || undefined, start_time: now },
      {
        onSuccess: () => {
          setDescription("");
          setOpen(false);
        },
        onError: () => {
          toast.error("Failed to start timer");
        },
      },
    );
  };

  const handleStop = () => {
    if (!currentEntry) return;
    stopMutation.mutate(currentEntry.id, {
      onError: () => toast.error("Failed to stop timer"),
    });
  };

  // ── Running state ───────────────────────────────────────────────────────────
  if (isRunning && currentEntry) {
    return (
      <SidebarMenuItem>
        <div className="flex items-center gap-2 px-2 py-1.5 text-sm">
          {/* Live counter */}
          <Clock className="size-4 shrink-0 text-brand" />
          <div className="min-w-0 flex-1">
            {currentEntry.description ? (
              <p className="truncate text-xs text-muted-foreground">{currentEntry.description}</p>
            ) : null}
            <LiveDuration
              entry={currentEntry}
              className="text-sm font-semibold text-foreground"
            />
          </div>
          {/* Stop button */}
          <Button
            size="icon"
            variant="ghost"
            className="size-7 shrink-0 text-muted-foreground hover:text-destructive"
            disabled={stopMutation.isPending}
            onClick={handleStop}
            aria-label="Stop timer"
          >
            <Square className="size-3.5 fill-current" />
          </Button>
        </div>
      </SidebarMenuItem>
    );
  }

  // ── Idle state ──────────────────────────────────────────────────────────────
  return (
    <SidebarMenuItem>
      <Popover
        open={open}
        onOpenChange={(v) => {
          setOpen(v);
          // Auto-focus the description input when popover opens.
          if (v) setTimeout(() => inputRef.current?.focus(), 50);
        }}
      >
        <PopoverTrigger render={<SidebarMenuButton className="text-muted-foreground" />}>
          <Clock />
          <span>Track time</span>
        </PopoverTrigger>
        <PopoverContent side="right" align="end" className="w-64 p-3" sideOffset={8}>
          <p className="mb-2 text-xs font-medium text-muted-foreground">Start timer</p>
          <Input
            ref={inputRef}
            placeholder="What are you working on?"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleStart();
            }}
            className="mb-3 h-8 text-sm"
          />
          <Button
            size="sm"
            className="w-full"
            disabled={startMutation.isPending}
            onClick={handleStart}
          >
            <Play className="mr-1.5 size-3.5" />
            Start
          </Button>
        </PopoverContent>
      </Popover>
    </SidebarMenuItem>
  );
}
