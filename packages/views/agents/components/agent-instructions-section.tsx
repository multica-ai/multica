"use client";

import { useLayoutEffect, useRef, useState } from "react";
import { Pencil, X } from "@multica/ui/icons";
import type { Agent } from "@multica/core/types";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { cn } from "@multica/ui/lib/utils";
import { ReadonlyContent } from "../../editor/readonly-content";
import {
  ObjectSection,
  ObjectSectionAction,
} from "../../layout/object-page";
import { useT } from "../../i18n";
import { InstructionsTab } from "./tabs/instructions-tab";

/**
 * The agent's instructions: the prompt as it reads, clamped to a few lines,
 * and the full editor in place of it while editing.
 */
export function AgentInstructionsSection({
  agent,
  canEdit,
  onUpdate,
}: {
  agent: Agent;
  canEdit: boolean;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
}) {
  const { t } = useT("agents");
  const [editing, setEditing] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [confirmDiscard, setConfirmDiscard] = useState(false);

  const stopEditing = () => {
    setEditing(false);
    setDirty(false);
  };

  return (
    <ObjectSection
      title={t(($) => $.tabs.instructions)}
      action={
        !canEdit ? null : editing ? (
          <ObjectSectionAction
            icon={X}
            onClick={() => (dirty ? setConfirmDiscard(true) : stopEditing())}
          >
            {t(($) => $.inspector.cancel)}
          </ObjectSectionAction>
        ) : (
          <ObjectSectionAction icon={Pencil} onClick={() => setEditing(true)}>
            {t(($) => $.sections.edit)}
          </ObjectSectionAction>
        )
      }
    >
      {editing ? (
        <InstructionsTab
          agent={agent}
          onSave={async (updates) => {
            await onUpdate(agent.id, updates);
            stopEditing();
          }}
          onDirtyChange={setDirty}
        />
      ) : (
        <InstructionsPreview content={agent.instructions ?? ""} />
      )}

      {confirmDiscard && (
        <AlertDialog
          open
          onOpenChange={(open) => {
            if (!open) setConfirmDiscard(false);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.tabs.discard_dialog_title)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.tabs.discard_dialog_description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.tabs.discard_keep)}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmDiscard(false);
                  stopEditing();
                }}
              >
                {t(($) => $.tabs.discard_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </ObjectSection>
  );
}

function InstructionsPreview({ content }: { content: string }) {
  const { t } = useT("agents");
  const { t: tc } = useT("common");
  const bodyRef = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);

  // Measured rather than assumed: only a prompt taller than the clamp gets
  // the toggle. Re-measured on resize, because a narrower column wraps more.
  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el || expanded) return;
    const measure = () => setOverflows(el.scrollHeight > el.clientHeight + 1);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [content, expanded]);

  if (!content.trim()) {
    return (
      <p className="text-body text-muted-foreground">
        {t(($) => $.sections.instructions_empty)}
      </p>
    );
  }

  return (
    <div className="rounded-lg border bg-card px-4 py-3">
      <div
        ref={bodyRef}
        className={cn("text-body", !expanded && "max-h-24 overflow-hidden")}
      >
        <ReadonlyContent content={content} />
      </div>
      {overflows || expanded ? (
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((value) => !value)}
          className="mt-1 rounded-sm text-caption font-medium text-brand hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          {expanded
            ? tc(($) => $.expandable_description.collapse)
            : tc(($) => $.expandable_description.expand)}
        </button>
      ) : null}
    </div>
  );
}
