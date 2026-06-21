"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { useStartFresh } from "./use-sessions";
import type { SessionStartMode } from "./types";

// Model B Start fresh: no human form. The user picks one of two modes; the
// new comment lands in the resulting session by construction, and the closing
// session keeps an (agent or auto-generated) handoff either way.
export function StartFresh({ issueId }: { issueId: string }) {
  const [open, setOpen] = useState(false);
  const mutation = useStartFresh(issueId);

  async function start(mode: SessionStartMode) {
    await mutation.mutateAsync({ mode });
    setOpen(false);
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button type="button" variant="outline" size="sm" />}>
        <Plus className="h-4 w-4" />
        Start fresh
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Start a fresh session</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            This closes the current session and opens a new one. New comments land in the new session
            automatically.
          </p>
          <div className="space-y-2">
            <Button
              type="button"
              className="w-full justify-start"
              onClick={() => start("handoff")}
              disabled={mutation.isPending}
            >
              Carry over handoff
            </Button>
            <p className="text-xs text-muted-foreground">
              The new session opens with a short handoff summarising the session you are closing.
            </p>
            <Button
              type="button"
              variant="outline"
              className="w-full justify-start"
              onClick={() => start("blank")}
              disabled={mutation.isPending}
            >
              Start blank
            </Button>
            <p className="text-xs text-muted-foreground">
              The new session opens empty — no context carried over. The closing session still keeps its
              handoff.
            </p>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
