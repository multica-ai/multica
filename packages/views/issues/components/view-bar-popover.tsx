"use client";

import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  EyeOff,
  GripVertical,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import type { IssueView } from "@multica/core/api/schemas";
import { useT } from "../../i18n";

/** One entry on the bar: a built-in tab or a saved view. */
export interface ViewBarItem {
  barItemId: string;
  label: string;
  kind: "builtin" | "view";
  view?: IssueView;
  canManage?: boolean;
}

/** The vertical-locked row drag has no DragOverlay under the pointer, so
 *  the grabbing cursor is promoted to the document for the drag's duration
 *  (see the `data-dnd-dragging` contract in ui/styles/base.css). */
function setDndCursor(on: boolean) {
  if (on) document.documentElement.dataset.dndDragging = "true";
  else delete document.documentElement.dataset.dndDragging;
}

/**
 * The one delete-view confirmation, shared by the manage dialog, the bar's
 * context menu, and the list-panel rows so every entrance carries identical
 * copy and the same "deletes the view only, never issues" contract.
 */
export function DeleteViewConfirm({
  view,
  onOpenChange,
  onConfirm,
}: {
  view: IssueView | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: (view: IssueView) => Promise<void>;
}) {
  const { t } = useT("issues");
  return (
    <AlertDialog open={!!view} onOpenChange={(v) => !v && onOpenChange(false)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.view_bar.delete_title)}</AlertDialogTitle>
          <AlertDialogDescription className="break-words">
            {t(($) => $.view_bar.delete_description, { name: view?.name ?? "" })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.save_view.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              const target = view;
              onOpenChange(false);
              if (!target) return;
              void onConfirm(target).then(
                () => toast.success(t(($) => $.view_bar.toast_deleted)),
                () => toast.error(t(($) => $.save_view.toast_failed)),
              );
            }}
          >
            {t(($) => $.view_bar.delete)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function SortablePanelRow({
  item,
  active,
  pinned,
  onSelect,
  onEdit,
  onDelete,
  onTogglePin,
  onHide,
}: {
  item: ViewBarItem;
  active: boolean;
  pinned: boolean;
  onSelect: () => void;
  onEdit?: () => void;
  onDelete?: () => void;
  onTogglePin?: () => void;
  onHide?: () => void;
}) {
  const { t } = useT("issues");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: item.barItemId });
  const isView = item.kind === "view";

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(
        "group/view-row flex items-center gap-1.5 rounded-md px-1.5 py-1",
        isDragging ? "z-10 bg-accent opacity-80" : "hover:bg-muted/60",
      )}
    >
      <button
        type="button"
        {...attributes}
        {...listeners}
        aria-label={t(($) => $.view_bar.drag_handle)}
        className={cn(
          "cursor-grab active:cursor-grabbing text-faint-foreground hover:text-muted-foreground",
          isDragging && "cursor-grabbing",
        )}
      >
        <GripVertical className="size-3.5" />
      </button>
      <button
        type="button"
        onClick={onSelect}
        className={cn(
          "flex min-w-0 flex-1 items-center gap-1.5 text-left text-body",
          active && "font-medium",
        )}
      >
        <span className="truncate">{item.label}</span>
        {item.kind === "builtin" && (
          <span className="shrink-0 text-caption text-muted-foreground">
            {t(($) => $.view_bar.builtin_tag)}
          </span>
        )}
      </button>
      {isView && item.view && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.view_bar.row_menu)}
                className="text-muted-foreground"
              >
                <MoreHorizontal className="size-3.5" />
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-44">
            {/* Same permission presentation as the tab context menu:
                edit greys out, delete hides. */}
            <DropdownMenuItem disabled={!item.canManage} onClick={onEdit}>
              <Pencil className="size-3.5" />
              {t(($) => $.view_bar.context_edit)}
            </DropdownMenuItem>
            {item.canManage && (
              <DropdownMenuItem variant="destructive" onClick={onDelete}>
                <Trash2 className="size-3.5" />
                {t(($) => $.view_bar.delete)}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={onTogglePin}>
              {pinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />}
              {pinned
                ? t(($) => $.view_bar.context_unpin)
                : t(($) => $.view_bar.context_pin)}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onHide}>
              <EyeOff className="size-3.5" />
              {t(($) => $.view_bar.row_hide)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}

/**
 * The pulled-down list: EVERY tab on the bar, in bar order, drag to
 * reorder — the same preference document the bar's inline drag writes.
 * Because the bar shows the longest fitting prefix, reordering here IS
 * the control over what stays visible: drag something up to put it on
 * the bar, down to tuck it away. A separator marks the current fold.
 */
export function ViewListPanel({
  items,
  fitCount,
  activeViewId,
  pinnedViewIds,
  onMoveItem,
  onSelectItem,
  onEditView,
  onDeleteView,
  onTogglePin,
  onHideItem,
}: {
  /** All bar candidates in order (hidden ones excluded, as on the bar). */
  items: ViewBarItem[];
  /** How many currently fit on the bar — the fold line. */
  fitCount: number;
  activeViewId: string | null;
  pinnedViewIds: ReadonlySet<string>;
  /** Reorder request: move the item with `activeId` next to `overId`. */
  onMoveItem: (activeId: string, overId: string) => void;
  onSelectItem: (item: ViewBarItem) => void;
  onEditView: (view: IssueView) => void;
  onDeleteView: (view: IssueView) => void;
  onTogglePin: (view: IssueView, pinned: boolean) => void;
  onHideItem: (barItemId: string) => void;
}) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
  );

  const handleDragEnd = (event: DragEndEvent) => {
    setDndCursor(false);
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    onMoveItem(String(active.id), String(over.id));
  };

  return (
    <div className="max-h-96 overflow-y-auto p-1">
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        modifiers={[restrictToVerticalAxis]}
        autoScroll={{ threshold: { x: 0, y: 0.15 } }}
        onDragStart={() => setDndCursor(true)}
        onDragCancel={() => setDndCursor(false)}
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={items.map((item) => item.barItemId)}
          strategy={verticalListSortingStrategy}
        >
          {items.map((item, index) => (
            <div key={item.barItemId}>
              {/* The fold: everything above is on the bar right now. */}
              {index === fitCount && index > 0 && (
                <div className="mx-1.5 my-1 border-t border-border" />
              )}
              <SortablePanelRow
                item={item}
                active={
                  item.kind === "view" && item.view
                    ? item.view.id === activeViewId
                    : false
                }
                pinned={!!item.view && pinnedViewIds.has(item.view.id)}
                onSelect={() => onSelectItem(item)}
                onEdit={item.view ? () => onEditView(item.view!) : undefined}
                onDelete={item.view ? () => onDeleteView(item.view!) : undefined}
                onTogglePin={
                  item.view
                    ? () => onTogglePin(item.view!, pinnedViewIds.has(item.view!.id))
                    : undefined
                }
                onHide={() => onHideItem(item.barItemId)}
              />
            </div>
          ))}
        </SortableContext>
      </DndContext>
    </div>
  );
}
