"use client";

import { useState } from "react";
import { ChevronRight, Pencil } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import type { Session } from "./types";
import { useUpdateSession } from "./use-sessions";

const statusLabel = {
  todo: "Todo",
  in_progress: "In progress",
  done: "Done",
} as const;

// FIR-1787 (review of FIR-1769) — the session headline is the top of the thread
// box, not a separate floating card: a slim header row that sits at the top of
// the session's box with a divider underneath, so a session reads exactly like
// a thread did before. The name is editable inline (Jesper: "man skal kunne
// navngive en session headline"); an agent can also name it at handoff.
//
// The active session is NOT collapsible — it always shows its thread (Jesper:
// "den aktive session skal ikke være collapsed"). Only closed/past sessions get
// the collapse chevron so they can fold into a chapter.
export function SessionHeader({
  issueId,
  session,
  open,
  active = false,
  onToggle,
}: {
  issueId: string;
  session: Session;
  open: boolean;
  active?: boolean;
  onToggle: () => void;
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
          <span className="rounded-full border px-2 py-0.5 text-xs text-muted-foreground">
            {statusLabel[session.status]}
          </span>
        </div>
      )}
    </div>
  );
}
