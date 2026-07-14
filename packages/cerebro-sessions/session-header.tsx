"use client";

import { useState } from "react";
import { ChevronRight, ScrollText } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import type { Session } from "./types";
import { useUpdateSession } from "./use-sessions";
import { SESSION_MODES, SESSION_MODE_STYLES, normalizeSessionMode, type SessionMode } from "./modes";

// FIR-2283 followup: workflow phase badge shown next to Open/Resolved.
const PHASE_LABELS: Record<string, string> = {
  plan: "Plan",
  build: "Build",
  review: "Review",
};
function phaseLabel(phase: string): string {
  return PHASE_LABELS[phase] ?? phase.charAt(0).toUpperCase() + phase.slice(1);
}
const PHASE_CLASSES: Record<string, string> = {
  plan: "border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400",
  build: "border-blue-500/30 bg-blue-500/10 text-blue-600 dark:text-blue-400",
  review: "border-purple-500/30 bg-purple-500/10 text-purple-600 dark:text-purple-400",
};

// FIR-1787 (review of FIR-1769) — the session headline is the top of the thread
// box, not a separate floating card: a slim header row that sits at the top of
// the session's box with a divider underneath, so a session reads exactly like
// a thread did before. The name is editable inline (Jesper: "man skal kunne
// navngive en session headline"); an agent can also name it at handoff.
//
// The active session is NOT collapsible — it always shows its thread (Jesper:
// "den aktive session skal ikke være collapsed"). Only closed/past sessions get
// the collapse chevron so they can fold into a collapsed session.
export function SessionHeader({
  issueId,
  session,
  open,
  active = false,
  resolved = false,
  onToggle,
  hasHandoff = false,
  handoffOpen = false,
  onToggleHandoff,
}: {
  issueId: string;
  session: Session;
  open: boolean;
  active?: boolean;
  // FIR-1874: a session's state IS its thread root's resolved_at. Resolved =
  // closed session; otherwise open.
  resolved?: boolean;
  onToggle: () => void;
  // FIR-1839 point 7: when the session has a handoff brief, the header shows a
  // "Handoff" toggle so the brief reads as part of the headline and only opens
  // on demand. The body itself is rendered by the host (SessionHandoff).
  hasHandoff?: boolean;
  handoffOpen?: boolean;
  onToggleHandoff?: () => void;
}) {
  const update = useUpdateSession(issueId);
  const modesEnabled = useFeatureFlag("cerebro_session_modes");
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.name);
  // The implicit "Session 1" has no DB row yet, so it cannot be renamed.
  const canRename = session.id !== "default";
  const mode = normalizeSessionMode(session.mode);

  function startEdit() {
    if (!canRename) return;
    setDraft(session.name);
    setEditing(true);
  }

  async function commit() {
    const name = draft.trim();
    setEditing(false);
    if (!name || name === session.name) return;
    await update.mutateAsync({ sessionId: session.id, input: { name } });
  }

  // Jesper (FIR-1874): rename happens inline by double-clicking the name. The
  // stopPropagation keeps a double-click on a collapsed session's name from also
  // toggling its fold.
  const title = editing ? null : (
    <span
      className={cn("truncate text-sm font-semibold", canRename && "cursor-text")}
      onDoubleClick={
        canRename
          ? (e) => {
              e.stopPropagation();
              startEdit();
            }
          : undefined
      }
      title={canRename ? "Double-click to rename" : undefined}
    >
      {session.name}
    </span>
  );

  return (
    <div className="flex w-full items-center justify-between gap-2 border-b px-4 py-2.5">
      {/* Jesper (FIR-1874): the rename input must stay in place where the name
          sits — on the left — not jump across to the right where the status
          badge is. So it replaces the name in the left slot, and the status
          group hides while editing. */}
      {editing ? (
        <Input
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") commit();
            if (e.key === "Escape") setEditing(false);
          }}
          className="h-7 min-w-0 flex-1 text-sm font-semibold"
        />
      ) : active ? (
        // Active session: always open, no collapse affordance.
        <div className="flex min-w-0 flex-1 items-center text-left">{title}</div>
      ) : (
        <button
          type="button"
          onClick={onToggle}
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
        >
          <ChevronRight
            className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
          />
          {title}
        </button>
      )}
      {!editing && (
        <div className="flex shrink-0 items-center gap-2">
          {modesEnabled && (
            <Select
              value={mode}
              items={Object.fromEntries(SESSION_MODES.map(({ value, label }) => [value, label]))}
              onValueChange={(v) =>
                update.mutateAsync({
                  sessionId: session.id,
                  input: { mode: v as SessionMode },
                })
              }
            >
              <SelectTrigger
                size="sm"
                aria-label="Session mode"
                className={cn(
                  "h-auto rounded-full border px-2 py-0.5 text-xs transition-colors",
                  SESSION_MODE_STYLES[mode],
                )}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SESSION_MODES.map(({ value, label }) => (
                  <SelectItem key={value} value={value}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          {hasHandoff && onToggleHandoff ? (
            <button
              type="button"
              onClick={onToggleHandoff}
              aria-pressed={handoffOpen}
              className={cn(
                "flex items-center gap-1 rounded px-1.5 py-0.5 text-xs transition-colors",
                handoffOpen
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
              )}
              title="Show the handoff brief for this session"
            >
              <ScrollText className="h-3.5 w-3.5" />
              Handoff
            </button>
          ) : null}
          {session.phase ? (
            <span
              data-testid="session-phase-badge"
              className={cn(
                "rounded-full border px-2 py-0.5 text-xs font-medium",
                PHASE_CLASSES[session.phase] ?? "border-muted-foreground/30 text-muted-foreground",
              )}
            >
              {phaseLabel(session.phase)}
            </span>
          ) : null}
          <span
            className={cn(
              "rounded-full border px-2 py-0.5 text-xs",
              resolved ? "text-muted-foreground" : "border-primary/30 text-primary",
            )}
          >
            {resolved ? "Resolved" : "Open"}
          </span>
        </div>
      )}
    </div>
  );
}
