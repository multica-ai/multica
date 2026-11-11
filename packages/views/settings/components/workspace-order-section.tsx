"use client";

import { useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { restrictToParentElement, restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Pin, PinOff } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import type { Workspace } from "@multica/core/types";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { useSetWorkspaceOrder, useSetWorkspacePinned } from "@multica/core/workspace/mutations";
import { sortWorkspaces } from "@multica/core/workspace/sort-workspaces";
import { toast } from "sonner";
import { useT } from "../../i18n";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { SettingsCard, SettingsSection } from "./settings-layout";

const EMPTY_WORKSPACES: Workspace[] = [];
const DRAG_MODIFIERS = [restrictToVerticalAxis, restrictToParentElement];

export function WorkspaceOrderSection() {
  const { t } = useT("settings");
  const { data = EMPTY_WORKSPACES, isPending, isError } = useQuery(workspaceListOptions());
  const workspaces = useMemo(() => sortWorkspaces(data), [data]);
  const order = useSetWorkspaceOrder();
  const pin = useSetWorkspacePinned();
  const saving = useRef(false);
  const busy = order.isPending || pin.isPending;

  const save = async (action: () => Promise<unknown>) => {
    if (saving.current) return;
    saving.current = true;
    try {
      await action();
    } catch {
      toast.error(t(($) => $.preferences.workspaces.save_failed));
    } finally {
      saving.current = false;
    }
  };

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id || busy) return;
    const from = workspaces.findIndex((ws) => ws.id === active.id);
    const to = workspaces.findIndex((ws) => ws.id === over.id);
    if (from < 0 || to < 0 || !!workspaces[from]!.pinned_at !== !!workspaces[to]!.pinned_at) return;
    void save(() => order.mutateAsync(arrayMove(workspaces, from, to).map((ws) => ws.id)));
  };

  const groups = [
    { label: t(($) => $.preferences.workspaces.pinned), rows: workspaces.filter((ws) => !!ws.pinned_at) },
    { label: t(($) => $.preferences.workspaces.other), rows: workspaces.filter((ws) => !ws.pinned_at) },
  ];

  return (
    <SettingsSection
      title={t(($) => $.preferences.workspaces.title)}
      description={t(($) => $.preferences.workspaces.hint)}
      scope="account"
      anchor="workspaces"
    >
      <SettingsCard>
        {isPending || isError ? (
          <p role="status" className="px-4 py-3 text-body text-muted-foreground">
            {isError ? t(($) => $.preferences.workspaces.load_failed) : t(($) => $.preferences.workspaces.loading)}
          </p>
        ) : groups.filter((group) => group.rows.length > 0).map((group) => (
          <WorkspaceGroup
            key={group.label}
            label={group.label}
            workspaces={group.rows}
            disabled={busy}
            onDragEnd={onDragEnd}
            onPin={(ws) => void save(() => pin.mutateAsync({ workspaceId: ws.id, pinned: !ws.pinned_at }))}
          />
        ))}
      </SettingsCard>
    </SettingsSection>
  );
}

function WorkspaceGroup({ label, workspaces, disabled, onDragEnd, onPin }: {
  label: string;
  workspaces: Workspace[];
  disabled: boolean;
  onDragEnd: (event: DragEndEvent) => void;
  onPin: (workspace: Workspace) => void;
}) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  return (
    <div className="p-2">
      <p className="px-2 py-1 text-caption font-medium text-muted-foreground">{label}</p>
      <DndContext sensors={sensors} collisionDetection={closestCenter} modifiers={DRAG_MODIFIERS} onDragEnd={onDragEnd}>
        <SortableContext items={workspaces.map((ws) => ws.id)} strategy={verticalListSortingStrategy}>
          <ul aria-label={label} aria-busy={disabled}>
            {workspaces.map((workspace) => (
              <WorkspaceRow key={workspace.id} workspace={workspace} disabled={disabled} onPin={() => onPin(workspace)} />
            ))}
          </ul>
        </SortableContext>
      </DndContext>
    </div>
  );
}

function WorkspaceRow({ workspace, disabled, onPin }: {
  workspace: Workspace;
  disabled: boolean;
  onPin: () => void;
}) {
  const { t } = useT("settings");
  const { t: layoutT } = useT("layout");
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: workspace.id,
    disabled,
  });
  const pinned = !!workspace.pinned_at;

  return (
    <li
      ref={setNodeRef}
      data-workspace-order-id={workspace.id}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(
        "flex items-center gap-2 rounded-md px-2 py-2 text-body",
        isDragging ? "relative z-10 bg-popover shadow-[var(--menu-shadow)]" : "hover:bg-accent/50",
      )}
    >
      <button
        ref={setActivatorNodeRef}
        type="button"
        {...attributes}
        {...listeners}
        disabled={disabled}
        aria-label={t(($) => $.preferences.workspaces.drag_handle, { name: workspace.name })}
        className="flex size-6 shrink-0 cursor-grab touch-none items-center justify-center rounded-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing disabled:opacity-50"
      >
        <GripVertical className="size-4" />
      </button>
      <WorkspaceAvatar name={workspace.name} avatarUrl={workspace.avatar_url} size="sm" />
      <span className="min-w-0 flex-1 truncate">{workspace.name}</span>
      <IconButton
        variant="ghost"
        size="icon-sm"
        label={pinned ? layoutT(($) => $.rail.unpin_workspace) : layoutT(($) => $.rail.pin_workspace)}
        disabled={disabled}
        onClick={onPin}
      >
        {pinned ? <PinOff className="size-4" /> : <Pin className="size-4" />}
      </IconButton>
    </li>
  );
}
