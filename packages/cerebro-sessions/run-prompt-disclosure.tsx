"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, MessageSquareText } from "lucide-react";
import type { AgentTask } from "@multica/core/types/agent";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { safeTaskText } from "./task-transcript-safety";

// FIR-1839 point 5: the Agent Runs transcript modal showed the agent's event
// timeline (thinking / tools / results) but NEVER the initial prompt that kicked
// the run off — so you could not read "all the context the issue got" for a run,
// only a 120-char teaser elsewhere. trigger_summary is the verbatim snapshot of
// that kickoff prompt (the full historical context posted to the session),
// falling back to the curated title. This disclosure renders it in full, openable
// and scrollable, as part of the modal header.
//
// Gated by the same cerebro_comment_chapters flag as the rest of the sessions UI
// so it ships and hides together; off → renders nothing and the modal is
// byte-for-byte unchanged.
export function RunPromptDisclosure({ task }: { task: AgentTask }) {
  const enabled = useFeatureFlag("cerebro_comment_chapters");
  const [open, setOpen] = useState(false);

  const prompt = safeTaskText(task.trigger_summary) || safeTaskText(task.title);
  if (!enabled || !prompt) return null;

  return (
    <div className="rounded-md border bg-muted/30">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
      >
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0" />
        )}
        <MessageSquareText className="h-3.5 w-3.5 shrink-0" />
        <span>Initial prompt</span>
        {!open && (
          <span className="ml-1 min-w-0 flex-1 truncate font-normal text-muted-foreground/70">
            {prompt}
          </span>
        )}
      </button>
      {open && (
        <div className="max-h-64 overflow-y-auto border-t px-3 py-2 text-xs leading-relaxed whitespace-pre-wrap break-words text-foreground/90">
          {prompt}
        </div>
      )}
    </div>
  );
}
