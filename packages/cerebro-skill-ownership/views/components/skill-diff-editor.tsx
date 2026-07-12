"use client";

// FIR-2924 — Jesper: while reviewing a proposed skill change, let the
// reviewer edit the content directly instead of only approve/reject-as-is.
// Toggling "Edit" swaps in a textarea seeded with the proposed content; the
// diff below re-renders live against the edited text so the reviewer always
// sees exactly what they are about to approve. Edits are local until Approve
// is pressed — Reject always uses the original proposal (see caller).

import { useState } from "react";
import { Pencil, RotateCcw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { SkillDiffView } from "./skill-diff-view";

interface Props {
  base: string;
  proposed: string;
  baseLabel: string;
  proposedLabel: string;
  /** Current edited value, or undefined when the reviewer hasn't touched it. */
  editedContent: string | undefined;
  onEditedContentChange: (value: string | undefined) => void;
}

export function SkillDiffEditor({
  base,
  proposed,
  baseLabel,
  proposedLabel,
  editedContent,
  onEditedContentChange,
}: Props) {
  const [editing, setEditing] = useState(false);
  const isEdited = editedContent !== undefined && editedContent !== proposed;
  const displayed = editedContent ?? proposed;

  const startEditing = () => {
    if (editedContent === undefined) onEditedContentChange(proposed);
    setEditing(true);
  };

  const resetEdits = () => {
    onEditedContentChange(undefined);
    setEditing(false);
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        {isEdited ? (
          <span className="rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:text-amber-400">
            Edited before approving
          </span>
        ) : (
          <span />
        )}
        <div className="flex items-center gap-1.5">
          {isEdited && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-6 gap-1 px-2 text-xs text-muted-foreground"
              onClick={resetEdits}
            >
              <RotateCcw className="h-3 w-3" />
              Revert to proposed
            </Button>
          )}
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-6 gap-1 px-2 text-xs"
            onClick={() => (editing ? setEditing(false) : startEditing())}
          >
            <Pencil className="h-3 w-3" />
            {editing ? "Done editing" : "Edit"}
          </Button>
        </div>
      </div>

      {editing && (
        <Textarea
          value={displayed}
          onChange={(e) => onEditedContentChange(e.target.value)}
          rows={12}
          className="resize-y font-mono text-xs"
          placeholder="Edit the proposed content…"
        />
      )}

      <SkillDiffView
        base={base}
        proposed={displayed}
        baseLabel={baseLabel}
        proposedLabel={isEdited ? `${proposedLabel} (edited)` : proposedLabel}
      />
    </div>
  );
}
