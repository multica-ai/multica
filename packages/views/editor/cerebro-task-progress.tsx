"use client";

/* eslint-disable i18next/no-literal-string --
   Cerebro fork component: its labels are English by workspace policy (app UI is
   English). The editor i18n catalog is upstream zone and owned by track A. */

/**
 * CEREBRO-PATCH(list-editing): FIR-4707 Phase 6 slice 13 — the task-list summary
 * shown above an editor that contains checkboxes: "N of M done", a progress bar,
 * and a "Hide N completed" toggle. Hiding is purely visual — it toggles the
 * `cerebro-hide-completed` class the CSS uses to collapse checked items — so no
 * content is ever removed. A parent item is never checked automatically (that is
 * the task-item node's own behaviour; this component only reports the counts).
 *
 * `countTaskItems` is the pure count behind the bar, unit-tested; the rendering
 * is verified by track C. Mounted by content-editor.tsx, gated on
 * isListEditingEnabled() (cerebro_editor_toolbar, default off).
 */
import { useEffect, useState } from "react";
import type { Editor } from "@tiptap/react";
import { useEditorState } from "@tiptap/react";
import type { Node as PMNode } from "@tiptap/pm/model";

const HIDE_COMPLETED_CLASS = "cerebro-hide-completed";

/** Count every task item in the document and how many are checked. */
export function countTaskItems(doc: PMNode): { total: number; done: number } {
  let total = 0;
  let done = 0;
  doc.descendants((node) => {
    if (node.type.name === "taskItem") {
      total += 1;
      if (node.attrs.checked) done += 1;
    }
    return true;
  });
  return { total, done };
}

export function CerebroTaskProgress({ editor }: { editor: Editor }) {
  const { total, done } = useEditorState({
    editor,
    selector: ({ editor: e }) => countTaskItems(e.state.doc),
  });
  const [hideCompleted, setHideCompleted] = useState(false);

  useEffect(() => {
    const dom = editor.view.dom;
    dom.classList.toggle(HIDE_COMPLETED_CLASS, hideCompleted);
    return () => dom.classList.remove(HIDE_COMPLETED_CLASS);
  }, [editor, hideCompleted]);

  // Nothing to summarise until the document actually has checkboxes.
  if (total === 0) return null;

  const pct = Math.round((done / total) * 100);

  return (
    <div className="cerebro-task-progress flex items-center gap-3 py-1 text-xs text-muted-foreground">
      <span className="tabular-nums">
        {done} of {total} done
      </span>
      <div
        className="cerebro-task-progress__bar h-1.5 flex-1 overflow-hidden rounded-full bg-muted"
        role="progressbar"
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <div
          className="h-full rounded-full bg-brand transition-[width]"
          style={{ width: `${pct}%` }}
        />
      </div>
      {done > 0 && (
        <button
          type="button"
          className="rounded px-1 hover:text-foreground"
          aria-pressed={hideCompleted}
          onClick={() => setHideCompleted((v) => !v)}
        >
          {hideCompleted ? "Show completed" : `Hide ${done} completed`}
        </button>
      )}
    </div>
  );
}
