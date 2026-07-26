"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, MessageSquareText } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { AgentTask } from "@multica/core/types/agent";
import { ApiError } from "@multica/core/api";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  getAgentPromptSnapshot,
  type PromptSnapshotLayer,
} from "@multica/cerebro-agent-prompt/api";
import { safeTaskText } from "./task-transcript-safety";

// FIR-1839 point 5: the Agent Runs transcript modal showed the agent's event
// timeline (thinking / tools / results) but NEVER the initial prompt that kicked
// the run off — so you could not read "all the context the issue got" for a run,
// only a 120-char teaser elsewhere.
//
// FIR-3782: `trigger_summary` is only the triggering comment, truncated to 200
// chars server-side (buildCommentTriggerSummary) — it is NOT what the model
// read. The byte-exact prompt IS recorded per run by the daemon (FIR-3212) but
// was only reachable from the agent page's Production prompt tab, which is the
// wrong place to look when a run goes wrong. This disclosure now reads that
// snapshot directly, layer by layer, and falls back to the triggering comment
// for runs that have none.
//
// Two independent gates, deliberately:
//   cerebro_comment_chapters — whether this panel exists at all. UNCHANGED: it
//                              ships and hides with the rest of the sessions UI.
//   cerebro_run_full_prompt  — whether the panel shows the recorded prompt or
//                              only the triggering comment.
export function RunPromptDisclosure({ task }: { task: AgentTask }) {
  const enabled = useFeatureFlag("cerebro_comment_chapters");
  const fullPromptEnabled = useFeatureFlag("cerebro_run_full_prompt");
  const [open, setOpen] = useState(false);
  const [activeLayer, setActiveLayer] = useState<string | null>(null);

  const triggerPrompt =
    safeTaskText(task.trigger_summary) || safeTaskText(task.title);

  // Fetch on open, never on mount: the transcript dialog already spends one
  // request on the task-access snapshot, and a finished run's prompt is
  // immutable — so it stays cached for the session.
  const {
    data: snapshot,
    isPending,
    isError,
  } = useQuery({
    queryKey: ["cerebro", "run-prompt-snapshot", task.agent_id, task.id],
    // A run with no recorded prompt 404s, and that is the ordinary case for
    // older runs and runtimes that never report one — it is an absence, not a
    // failure, so it must not surface as an error. Same 404 → null convention
    // the API client already uses elsewhere.
    queryFn: async () => {
      try {
        return await getAgentPromptSnapshot(task.agent_id, task.id);
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null;
        throw err;
      }
    },
    enabled: enabled && fullPromptEnabled && open && Boolean(task.agent_id),
    staleTime: Infinity,
    retry: false,
  });

  if (!enabled || !triggerPrompt) return null;

  const layers = snapshot?.layers ?? [];
  const hasLayers = layers.length > 0;
  const selected = hasLayers
    ? (layers.find((l) => l.name === activeLayer) ?? layers[0]!)
    : null;
  // A disabled query reports `isPending` forever, so only treat it as loading
  // while a request can actually be in flight.
  const loading = fullPromptEnabled && isPending && !isError;

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
            {triggerPrompt}
          </span>
        )}
      </button>

      {open && (
        <div className="border-t">
          {selected ? (
            <FullPrompt
              layers={layers}
              selected={selected}
              onSelectLayer={setActiveLayer}
              totalBytes={snapshot?.total_bytes ?? 0}
              redacted={snapshot?.redacted === true}
            />
          ) : loading ? (
            <p className="px-3 py-2 text-xs text-muted-foreground">
              Loading the full prompt…
            </p>
          ) : (
            <TriggerOnly
              text={triggerPrompt}
              // Only explain the absence when the reader had reason to expect
              // more. With the flag off, the comment IS the intended content.
              note={
                !fullPromptEnabled
                  ? null
                  : isError
                    ? "The full prompt for this run could not be loaded. Showing the comment that triggered it."
                    : "No full prompt was recorded for this run. Showing the comment that triggered it."
              }
            />
          )}
        </div>
      )}
    </div>
  );
}

function TriggerOnly({ text, note }: { text: string; note: string | null }) {
  return (
    <div>
      {note && (
        <p className="border-b px-3 py-1.5 text-[11px] text-muted-foreground">
          {note}
        </p>
      )}
      <div className="max-h-64 overflow-y-auto px-3 py-2 text-xs leading-relaxed whitespace-pre-wrap break-words text-foreground/90">
        {text}
      </div>
    </div>
  );
}

/**
 * One layer at a time, not all of them: the runtime-brief layer alone runs to
 * tens of thousands of bytes, and this panel sits above the transcript's
 * virtualized event list inside the same dialog.
 */
function FullPrompt({
  layers,
  selected,
  onSelectLayer,
  totalBytes,
  redacted,
}: {
  layers: PromptSnapshotLayer[];
  selected: PromptSnapshotLayer;
  onSelectLayer: (name: string) => void;
  totalBytes: number;
  redacted: boolean;
}) {
  return (
    <div>
      <div className="flex flex-wrap items-center gap-1 border-b px-2 py-1.5">
        {layers.map((layer) => (
          <button
            key={layer.name}
            type="button"
            aria-pressed={layer.name === selected.name}
            onClick={() => onSelectLayer(layer.name)}
            className={`rounded px-1.5 py-0.5 font-mono text-[10px] transition-colors ${
              layer.name === selected.name
                ? "bg-muted font-medium text-foreground"
                : "text-muted-foreground hover:bg-muted/50"
            }`}
          >
            {layer.name}
            <span className="ml-1 text-muted-foreground/70">
              {formatBytes(layer.byte_size)}
            </span>
          </button>
        ))}
        <span className="ml-auto text-[10px] text-muted-foreground">
          {formatBytes(totalBytes)} total
          {redacted ? " · secrets redacted" : ""}
        </span>
      </div>
      <div className="max-h-64 overflow-auto px-3 py-2">
        <pre className="text-xs leading-relaxed whitespace-pre-wrap break-words text-foreground/90">
          {selected.content_redacted}
        </pre>
      </div>
    </div>
  );
}

/** Compact enough to sit inside a layer chip: 77623 → "76 KB". */
function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  return `${Math.round(bytes / 1024).toLocaleString("en-US")} KB`;
}
