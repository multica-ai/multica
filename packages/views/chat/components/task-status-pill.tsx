"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { UnicodeSpinner } from "@multica/ui/components/common/unicode-spinner";
import type { AgentAvailability } from "@multica/core/agents";
import type { ChatPendingTask, TaskMessagePayload } from "@multica/core/types";
import { formatElapsedSecs } from "../lib/format";

interface Props {
  /** Server-authoritative pending-task snapshot (`created_at` anchors the timer). */
  pendingTask: ChatPendingTask;
  /** Live task-message stream — the latest non-error entry decides the running-stage label. */
  taskMessages: readonly TaskMessagePayload[];
  /** Resolved presence; pass `undefined` to suppress availability hints. */
  availability: AgentAvailability | undefined;
}

interface Stage {
  /** Standalone label, capitalised so it reads as a complete short phrase
   *  ("Searching the web · 14s") without needing a subject. Matches the
   *  ChatGPT / Cursor / Claude style — the agent identity is already on
   *  the chat header, so we don't repeat it inline. */
  label: string;
  /** Stage represents a stable holding state (offline / waiting). When true,
   *  the spinner is suppressed and the shimmer animation is disabled —
   *  shimmer / spinning implies "the agent is actively doing something",
   *  which a holding state isn't. */
  static?: boolean;
}

// Tool → label. Short, action-flavoured phrases — the daemon-reported tool
// slug is meaningful but ugly ("ToolUse: read"); these are the user-facing
// translations. Unknown tools fall back to "Working" rather than leaking
// the raw slug.
const TOOL_LABELS: Record<string, string> = {
  bash: "Running a command",
  exec: "Running a command",
  read: "Reading files",
  glob: "Reading files",
  grep: "Searching the code",
  write: "Making edits",
  edit: "Making edits",
  multi_edit: "Making edits",
  multiedit: "Making edits",
  web_search: "Searching the web",
  websearch: "Searching the web",
};

const TOOL_FALLBACK = "Working";

// Pure stage decision. Two-tier signal: presence + status drive the
// queued/wait copy, then taskMessages drive the running-state label.
// Errors deliberately don't flip the pill — the timeline already renders
// the error inline, and overwriting the label would mask whatever the
// agent does next.
function pickStage(
  status: string | undefined,
  taskMessages: readonly TaskMessagePayload[],
  availability: AgentAvailability | undefined,
): Stage {
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "offline"
  ) {
    return { label: "Offline", static: true };
  }
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "unstable"
  ) {
    return { label: "Reconnecting" };
  }
  if (status === "queued") return { label: "Queued" };
  if (status === "dispatched") return { label: "Starting up" };

  // running: latest meaningful message decides the label. We deliberately
  // skip both `error` rows (rendered inline by the timeline; flipping the
  // pill would mask the next real action) and `tool_result` rows
  // (tool_result is the completion event for a tool_use, not a new stage —
  // treating it as one made the pill flicker bash → Thinking → grep →
  // Thinking → web_search on every tool boundary, where reality is just
  // bash → grep → web_search).
  let latest: TaskMessagePayload | null = null;
  for (let i = taskMessages.length - 1; i >= 0; i--) {
    const m = taskMessages[i];
    if (m && m.type !== "error" && m.type !== "tool_result") {
      latest = m;
      break;
    }
  }

  if (!latest) return { label: "Thinking" };
  if (latest.type === "thinking") return { label: "Thinking" };
  if (latest.type === "text") return { label: "Typing" };
  if (latest.type === "tool_use") {
    const tool = (latest.tool ?? "").toLowerCase();
    return { label: TOOL_LABELS[tool] ?? TOOL_FALLBACK };
  }
  return { label: "Thinking" };
}

export function TaskStatusPill({
  pendingTask,
  taskMessages,
  availability,
}: Props) {
  // Anchor: locked on first render. Once set we never reassign — otherwise
  // the timer would visibly snap backwards when an optimistic-seeded
  // `Date.now()` anchor is later replaced by a server-side created_at that
  // happened a few hundred ms earlier. Monotonic elapsed > strict accuracy.
  const anchorRef = useRef<number | null>(null);
  if (anchorRef.current === null) {
    if (pendingTask.created_at) {
      const t = Date.parse(pendingTask.created_at);
      anchorRef.current = Number.isFinite(t) ? t : Date.now();
    } else {
      anchorRef.current = Date.now();
    }
  }
  const anchor = anchorRef.current;

  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);

  // Effective status — defense-in-depth derive on top of the cache. If any
  // task_message has streamed in, the daemon has by definition started
  // running; we trust that observation over a stale cache. Catches WS gaps,
  // reconnect windows, or out-of-order delivery where the cache hasn't been
  // writethrough'd yet.
  const status = taskMessages.length > 0 ? "running" : pendingTask.status;
  const elapsedSecs = Math.max(0, Math.floor((now - anchor) / 1000));
  const stage = pickStage(status, taskMessages, availability);

  return (
    <div
      className="flex items-center gap-1.5 px-1 text-xs text-muted-foreground"
      aria-live="polite"
    >
      {!stage.static && (
        <UnicodeSpinner name="breathe" className="opacity-70" />
      )}
      <span className="truncate">
        <span className={cn(!stage.static && "animate-chat-text-shimmer")}>
          {stage.label}
        </span>
        <span className="opacity-70"> · {formatElapsedSecs(elapsedSecs)}</span>
      </span>
    </div>
  );
}
