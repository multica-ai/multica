"use client";

import { useState } from "react";
import {
  Bold,
  Code,
  Eye,
  EyeOff,
  GripVertical,
  Highlighter,
  Italic,
  Link2,
  List,
  MessageSquarePlus,
  Pilcrow,
  Quote,
  RotateCcw,
  Strikethrough,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import {
  DEFAULT_EDITOR_TOOLBAR_ORDER,
  type EditorToolbarActionId,
  type EditorToolbarRow,
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
  comment: "Comment",
  italic: "Italic",
  strike: "Strikethrough",
  lists: "Lists",
  blockquote: "Quote",
  code: "Code",
};

// The same icons the row draws, so the list can be matched to the toolbar by
// eye instead of by reading every label.
const SETTING_ICONS: Record<
  EditorToolbarActionId,
  React.ComponentType<{ className?: string }>
> = {
  bold: Bold,
  link: Link2,
  heading: Pilcrow,
  highlight: Highlighter,
  comment: MessageSquarePlus,
  italic: Italic,
  strike: Strikethrough,
  lists: List,
  blockquote: Quote,
  code: Code,
};

const DRAG_MIME = "text/x-editor-toolbar-action";

function reorder(
  order: EditorToolbarActionId[],
  moved: EditorToolbarActionId,
  target: EditorToolbarActionId,
): EditorToolbarActionId[] {
  if (moved === target) return order;
  const without = order.filter((action) => action !== moved);
  const at = without.indexOf(target);
  if (at === -1) return order;
  return [...without.slice(0, at), moved, ...without.slice(at)];
}

export function EditorToolbarSettings() {
  const { order, hidden, saveRow, canSave } = useEditorToolbarOrder();
  const [saving, setSaving] = useState(false);
  const [draggingOver, setDraggingOver] = useState<EditorToolbarActionId | null>(
    null,
  );

  const persist = async (next: EditorToolbarRow) => {
    setSaving(true);
    try {
      await saveRow(next);
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

  const toggleHidden = (action: EditorToolbarActionId) => {
    const next = hidden.includes(action)
      ? hidden.filter((entry) => entry !== action)
      : [...hidden, action];
    void persist({ order, hidden: next });
  };

  const visible = order.filter((action) => !hidden.includes(action));

  return (
    <section className="space-y-3">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <h2 className="text-sm font-semibold">Formatting toolbar</h2>
          <p className="text-xs text-muted-foreground">
            Drag to choose the order of the controls shown above the editor in
            Notes and Documents, and hide the ones you never use. Hidden
            controls move into the ⋯ menu, not out of the app. Your setup
            follows you across devices.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!canSave || saving}
          onClick={() =>
            void persist({
              order: [...DEFAULT_EDITOR_TOOLBAR_ORDER],
              hidden: [],
            })
          }
        >
          <RotateCcw className="size-3.5" />
          Reset
        </Button>
      </div>

      {/* Live preview: the user's own row, so the effect of a drag is visible
          without leaving settings. */}
      <div
        data-testid="toolbar-settings-preview"
        className="flex min-h-11 items-center gap-1 rounded-md border bg-muted/20 px-2 py-1.5"
      >
        {visible.map((action) => {
          const Icon = SETTING_ICONS[action];
          return (
            <span
              key={action}
              title={EDITOR_TOOLBAR_ACTION_LABELS[action]}
              className="flex size-7 items-center justify-center rounded-md text-muted-foreground"
            >
              <Icon className="size-4" />
            </span>
          );
        })}
      </div>

      <ol className="divide-y rounded-md border bg-card">
        {order.map((action, index) => {
          const label = EDITOR_TOOLBAR_ACTION_LABELS[action];
          const Icon = SETTING_ICONS[action];
          const isHidden = hidden.includes(action);
          return (
            <li
              key={action}
              data-testid={`toolbar-setting-${action}`}
              draggable={canSave && !saving}
              onDragStart={(event) => {
                event.dataTransfer.setData(DRAG_MIME, action);
                event.dataTransfer.effectAllowed = "move";
              }}
              onDragOver={(event) => {
                event.preventDefault();
                event.dataTransfer.dropEffect = "move";
                setDraggingOver(action);
              }}
              onDragLeave={() => setDraggingOver(null)}
              onDrop={(event) => {
                event.preventDefault();
                setDraggingOver(null);
                const moved = event.dataTransfer.getData(
                  DRAG_MIME,
                ) as EditorToolbarActionId;
                if (!moved) return;
                const next = reorder(order, moved, action);
                // One save when the row lands, not one per step.
                if (next !== order) void persist({ order: next, hidden });
              }}
              className={cn(
                "flex items-center gap-3 px-3 py-2",
                draggingOver === action && "bg-accent/40",
                isHidden && "opacity-60",
              )}
            >
              <GripVertical
                aria-hidden
                className="size-4 shrink-0 cursor-grab text-muted-foreground"
              />
              <span className="w-6 text-right text-xs tabular-nums text-muted-foreground">
                {index + 1}
              </span>
              <Icon className="size-4 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 text-sm">{label}</span>
              {isHidden && (
                <span className="text-xs text-muted-foreground">
                  In the ⋯ menu
                </span>
              )}
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={`${isHidden ? "Show" : "Hide"} ${label}`}
                disabled={!canSave || saving}
                onClick={() => toggleHidden(action)}
              >
                {isHidden ? (
                  <EyeOff className="size-4" />
                ) : (
                  <Eye className="size-4" />
                )}
              </Button>
            </li>
          );
        })}
      </ol>
    </section>
  );
}
