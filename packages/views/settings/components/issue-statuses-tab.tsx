"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Archive,
  ArchiveRestore,
  GripVertical,
  Lock,
  MoreHorizontal,
  Pencil,
  Plus,
} from "lucide-react";
import { toast } from "sonner";
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
  arrayMove,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { useFeatureEnabled } from "@multica/core/config";
import { memberListOptions } from "@multica/core/workspace/queries";
import { issueStatusListOptions } from "@multica/core/issue-statuses/queries";
import {
  useArchiveIssueStatus,
  useCreateIssueStatus,
  useReorderIssueStatuses,
  useUpdateIssueStatus,
} from "@multica/core/issue-statuses/mutations";
import { ALL_STATUSES } from "@multica/core/issues/config";
import type { IssueStatusCategory, IssueStatusEntry } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label as FieldLabel } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ColorPicker, COLOR_PICKER_PRESETS } from "../../common/color-picker";
import { StatusIcon } from "../../issues/components/status-icon";
import { useStatusLabel } from "../../issues/utils/status-label";
import { useT } from "../../i18n";
import { SettingsTab } from "./settings-layout";

/**
 * Workspace issue status catalog management (MUL-6243).
 *
 * The page is organised by CATEGORY rather than as one flat list, because a
 * category is not decoration here — it is the behavior a status inherits. A
 * status in `todo` starts the assigned agent; one in `in_review` finalizes an
 * autopilot run. Grouping is what makes that consequence visible at the moment
 * the admin picks a category, which is also the only moment they can: category
 * is immutable after creation, since changing it would silently rewrite the
 * machine semantics of every issue already on the status.
 *
 * Built-ins are shown but locked. Each one is its category's canonical
 * definition, and the default workspace has to look identical for every user
 * who never opens this page.
 */

interface StatusDraft {
  name: string;
  description: string;
  category: IssueStatusCategory;
  color: string;
}

const EMPTY_DRAFT: StatusDraft = {
  name: "",
  description: "",
  category: "todo",
  color: COLOR_PICKER_PRESETS[6]!,
};

export function IssueStatusesTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const canCreate = useFeatureEnabled("custom_issue_statuses");

  const [showArchived, setShowArchived] = useState(false);
  const [createCategory, setCreateCategory] = useState<IssueStatusCategory | null>(null);
  const [editing, setEditing] = useState<IssueStatusEntry | null>(null);
  const [pendingArchive, setPendingArchive] = useState<IssueStatusEntry | null>(null);

  const { data: statuses = [], isLoading } = useQuery(issueStatusListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentUser = useAuthStore((s) => s.user);
  const myRole = useMemo(() => {
    if (!currentUser) return null;
    return members.find((m) => m.user_id === currentUser.id)?.role ?? null;
  }, [members, currentUser]);
  const isAdmin = myRole === "owner" || myRole === "admin";

  const groups = useMemo(
    () =>
      ALL_STATUSES.map((category) => {
        const inCategory = statuses.filter((s) => s.category === category);
        return {
          category,
          builtIn: inCategory.find((s) => s.is_system),
          // Archived rows are hidden behind a toggle rather than dropped: an
          // admin needs to see what a lingering status on an old issue is.
          custom: inCategory.filter(
            (s) => !s.is_system && (showArchived || !s.archived_at),
          ),
        };
      }),
    [statuses, showArchived],
  );

  const archivedCount = statuses.filter((s) => !s.is_system && s.archived_at).length;

  return (
    <SettingsTab
      title={t(($) => $.issue_statuses.title)}
      description={t(($) => $.issue_statuses.description)}
    >
      <div className="space-y-5">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <label className="flex items-center gap-2 text-body text-muted-foreground">
            <Switch
              checked={showArchived}
              onCheckedChange={setShowArchived}
              disabled={archivedCount === 0}
            />
            {t(($) => $.issue_statuses.show_archived, { count: archivedCount })}
          </label>
        </div>

        {!canCreate && (
          <p className="rounded-lg border border-surface-border bg-muted/20 px-4 py-3 text-caption text-muted-foreground">
            {t(($) => $.issue_statuses.flag_off)}
          </p>
        )}

        {isLoading ? (
          <div className="rounded-lg border border-surface-border bg-card px-4 py-12 text-center text-body text-muted-foreground">
            {t(($) => $.issue_statuses.loading)}
          </div>
        ) : (
          <div className="space-y-4">
            {groups.map((group) => (
              <CategorySection
                key={group.category}
                category={group.category}
                builtIn={group.builtIn}
                custom={group.custom}
                canManage={isAdmin}
                canCreate={isAdmin && canCreate}
                onCreate={() => setCreateCategory(group.category)}
                onEdit={setEditing}
                onArchive={setPendingArchive}
              />
            ))}
          </div>
        )}
      </div>

      <StatusEditorDialog
        open={createCategory !== null}
        onOpenChange={(open) => !open && setCreateCategory(null)}
        category={createCategory}
      />
      <StatusEditorDialog
        open={Boolean(editing)}
        onOpenChange={(open) => !open && setEditing(null)}
        category={editing?.category ?? null}
        status={editing}
      />
      <ArchiveStatusDialog status={pendingArchive} onClose={() => setPendingArchive(null)} />
    </SettingsTab>
  );
}

function CategorySection({
  category,
  builtIn,
  custom,
  canManage,
  canCreate,
  onCreate,
  onEdit,
  onArchive,
}: {
  category: IssueStatusCategory;
  builtIn: IssueStatusEntry | undefined;
  custom: IssueStatusEntry[];
  canManage: boolean;
  canCreate: boolean;
  onCreate: () => void;
  onEdit: (status: IssueStatusEntry) => void;
  onArchive: (status: IssueStatusEntry) => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const labelOf = useStatusLabel(wsId);
  const reorder = useReorderIssueStatuses();

  // Local order so the drag reads as instant even before the optimistic cache
  // write settles; resynced whenever the server list changes.
  const [order, setOrder] = useState(custom);
  useEffect(() => setOrder(custom), [custom]);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
  );

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const from = order.findIndex((s) => s.id === active.id);
    const to = order.findIndex((s) => s.id === over.id);
    if (from < 0 || to < 0) return;
    const next = arrayMove(order, from, to);
    setOrder(next);
    // ACTIVE rows only. With "show archived" on, `order` also holds archived
    // rows; sending those made the server reject the request, and before the
    // write became atomic that rejection landed AFTER the active rows had
    // already been reordered. Archived rows are frozen, so their absence from
    // the payload is also what the user sees.
    reorder.mutate(
      { category, ordered: next.filter((entry) => !entry.archived_at) },
      {
        onError: (error) => {
          setOrder(custom);
          toast.error(
            error instanceof Error ? error.message : t(($) => $.issue_statuses.reorder_failed),
          );
        },
      },
    );
  };

  // Only rows that can actually move are draggable. A single custom status has
  // nothing to swap with, and archived rows are frozen.
  const sortableIds = order.filter((s) => !s.archived_at).map((s) => s.id);
  const canReorder = canManage && sortableIds.length > 1;

  return (
    <div className="overflow-hidden rounded-lg border border-surface-border bg-card">
      <div className="flex items-center gap-2 border-b border-surface-border bg-muted/20 px-4 py-2.5">
        <StatusIcon status={category} category={category} className="size-3.5" />
        <span className="text-caption font-medium">{labelOf(category)}</span>
        <span className="flex-1 text-caption text-muted-foreground">
          {t(($) => $.issue_statuses.categories[category])}
        </span>
        {canCreate && (
          <Button variant="ghost" size="sm" className="gap-1.5" onClick={onCreate}>
            <Plus className="size-3.5" />
            {t(($) => $.issue_statuses.add)}
          </Button>
        )}
      </div>

      <div className="divide-y divide-surface-border">
        {builtIn && <BuiltInRow entry={builtIn} label={labelOf(builtIn.key)} />}
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
          <SortableContext items={sortableIds} strategy={verticalListSortingStrategy}>
            {order.map((entry) => (
              <CustomStatusRow
                key={entry.id}
                entry={entry}
                canManage={canManage}
                canReorder={canReorder && !entry.archived_at}
                onEdit={() => onEdit(entry)}
                onArchive={() => onArchive(entry)}
              />
            ))}
          </SortableContext>
        </DndContext>
      </div>
    </div>
  );
}

function BuiltInRow({ entry, label }: { entry: IssueStatusEntry; label: string }) {
  const { t } = useT("settings");
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <span className="w-4 shrink-0" />
      <StatusIcon status={entry.key} category={entry.category} className="size-3.5" />
      <span className="min-w-0 truncate text-body font-medium">{label}</span>
      <Tooltip>
        <TooltipTrigger
          render={
            <span className="inline-flex items-center text-muted-foreground">
              <Lock className="size-3.5" />
            </span>
          }
        />
        <TooltipContent>{t(($) => $.issue_statuses.built_in_locked)}</TooltipContent>
      </Tooltip>
      <span className="ml-auto truncate text-caption text-muted-foreground">
        {entry.description || "—"}
      </span>
    </div>
  );
}

function CustomStatusRow({
  entry,
  canManage,
  canReorder,
  onEdit,
  onArchive,
}: {
  entry: IssueStatusEntry;
  canManage: boolean;
  canReorder: boolean;
  onEdit: () => void;
  onArchive: () => void;
}) {
  const { t } = useT("settings");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: entry.id,
    disabled: !canReorder,
  });

  const archived = Boolean(entry.archived_at);

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={`flex items-center gap-3 bg-card px-4 py-3 ${isDragging ? "relative z-10 shadow-[var(--surface-shadow)]" : ""} ${archived ? "opacity-60" : ""}`}
    >
      {canReorder ? (
        <button
          type="button"
          aria-label={t(($) => $.issue_statuses.actions.reorder, { name: entry.name })}
          className="w-4 shrink-0 cursor-grab text-muted-foreground active:cursor-grabbing"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-4" />
        </button>
      ) : (
        <span className="w-4 shrink-0" />
      )}
      <StatusIcon
        status={entry.key}
        category={entry.category}
        color={entry.color}
        className="size-3.5"
      />
      <span className="min-w-0 truncate text-body font-medium">{entry.name}</span>
      <code className="shrink-0 rounded bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground">
        {entry.key}
      </code>
      {archived && (
        <span className="shrink-0 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground">
          {t(($) => $.issue_statuses.archived_badge)}
        </span>
      )}
      <span className="ml-auto min-w-0 flex-1 truncate text-right text-caption text-muted-foreground">
        {entry.description || "—"}
      </span>
      {canManage && !archived && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.issue_statuses.actions.open, { name: entry.name })}
              >
                <MoreHorizontal className="size-4" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onEdit}>
              <Pencil className="size-4" />
              {t(($) => $.issue_statuses.actions.edit)}
            </DropdownMenuItem>
            <DropdownMenuItem variant="destructive" onClick={onArchive}>
              <Archive className="size-4" />
              {t(($) => $.issue_statuses.actions.archive)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      {archived && (
        <Tooltip>
          <TooltipTrigger
            render={
              <span className="inline-flex size-8 items-center justify-center text-muted-foreground">
                <ArchiveRestore className="size-4" />
              </span>
            }
          />
          <TooltipContent>{t(($) => $.issue_statuses.archived_hint)}</TooltipContent>
        </Tooltip>
      )}
    </div>
  );
}

function StatusEditorDialog({
  open,
  onOpenChange,
  category,
  status,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  category: IssueStatusCategory | null;
  status?: IssueStatusEntry | null;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const labelOf = useStatusLabel(wsId);
  const create = useCreateIssueStatus();
  const update = useUpdateIssueStatus();
  const [draft, setDraft] = useState<StatusDraft>(EMPTY_DRAFT);

  const categoryItems = ALL_STATUSES.map((c) => ({
    value: c,
    label: (
      <span className="flex items-center gap-2">
        <StatusIcon status={c} category={c} className="size-3.5" />
        {labelOf(c)}
      </span>
    ),
  }));

  useEffect(() => {
    if (!open) return;
    setDraft(
      status
        ? {
            name: status.name,
            description: status.description ?? "",
            category: status.category,
            color: status.color,
          }
        : { ...EMPTY_DRAFT, category: category ?? "todo" },
    );
  }, [status, category, open]);

  const submit = () => {
    const name = draft.name.trim();
    if (!name) return;
    const onError = (error: unknown) =>
      toast.error(
        error instanceof Error ? error.message : t(($) => $.issue_statuses.editor.save_failed),
      );

    if (status) {
      update.mutate(
        {
          id: status.id,
          name,
          description: draft.description.trim(),
          color: draft.color,
        },
        { onSuccess: () => onOpenChange(false), onError },
      );
      return;
    }
    create.mutate(
      {
        name,
        description: draft.description.trim(),
        category: draft.category,
        color: draft.color,
      },
      { onSuccess: () => onOpenChange(false), onError },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {status
              ? t(($) => $.issue_statuses.editor.edit_title)
              : t(($) => $.issue_statuses.editor.create_title)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.issue_statuses.editor.behavior_hint, {
              category: labelOf(draft.category),
            })}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-5 py-2">
          <div className="space-y-2">
            <FieldLabel htmlFor="status-name">
              {t(($) => $.issue_statuses.editor.name)}
            </FieldLabel>
            <Input
              id="status-name"
              autoFocus
              maxLength={64}
              value={draft.name}
              onChange={(event) =>
                setDraft((current) => ({ ...current, name: event.target.value }))
              }
              placeholder={t(($) => $.issue_statuses.editor.name_placeholder)}
            />
          </div>
          <div className="space-y-2">
            <FieldLabel>{t(($) => $.issue_statuses.editor.category)}</FieldLabel>
            {/* Immutable after creation: changing it would silently rewrite the
                platform behavior of every issue already on this status. */}
            <Select
              items={categoryItems}
              value={draft.category}
              onValueChange={(value) =>
                value &&
                setDraft((current) => ({ ...current, category: value as IssueStatusCategory }))
              }
              disabled={Boolean(status)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {categoryItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-caption text-muted-foreground">
              {status
                ? t(($) => $.issue_statuses.editor.category_locked)
                : t(($) => $.issue_statuses.categories[draft.category])}
            </p>
          </div>
          <div className="space-y-2">
            <FieldLabel htmlFor="status-description">
              {t(($) => $.issue_statuses.editor.description)}
            </FieldLabel>
            <Textarea
              id="status-description"
              rows={3}
              maxLength={256}
              value={draft.description}
              onChange={(event) =>
                setDraft((current) => ({ ...current, description: event.target.value }))
              }
              placeholder={t(($) => $.issue_statuses.editor.description_placeholder)}
            />
          </div>
          <div className="space-y-2">
            <FieldLabel>{t(($) => $.issue_statuses.editor.color)}</FieldLabel>
            <ColorPicker
              value={draft.color}
              onChange={(color) => setDraft((current) => ({ ...current, color }))}
              trigger={
                <button
                  type="button"
                  aria-label={t(($) => $.issue_statuses.editor.color)}
                  className="flex h-9 items-center gap-2.5 rounded-md border border-surface-border px-2.5 transition-colors hover:bg-surface-hover"
                >
                  <span className="size-5 rounded-full" style={{ backgroundColor: draft.color }} />
                  <span className="font-mono text-caption uppercase text-muted-foreground">
                    {draft.color}
                  </span>
                </button>
              }
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.issue_statuses.editor.cancel)}
          </Button>
          <Button
            onClick={submit}
            disabled={!draft.name.trim() || create.isPending || update.isPending}
          >
            {create.isPending || update.isPending
              ? t(($) => $.issue_statuses.editor.saving)
              : t(($) => $.issue_statuses.editor.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ArchiveStatusDialog({
  status,
  onClose,
}: {
  status: IssueStatusEntry | null;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const archive = useArchiveIssueStatus();
  return (
    <AlertDialog open={Boolean(status)} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.issue_statuses.archive_dialog.title)}</AlertDialogTitle>
          {/* Archiving retires a status from FUTURE assignment. Issues already
              on it keep it and keep behaving as their category prescribes —
              say so, or this reads like a delete. */}
          <AlertDialogDescription>
            {t(($) => $.issue_statuses.archive_dialog.description, {
              name: status?.name ?? "",
            })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>
            {t(($) => $.issue_statuses.archive_dialog.cancel)}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              if (!status) return;
              archive.mutate(status.id, {
                onSuccess: onClose,
                onError: (error) =>
                  toast.error(
                    error instanceof Error
                      ? error.message
                      : t(($) => $.issue_statuses.archive_dialog.failed),
                  ),
              });
            }}
          >
            {t(($) => $.issue_statuses.archive_dialog.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
