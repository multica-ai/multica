"use client";

import { useState } from "react";
import { ChevronDown, ChevronUp, RotateCcw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import {
  DEFAULT_EDITOR_TOOLBAR_ORDER,
  type EditorToolbarActionId,
} from "./editor-toolbar-preferences";
import { useEditorToolbarOrder } from "./use-editor-toolbar-order";

export const EDITOR_TOOLBAR_ACTION_LABELS: Record<
  EditorToolbarActionId,
  string
> = {
  bold: "Bold",
  link: "Link",
  heading: "Text style",
  highlight: "Highlight",
  taskList: "Task list",
  comment: "Comment",
  italic: "Italic",
  strike: "Strikethrough",
  bulletList: "Bullet list",
  orderedList: "Ordered list",
  blockquote: "Quote",
  code: "Code",
  indent: "Increase indent",
  outdent: "Decrease indent",
};

export function EditorToolbarSettings() {
  const { order, saveOrder, canSave } = useEditorToolbarOrder();
  const [saving, setSaving] = useState(false);

  const persist = async (nextOrder: EditorToolbarActionId[]) => {
    setSaving(true);
    try {
      await saveOrder(nextOrder);
      toast.success("Formatting toolbar updated");
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update formatting toolbar",
      );
    } finally {
      setSaving(false);
    }
  };

  const move = (index: number, offset: -1 | 1) => {
    const target = index + offset;
    if (target < 0 || target >= order.length) return;
    const next = [...order];
    const currentAction = next[index]!;
    const targetAction = next[target]!;
    next[index] = targetAction;
    next[target] = currentAction;
    void persist(next);
  };

  return (
    <section className="space-y-3">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <h2 className="text-sm font-semibold">Formatting toolbar</h2>
          <p className="text-xs text-muted-foreground">
            Choose the order of the controls shown above the editor in Notes and
            Documents. Your order follows you across devices.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!canSave || saving}
          onClick={() => void persist([...DEFAULT_EDITOR_TOOLBAR_ORDER])}
        >
          <RotateCcw className="size-3.5" />
          Reset
        </Button>
      </div>

      <ol className="divide-y rounded-md border bg-card">
        {order.map((action, index) => {
          const label = EDITOR_TOOLBAR_ACTION_LABELS[action];
          return (
            <li
              key={action}
              data-testid={`toolbar-setting-${action}`}
              className="flex items-center gap-3 px-3 py-2"
            >
              <span className="w-6 text-right text-xs tabular-nums text-muted-foreground">
                {index + 1}
              </span>
              <span className="min-w-0 flex-1 text-sm">{label}</span>
              <div className="flex items-center gap-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Move ${label} up`}
                  disabled={!canSave || saving || index === 0}
                  onClick={() => move(index, -1)}
                >
                  <ChevronUp className="size-4" />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Move ${label} down`}
                  disabled={!canSave || saving || index === order.length - 1}
                  onClick={() => move(index, 1)}
                >
                  <ChevronDown className="size-4" />
                </Button>
              </div>
            </li>
          );
        })}
      </ol>
    </section>
  );
}
