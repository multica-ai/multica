"use client";

import { AlertCircle } from "lucide-react";
import { resolveFailureReasonLabel } from "../failure-reason-label";

// FIR-3782: the answer to "why did this fail?" at the top of a failed run.
// Before this, the reason was sr-only in the execution log and the transcript
// was an undifferentiated event list, so a reader had to scroll and guess.
// Everything rendered here already exists on the timeline — the card captures
// nothing new, it just stops the reader from hunting for it.

/** Structural shape of a transcript timeline entry. */
interface FailureCardItem {
  seq: number;
  type: string;
  tool?: string;
  input?: Record<string, unknown>;
  output?: string;
  content?: string;
}

interface RunFailureCardProps {
  failureReason: string | null | undefined;
  items: FailureCardItem[];
}

const OUTPUT_TAIL_CHARS = 600;

/** The command of the last tool call, as one line. */
function lastCommand(items: FailureCardItem[]): string | null {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (item?.type !== "tool_use") continue;
    const raw = item.input?.command ?? item.input?.description ?? item.input?.file_path;
    if (typeof raw === "string" && raw.length > 0) return raw;
    if (item.tool) return item.tool;
  }
  return null;
}

/** The tail of the last output or error on the timeline — where failures land. */
function lastOutputTail(items: FailureCardItem[]): string | null {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (item?.type !== "tool_result" && item?.type !== "error") continue;
    const body = item.output ?? item.content ?? "";
    if (body.length === 0) continue;
    return body.length > OUTPUT_TAIL_CHARS ? body.slice(-OUTPUT_TAIL_CHARS) : body;
  }
  return null;
}

export function RunFailureCard({ failureReason, items }: RunFailureCardProps) {
  const label = resolveFailureReasonLabel(failureReason);
  if (label === null) return null;

  const command = lastCommand(items);
  const tail = lastOutputTail(items);

  return (
    <div className="mx-4 mb-3 rounded-lg border border-destructive/30 bg-destructive/5 p-3">
      <div className="flex items-start gap-2">
        <AlertCircle aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium text-destructive">{label}</p>
          {command && (
            <p
              className="mt-1.5 truncate font-mono text-[11px] text-muted-foreground"
              title={command}
            >
              Last step: {command}
            </p>
          )}
          {tail && (
            <pre className="mt-1.5 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded bg-muted/50 p-2 font-mono text-[11px] text-muted-foreground">
              {tail}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
}
