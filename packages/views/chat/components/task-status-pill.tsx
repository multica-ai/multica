"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { UnicodeSpinner } from "@multica/ui/components/common/unicode-spinner";
import type { AgentAvailability } from "@multica/core/agents";
import type { ChatPendingTask, TaskMessagePayload } from "@multica/core/types";
import { formatElapsedSecs } from "../lib/format";
import { useT } from "../../i18n";

interface Props {
  /** Server-authoritative pending-task snapshot (`created_at` anchors the timer). */
  pendingTask: ChatPendingTask;
  /** Live task-message stream — the latest non-error entry decides the running-stage label. */
  taskMessages: readonly TaskMessagePayload[];
  /** Resolved presence; pass `undefined` to suppress availability hints. */
  availability: AgentAvailability | undefined;
}

interface Stage {
  label: string;
  static?: boolean;
}

type StageKey =
  | "offline"
  | "reconnecting"
  | "queued"
  | "starting_up"
  | "thinking"
  | "typing";

type ToolKey =
  | "running_command"
  | "reading_files"
  | "searching_code"
  | "making_edits"
  | "searching_web"
  | "fallback";

// Tool slug → translation key. Unknown tools fall back to "Working".
const TOOL_KEY_BY_SLUG: Record<string, Exclude<ToolKey, "fallback">> = {
  bash: "running_command",
  exec: "running_command",
  read: "reading_files",
  glob: "reading_files",
  grep: "searching_code",
  write: "making_edits",
  edit: "making_edits",
  multi_edit: "making_edits",
  multiedit: "making_edits",
  web_search: "searching_web",
  websearch: "searching_web",
};

// Pure stage decision returning translation keys. The hook below maps these
// keys into localized labels — keeping the decision pure makes it easy to
// follow the priority rules without translation noise.
function pickStageKeys(
  status: string | undefined,
  taskMessages: readonly TaskMessagePayload[],
  availability: AgentAvailability | undefined,
): { stageKey: StageKey; toolKey?: ToolKey; static?: boolean } {
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "offline"
  ) {
    return { stageKey: "offline", static: true };
  }
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "unstable"
  ) {
    return { stageKey: "reconnecting" };
  }
  if (status === "queued") return { stageKey: "queued" };
  if (status === "dispatched") return { stageKey: "starting_up" };

  // running: latest meaningful message decides the label.
  let latest: TaskMessagePayload | null = null;
  for (let i = taskMessages.length - 1; i >= 0; i--) {
    const m = taskMessages[i];
    if (m && m.type !== "error" && m.type !== "tool_result") {
      latest = m;
      break;
    }
  }

  if (!latest) return { stageKey: "thinking" };
  if (latest.type === "thinking") return { stageKey: "thinking" };
  if (latest.type === "text") return { stageKey: "typing" };
  if (latest.type === "tool_use") {
    const tool = (latest.tool ?? "").toLowerCase();
    const toolKey = TOOL_KEY_BY_SLUG[tool] ?? "fallback";
    // tool_use is technically still "thinking + tool" — surface the tool
    // label in the toolKey channel; main stage label uses the tool one.
    return { stageKey: "thinking", toolKey };
  }
  return { stageKey: "thinking" };
}

function useResolveStage(): (
  status: string | undefined,
  taskMessages: readonly TaskMessagePayload[],
  availability: AgentAvailability | undefined,
) => Stage {
  const { t } = useT("chat");
  return (status, taskMessages, availability) => {
    const decision = pickStageKeys(status, taskMessages, availability);
    if (decision.toolKey) {
      return {
        label: t(($) => $.status_pill.tools[decision.toolKey!]),
      };
    }
    return {
      label: t(($) => $.status_pill.stages[decision.stageKey]),
      static: decision.static,
    };
  };
}

export function TaskStatusPill({
  pendingTask,
  taskMessages,
  availability,
}: Props) {
  const resolveStage = useResolveStage();
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
  // running; we trust that observation over a stale cache.
  const status = taskMessages.length > 0 ? "running" : pendingTask.status;
  const elapsedSecs = Math.max(0, Math.floor((now - anchor) / 1000));
  const stage = resolveStage(status, taskMessages, availability);

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
