"use client";

import { useEffect, useRef, useState } from "react";
import { X } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "@multica/i18n/react";
import { UnicodeSpinner } from "@multica/ui/components/common/unicode-spinner";
import type { BrailleSpinnerName } from "unicode-animations";
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
  /** When set, `onCancel` is exposed once the task crosses the long-run threshold. */
  onCancel?: () => void;
}

interface Stage {
  /** Standalone label, capitalised so it reads as a complete short phrase
   *  ("Searching the web · 14s") without needing a subject. Matches the
   *  ChatGPT / Cursor / Claude style — the agent identity is already on
   *  the chat header, so we don't repeat it inline. */
  label: string;
  /** null = static (offline / unstable spinning would feel anxious). */
  spinner: BrailleSpinnerName | null;
  /** Stage represents a stable holding state (offline / waiting). When true,
   *  the label is rendered without the shimmer animation — shimmer implies
   *  "the agent is actively doing something", which a holding state isn't. */
  static?: boolean;
}

// Tool → labelKey. Short, action-flavoured phrases — the daemon-reported tool
// slug is meaningful but ugly ("ToolUse: read"); these are the user-facing
// translations. Unknown tools fall back to "Working" rather than leaking
// the raw slug. labelKey is resolved to a localized label via the chat
// namespace inside pickStage.
const TOOL_STAGES: Record<string, { labelKey: string; spinner: BrailleSpinnerName }> = {
  bash: { labelKey: "pill_running_command", spinner: "helix" },
  exec: { labelKey: "pill_running_command", spinner: "helix" },
  read: { labelKey: "pill_reading_files", spinner: "scan" },
  glob: { labelKey: "pill_reading_files", spinner: "scan" },
  grep: { labelKey: "pill_searching_code", spinner: "scan" },
  write: { labelKey: "pill_making_edits", spinner: "cascade" },
  edit: { labelKey: "pill_making_edits", spinner: "cascade" },
  multi_edit: { labelKey: "pill_making_edits", spinner: "cascade" },
  multiedit: { labelKey: "pill_making_edits", spinner: "cascade" },
  web_search: { labelKey: "pill_searching_web", spinner: "orbit" },
  websearch: { labelKey: "pill_searching_web", spinner: "orbit" },
};

const STAGE_FALLBACK_KEY = "pill_working";
const STAGE_FALLBACK_SPINNER: BrailleSpinnerName = "helix";

// During the first-token gap (status=running but no task_message yet)
// the agent could be loading the model, opening an API session, or
// actually reasoning. Rotating the label by elapsed seconds — instead
// of pinning a single "Thinking..." — makes the wait feel progressive
// without claiming what the model is literally doing. Boundaries are
// tiered (each label implies "this is taking a bit longer") rather
// than randomised, which would jitter on every render.
function pickThinkingLabel(elapsedSecs: number, t: ReturnType<typeof useT>): string {
  if (elapsedSecs < 5) return t("pill_thinking");
  if (elapsedSecs < 15) return t("pill_reasoning");
  if (elapsedSecs < 30) return t("pill_working_through");
  return t("pill_closer_look");
}

// Pure stage decision. Two-tier signal: presence + status drive the
// queued/wait copy, then taskMessages drive the running-state label.
// Errors deliberately don't flip the pill — the timeline already renders
// the error inline, and overwriting the label would mask whatever the
// agent does next.
function pickStage(
  status: string | undefined,
  taskMessages: readonly TaskMessagePayload[],
  availability: AgentAvailability | undefined,
  elapsedSecs: number,
  t: ReturnType<typeof useT>,
): Stage {
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "offline"
  ) {
    return { label: t("pill_offline"), spinner: null, static: true };
  }
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "unstable"
  ) {
    return { label: t("pill_reconnecting"), spinner: "pulse" };
  }
  if (status === "queued") return { label: t("pill_queued"), spinner: "pulse" };
  if (status === "dispatched") return { label: t("pill_starting_up"), spinner: "breathe" };

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

  // No task_message yet — first-token delay. Rotate the thinking label
  // by elapsed so the user perceives progressive waiting rather than
  // a stuck "Thinking..." loop.
  if (!latest) {
    return { label: pickThinkingLabel(elapsedSecs, t), spinner: "breathe" };
  }

  if (latest.type === "thinking") {
    return { label: pickThinkingLabel(elapsedSecs, t), spinner: "breathe" };
  }
  if (latest.type === "text") {
    return { label: t("pill_typing"), spinner: "braille" };
  }
  if (latest.type === "tool_use") {
    const tool = (latest.tool ?? "").toLowerCase();
    const mapped = TOOL_STAGES[tool];
    if (mapped) {
      return { label: t(mapped.labelKey), spinner: mapped.spinner };
    }
    return { label: t(STAGE_FALLBACK_KEY), spinner: STAGE_FALLBACK_SPINNER };
  }
  return { label: pickThinkingLabel(elapsedSecs, t), spinner: "breathe" };
}

const WARNING_THRESHOLD_S = 60;
const CANCEL_THRESHOLD_S = 300;

export function TaskStatusPill({
  pendingTask,
  taskMessages,
  availability,
  onCancel,
}: Props) {
  const t = useT("chat");
  const c = useT("common");

  // Anchor: locked on first render. Once set we never reassign — otherwise
  // the timer would visibly snap backwards when an optimistic-seeded
  // `Date.now()` anchor is later replaced by a server-side created_at that
  // happened a few hundred ms earlier. Monotonic elapsed > strict accuracy.
  const anchorRef = useRef<number | null>(null);
  if (anchorRef.current === null) {
    if (pendingTask.created_at) {
      const parsed = Date.parse(pendingTask.created_at);
      anchorRef.current = Number.isFinite(parsed) ? parsed : Date.now();
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
  const stage = pickStage(status, taskMessages, availability, elapsedSecs, t);
  const isWarning = elapsedSecs >= WARNING_THRESHOLD_S;
  const showCancel = !!onCancel && elapsedSecs >= CANCEL_THRESHOLD_S;

  // Shimmer the label whenever the agent is actively doing something —
  // skipped for `static` stages (offline holding) and `isWarning` (the
  // amber colour is the signal we want, shimmer would mute it under the
  // gradient mask).
  const animateLabel = !stage.static && !isWarning;

  return (
    <div
      className={cn(
        "flex items-center gap-1.5 px-1 text-xs",
        isWarning ? "text-amber-700 dark:text-amber-300" : "text-muted-foreground",
      )}
      aria-live="polite"
    >
      {stage.spinner && (
        <UnicodeSpinner name={stage.spinner} className="opacity-70" />
      )}
      <span className="truncate">
        <span className={cn(animateLabel && "animate-chat-text-shimmer")}>
          {stage.label}
        </span>
        <span className="opacity-70"> · {formatElapsedSecs(elapsedSecs)}</span>
      </span>
      {showCancel && (
        <button
          type="button"
          onClick={onCancel}
          className="ml-2 inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium text-foreground hover:bg-accent transition-colors"
        >
          <X className="size-3" />
          {c("cancel")}
        </button>
      )}
    </div>
  );
}
