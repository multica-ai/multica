"use client";

import { useState } from "react";
import { Archive, ArrowRight, Bot } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";

interface BacklogAgentHintDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDismissPermanently: () => void;
  onMoveToTodo: () => void;
}

export function BacklogAgentHintDialog({
  open,
  onOpenChange,
  onDismissPermanently,
  onMoveToTodo,
}: BacklogAgentHintDialogProps) {
  const [dontShowAgain, setDontShowAgain] = useState(false);

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="w-[calc(100vw-2rem)] !max-w-[460px] gap-0 overflow-hidden rounded-lg p-0">
        <div className="px-5 pb-4 pt-5">
          <div className="flex items-start gap-3">
            <div className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
              <Bot className="size-4" />
            </div>
            <div className="min-w-0">
              <AlertDialogTitle className="text-base font-semibold">
                Agent is paused in Backlog
              </AlertDialogTitle>
              <AlertDialogDescription className="mt-1 text-sm leading-5 text-muted-foreground">
                This issue is parked, so the assigned agent will wait. Move it
                to Todo when you want the agent to start.
              </AlertDialogDescription>
            </div>
          </div>

          <div className="mt-4 flex items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-xs">
            <span className="inline-flex min-w-0 items-center gap-1.5 text-muted-foreground">
              <Archive className="size-3.5 shrink-0" />
              <span className="truncate">Backlog</span>
            </span>
            <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/70" />
            <span className="min-w-0 truncate font-medium">
              Todo starts the agent
            </span>
          </div>
        </div>

        <div className="border-t bg-muted/30 px-5 py-3">
          <div className="flex flex-col gap-3 sm:gap-2">
            <label className="flex items-center gap-2 cursor-pointer text-sm text-muted-foreground">
              <input
                type="checkbox"
                checked={dontShowAgain}
                onChange={(e) => setDontShowAgain(e.target.checked)}
                className="size-4 rounded border-border accent-primary cursor-pointer"
              />
              Don&apos;t show this again
            </label>
            <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
              <AlertDialogCancel
                className="w-full sm:w-auto"
                onClick={() => { if (dontShowAgain) onDismissPermanently(); }}
              >
                Keep in Backlog
              </AlertDialogCancel>
              <AlertDialogAction
                className="w-full sm:w-auto"
                onClick={() => { if (dontShowAgain) onDismissPermanently(); onMoveToTodo(); }}
              >
                Move to Todo
              </AlertDialogAction>
            </div>
          </div>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  );
}
