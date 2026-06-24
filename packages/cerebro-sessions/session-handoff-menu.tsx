"use client";

import { Check, ChevronDown, ScrollText } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
} from "@multica/ui/components/ui/dropdown-menu";

export interface OpenThread {
  id: string; // thread root comment id
  name: string;
}

// FIR-1874 point 2 (Jesper A): the Send-button fold-out arms "Start new session
// with Handoff". Picking an OPEN thread does NOT close it immediately — it arms
// the composer (the host shows a banner across the field), and the handoff (the
// chosen thread is resolved and a summary stored, so the next comment starts a
// fresh session) only fires when the user presses Send. The X on the banner
// cancels it before then. When several threads are open the user picks which one
// to hand off; the armed one shows a check. Renders nothing when no thread is
// open (there is nothing to hand off).
export function SessionHandoffMenu({
  openThreads,
  armedId,
  onArm,
}: {
  openThreads: OpenThread[];
  armedId?: string | null;
  onArm: (thread: OpenThread) => void;
}) {
  if (openThreads.length === 0) return null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="secondary"
            size="icon-sm"
            aria-label="Start new session with Handoff"
          >
            <ChevronDown />
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Start new session with Handoff</DropdownMenuLabel>
          {openThreads.map((thread) => (
            <DropdownMenuItem key={thread.id} onClick={() => onArm(thread)}>
              {armedId === thread.id ? (
                <Check className="h-3.5 w-3.5" />
              ) : (
                <ScrollText className="h-3.5 w-3.5" />
              )}
              Hand off: {thread.name}
            </DropdownMenuItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
