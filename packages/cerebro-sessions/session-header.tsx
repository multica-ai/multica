"use client";

import { useState } from "react";
import { ChevronRight, Pencil, ScrollText } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import type { Session } from "./types";
import { useUpdateSession } from "./use-sessions";

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
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.name);
  // The implicit "Session 1" has no DB row yet, so it cannot be renamed.
  const canRename = session.id !== "default";

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

  const title = editing ? null : (
    <span className="truncate text-sm font-semibold">{session.name}</span>
  );

  return (
    <div className="flex w-full items-center justify-between gap-2 border-b px-4 py-2.5">
      {active ? (
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
          className="h-7 max-w-[16rem] text-sm font-semibold"
        />
      ) : (
        <div className="flex shrink-0 items-center gap-2">
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
          {canRename ? (
            <button
              type="button"
              onClick={startEdit}
              className="text-muted-foreground/60 transition-colors hover:text-foreground"
              aria-label="Rename session"
            >
              <Pencil className="h-3.5 w-3.5" />
            </button>
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
