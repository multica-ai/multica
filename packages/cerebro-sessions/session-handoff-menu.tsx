"use client";

import { ChevronDown, ScrollText } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
} from "@multica/ui/components/ui/dropdown-menu";
import { useStartFresh } from "./use-sessions";

export interface OpenThread {
  id: string; // thread root comment id
  name: string;
}

// FIR-1874 point 2: the Send-button fold-out. "Start new session with Handoff"
// closes a chosen OPEN thread — resolving its root (= closing the session) and
// storing a handoff on its row — so the next comment starts a fresh session. When
// several threads are open the user picks which one to hand off. Renders nothing
// when no thread is open (there is nothing to hand off).
export function SessionHandoffMenu({
  issueId,
  openThreads,
}: {
  issueId: string;
  openThreads: OpenThread[];
}) {
  const handoff = useStartFresh(issueId);
  if (openThreads.length === 0) return null;

  async function start(rootCommentId: string) {
    await handoff.mutateAsync({ root_comment_id: rootCommentId });
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="secondary"
            size="icon-sm"
            aria-label="Start new session with Handoff"
            disabled={handoff.isPending}
          >
            <ChevronDown />
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Start new session with Handoff</DropdownMenuLabel>
        {openThreads.map((thread) => (
          <DropdownMenuItem key={thread.id} onClick={() => void start(thread.id)}>
            <ScrollText className="h-3.5 w-3.5" />
            Hand off: {thread.name}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
