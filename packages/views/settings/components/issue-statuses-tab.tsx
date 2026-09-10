"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Archive,
  ArrowDown,
  ArrowUp,
  GripVertical,
  MoreHorizontal,
  Pencil,
  Plus,
} from "lucide-react";
import { toast } from "sonner";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  issueStatusColor,
  compareIssueStatusEntries,
  issueStatusListOptions,
  normalizeIssueStatusCategory,
} from "@multica/core/issue-statuses/queries";
import {
  useArchiveIssueStatus,
  useCreateIssueStatus,
  useReorderIssueStatuses,
  useUpdateIssueStatus,
} from "@multica/core/issue-statuses/mutations";
import { ALL_STATUSES } from "@multica/core/issues/config";
import type {
  BuiltInIssueStatus,
  IssueStatusCategory,
  IssueStatusEntry,
  IssueStatusIcon,
} from "@multica/core/types";
import { ISSUE_STATUS_ICONS } from "@multica/core/types/issue-status";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label as FieldLabel } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Dialog,
  DialogContent,
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
  DropdownMenuSeparator,
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
 * category is the stable lifecycle group users scan. Concrete built-ins keep
 * their distinct automation behavior inside those groups: In Progress, In
 * Review, and Blocked all appear under Started without becoming one status.
 *
 * Every active status can be ordered within its category. Built-in definitions
 * remain locked; moving their display position does not change automation.
 *
 * The chrome is deliberately thin (MUL-6422): one bordered workflow list,
 * muted group headers, and row-level actions revealed only where available.
 */

interface StatusDraft {
  name: string;
  description: string;
  category: IssueStatusCategory;
  color: string;
  icon: IssueStatusIcon | "";
}

const EMPTY_DRAFT: StatusDraft = {
  name: "",
  description: "",
  category: "unstarted",
  color: COLOR_PICKER_PRESETS[6]!,
  icon: "",
};

export function IssueStatusesTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();

  const [showArchived, setShowArchived] = useState(false);
  const [createCategory, setCreateCategory] = useState<IssueStatusCategory | null>(null);
  const [editing, setEditing] = useState<IssueStatusEntry | null>(null);
  const [pendingArchive, setPendingArchive] = useState<IssueStatusEntry | null>(null);
  const [showBuiltInNotice, setShowBuiltInNotice] = useState(false);

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
        const inCategory = statuses.filter(
          (status) => normalizeIssueStatusCategory(status.category) === category,
        );
        return {
          category,
          // Archived rows are hidden behind a toggle rather than dropped: an
          // admin needs to see what a lingering status on an old issue is.
          entries: inCategory.filter((s) => showArchived || !s.archived_at)
            .sort(compareIssueStatusEntries),
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
      <div className="space-y-4">
        {isAdmin && (
          <p className="text-caption text-muted-foreground">{t(($) => $.issue_statuses.reorder_hint)}</p>
        )}
        {/* Offered only once the workspace has something archived. A permanently
            disabled "Show archived (0)" is a control that can never do
            anything. */}
        {archivedCount > 0 && (
          <label className="flex items-center justify-end gap-2 text-caption text-muted-foreground">
            {t(($) => $.issue_statuses.show_archived, { count: archivedCount })}
            <Switch checked={showArchived} onCheckedChange={setShowArchived} />
          </label>
        )}

        {isLoading ? (
          <div className="rounded-lg border border-surface-border bg-card px-4 py-12 text-center text-body text-muted-foreground">
            {t(($) => $.issue_statuses.loading)}
          </div>
        ) : (
          // The four categories are sections of a single
          // workflow, and separate borders made them read as unrelated
          // settings.
          <div className="overflow-hidden rounded-lg border border-surface-border bg-card">
            {groups.map((group) => (
              <CategorySection
                key={`${wsId}:${group.category}`}
                category={group.category}
                entries={group.entries}
                canManage={isAdmin}
                onCreate={() => setCreateCategory(group.category)}
                onEdit={(entry) => entry.is_system ? setShowBuiltInNotice(true) : setEditing(entry)}
                onArchive={(entry) => entry.is_system ? setShowBuiltInNotice(true) : setPendingArchive(entry)}
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
        category={
          editing ? normalizeIssueStatusCategory(editing.category) : null
        }
        status={editing}
      />
      <ArchiveStatusDialog status={pendingArchive} onClose={() => setPendingArchive(null)} />
      <AlertDialog open={showBuiltInNotice} onOpenChange={setShowBuiltInNotice}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.issue_statuses.built_in_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.issue_statuses.built_in_dialog.description)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogAction>{t(($) => $.issue_statuses.built_in_dialog.confirm)}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}

function CategorySection({
  category,
  entries,
  canManage,
  onCreate,
  onEdit,
  onArchive,
}: {
  category: IssueStatusCategory;
  entries: IssueStatusEntry[];
  canManage: boolean;
  onCreate: () => void;
  onEdit: (status: IssueStatusEntry) => void;
  onArchive: (status: IssueStatusEntry) => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const labelOf = useStatusLabel(wsId);
  const reorder = useReorderIssueStatuses();
  const saving = useRef(false);

  // Local order so the drag reads as instant even before the optimistic cache
  // write settles; resynced whenever the server list changes.
  const [order, setOrder] = useState(entries);
  useEffect(() => setOrder(entries), [entries]);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const move = (from: number, to: number) => {
    if (!canManage || saving.current || from < 0 || to < 0 || from === to ||
      !order[from] || !order[to] || order[from].archived_at || order[to].archived_at) return;
    const next = arrayMove(order, from, to);
    saving.current = true;
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
          setOrder(entries);
          toast.error(
            error instanceof Error ? error.message : t(($) => $.issue_statuses.reorder_failed),
          );
        },
        onSettled: () => { saving.current = false; },
      },
    );
  };
  const handleDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over) return;
    move(order.findIndex((s) => s.id === active.id), order.findIndex((s) => s.id === over.id));
  };

  // Only rows that can actually move are draggable. A single active status has
  // nothing to swap with, and archived rows are frozen.
  const sortableIds = order.filter((s) => !s.archived_at).map((s) => s.id);
  const canReorder = canManage && sortableIds.length > 1;

  return (
    <section
      aria-labelledby={`issue-status-category-${category}`}
      aria-busy={reorder.isPending}
      className="border-b border-surface-border last:border-b-0"
    >
      {/* Label plus the one action the header owns. The category glyph is the
          same glyph the built-in row renders directly below it, so it said
          nothing the eye had not already read. */}
      <div className="flex items-center justify-between gap-2 bg-muted/20 px-4 py-1.5">
        <h3
          id={`issue-status-category-${category}`}
          className="text-caption font-medium text-muted-foreground"
        >
          {t(($) => $.issue_statuses.category_labels[category])}
        </h3>
        {canManage && (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`${t(($) => $.issue_statuses.add)}: ${t(($) => $.issue_statuses.category_labels[category])}`}
                  onClick={onCreate}
                >
                  <Plus className="size-4" />
                </Button>
              }
            />
            <TooltipContent>{t(($) => $.issue_statuses.add)}</TooltipContent>
          </Tooltip>
        )}
      </div>

      <div className="divide-y divide-surface-border">
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
          <SortableContext items={sortableIds} strategy={verticalListSortingStrategy}>
            {order.map((entry, index) => {
              const activeIndex = sortableIds.indexOf(entry.id);
              const canMove = canReorder && !reorder.isPending && activeIndex >= 0;
              return (
                <StatusRow
                  key={entry.id}
                  entry={entry}
                  label={entry.is_system ? labelOf(entry.key) : entry.name}
                  description={entry.is_system
                    ? t(($) => $.issue_statuses.built_in_descriptions[entry.key as BuiltInIssueStatus])
                    : entry.description}
                  canManage={canManage}
                  canReorder={canReorder && !entry.archived_at}
                  isReordering={reorder.isPending}
                  onEdit={() => onEdit(entry)}
                  onArchive={() => onArchive(entry)}
                  onMoveUp={canMove && activeIndex > 0
                    ? () => move(index, order.findIndex((s) => s.id === sortableIds[activeIndex - 1]))
                    : undefined}
                  onMoveDown={canMove && activeIndex < sortableIds.length - 1
                    ? () => move(index, order.findIndex((s) => s.id === sortableIds[activeIndex + 1]))
                    : undefined}
                />
              );
            })}
          </SortableContext>
        </DndContext>
      </div>
    </section>
  );
}

function StatusRow({
  entry,
  label,
  description,
  canManage,
  canReorder,
  isReordering,
  onEdit,
  onArchive,
  onMoveUp,
  onMoveDown,
}: {
  entry: IssueStatusEntry;
  label: string;
  description: string;
  canManage: boolean;
  canReorder: boolean;
  isReordering: boolean;
  onEdit: () => void;
  onArchive: () => void;
  onMoveUp?: () => void;
  onMoveDown?: () => void;
}) {
  const { t } = useT("settings");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: entry.id,
    disabled: !canReorder || isReordering,
  });

  const archived = Boolean(entry.archived_at);

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={`group/row relative flex min-h-12 items-center gap-2 bg-card px-2 py-2 motion-reduce:transition-none! ${isDragging ? "z-10 shadow-[var(--surface-shadow)]" : ""} ${archived ? "opacity-60" : ""}`}
    >
      {canReorder ? (
        <button
          type="button"
          disabled={isReordering}
          aria-label={t(($) => $.issue_statuses.actions.reorder, { name: label })}
          className="flex size-6 shrink-0 touch-none items-center justify-center rounded cursor-grab text-muted-foreground focus-visible:outline-2 focus-visible:outline-ring active:cursor-grabbing [@media(pointer:coarse)]:size-11"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-4" />
        </button>
      ) : <span className="size-6 shrink-0 [@media(pointer:coarse)]:size-11" />}
      <StatusIcon
        status={entry.key}
        category={normalizeIssueStatusCategory(entry.category) ?? "unstarted"}
        color={issueStatusColor(entry)}
        icon={entry.icon}
        className="size-4"
      />
      {/* Name over description, the way the row is read. The old layout pinned
          the description to the far right, which left a column of em dashes on
          every status nobody had described. */}
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-body font-medium">{label}</span>
          {archived && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="shrink-0 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground">
                    {t(($) => $.issue_statuses.archived_badge)}
                  </span>
                }
              />
              <TooltipContent>{t(($) => $.issue_statuses.archived_hint)}</TooltipContent>
            </Tooltip>
          )}
        </div>
        {description && (
          <p className="truncate text-caption text-muted-foreground">{description}</p>
        )}
      </div>
      {canManage && !archived && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.issue_statuses.actions.open, { name: label })}
                className="shrink-0 group-hover/row:opacity-100 group-focus-within/row:opacity-100 data-popup-open:opacity-100 [@media(hover:hover)]:opacity-0 [@media(pointer:coarse)]:size-11 [@media(pointer:coarse)]:opacity-100"
              >
                <MoreHorizontal className="size-4" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem disabled={!onMoveUp} onClick={onMoveUp}>
              <ArrowUp className="size-4" />
              {t(($) => $.issue_statuses.actions.move_up)}
            </DropdownMenuItem>
            <DropdownMenuItem disabled={!onMoveDown} onClick={onMoveDown}>
              <ArrowDown className="size-4" />
              {t(($) => $.issue_statuses.actions.move_down)}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
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
  const create = useCreateIssueStatus();
  const update = useUpdateIssueStatus();
  const [draft, setDraft] = useState<StatusDraft>(EMPTY_DRAFT);
  const [iconChanged, setIconChanged] = useState(false);

  const categoryItems = ALL_STATUSES.map((c) => ({
    value: c,
    label: t(($) => $.issue_statuses.category_labels[c]),
  }));

  useEffect(() => {
    if (!open) return;
    setIconChanged(false);
    setDraft(
      status
        ? {
            name: status.name,
            description: status.description ?? "",
            category: normalizeIssueStatusCategory(status.category) ?? "unstarted",
            color: status.color,
            icon: ISSUE_STATUS_ICONS.includes(status.icon as IssueStatusIcon)
              ? status.icon as IssueStatusIcon : "",
          }
        : { ...EMPTY_DRAFT, category: category ?? "unstarted" },
    );
  }, [status, category, open]);

  const submit = () => {
    const name = draft.name.trim();
    if (!name || create.isPending || update.isPending) return;
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
          // Preserve an unknown future icon when only other fields are edited.
          ...(iconChanged ? { icon: draft.icon } : {}),
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
        icon: draft.icon,
      },
      {
        onSuccess: (created) => {
          onOpenChange(false);
          // The key is derived server-side and is the only handle the API and
          // the CLI accept, so creation has to say what it minted. A name with
          // no ASCII to slug gets one that cannot be guessed back from the name
          // — "客户确认" becomes `started_2` (MUL-6749) — so staying silent
          // would leave the admin no way to learn it short of reopening the
          // row. The dialog is already closing; a toast is the one surface
          // still visible.
          if (created?.key) {
            toast.success(t(($) => $.issue_statuses.editor.created, { key: created.key }));
          }
        },
        onError,
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            submit();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {status
                ? t(($) => $.issue_statuses.editor.edit_title)
                : t(($) => $.issue_statuses.editor.create_title)}
            </DialogTitle>
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
              {/* The key is the string the API and the CLI take, and renaming a
                  status does not move it — so it has to be readable somewhere.
                  Here, not as a chip on every row: the list is for scanning
                  names, and a slug beside each one is what turned it into a
                  table of internals. (MUL-6422) */}
              {status && (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.issue_statuses.editor.key_hint, { key: status.key })}
                </p>
              )}
            </div>
            <div className="space-y-2">
              <FieldLabel htmlFor="status-category">{t(($) => $.issue_statuses.editor.category)}</FieldLabel>
              {/* Immutable after creation: changing it would alter the
                  lifecycle of every issue already on this status. */}
              <Select
                items={categoryItems}
                value={draft.category}
                onValueChange={(value) =>
                  value &&
                  setDraft((current) => ({ ...current, category: value as IssueStatusCategory }))
                }
                disabled={Boolean(status)}
              >
                <SelectTrigger id="status-category">
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
                onKeyDown={(event) => {
                  if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
                    event.currentTarget.form?.requestSubmit();
                  }
                }}
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
                    <StatusIcon
                      status={status?.key ?? ""}
                      category={draft.category}
                      color={draft.color}
                      icon={draft.icon}
                      className="size-5"
                    />
                    <span className="font-mono text-caption uppercase text-muted-foreground">
                      {draft.color}
                    </span>
                  </button>
                }
              />
            </div>
            <fieldset className="space-y-2">
              <legend className="text-caption font-medium">
                {t(($) => $.issue_statuses.editor.icon)}
              </legend>
              <div className="flex flex-wrap gap-2">
                {(["", ...ISSUE_STATUS_ICONS] as const).map((icon) => (
                  <Button
                    key={icon}
                    type="button"
                    variant={draft.icon === icon ? "secondary" : "outline"}
                    size="icon"
                    className="size-11 aria-pressed:ring-2 aria-pressed:ring-ring"
                    aria-label={t(($) => $.issue_statuses.editor.icon_shapes[icon || "default"])}
                    aria-pressed={draft.icon === icon}
                    title={t(($) => $.issue_statuses.editor.icon_shapes[icon || "default"])}
                    onClick={() => {
                      setIconChanged(true);
                      setDraft((current) => ({ ...current, icon }));
                    }}
                  >
                    <StatusIcon status="" category={draft.category} color={draft.color} icon={icon} className="size-5" />
                  </Button>
                ))}
              </div>
            </fieldset>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t(($) => $.issue_statuses.editor.cancel)}
            </Button>
            <Button
              type="submit"
              disabled={!draft.name.trim() || create.isPending || update.isPending}
            >
              {create.isPending || update.isPending
                ? t(($) => $.issue_statuses.editor.saving)
                : t(($) => $.issue_statuses.editor.save)}
            </Button>
          </DialogFooter>
        </form>
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
