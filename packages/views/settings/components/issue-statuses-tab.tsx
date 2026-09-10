"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Archive,
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
} from "@multica/core/types";
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
 * Built-ins are shown but locked, while custom statuses can be added and
 * ordered within any group.
 *
 * The chrome is deliberately thin (MUL-6422): one bordered workflow list,
 * muted group headers, and row-level actions revealed only where available.
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
  category: "unstarted",
  color: COLOR_PICKER_PRESETS[6]!,
};

export function IssueStatusesTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();

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
        const inCategory = statuses.filter(
          (status) => normalizeIssueStatusCategory(status.category) === category,
        );
        return {
          category,
          system: inCategory.filter((status) => status.is_system),
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
      <div className="space-y-4">
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
                key={group.category}
                category={group.category}
                system={group.system}
                custom={group.custom}
                canManage={isAdmin}
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
        category={
          editing ? normalizeIssueStatusCategory(editing.category) : null
        }
        status={editing}
      />
      <ArchiveStatusDialog status={pendingArchive} onClose={() => setPendingArchive(null)} />
    </SettingsTab>
  );
}

function CategorySection({
  category,
  system,
  custom,
  canManage,
  onCreate,
  onEdit,
  onArchive,
}: {
  category: IssueStatusCategory;
  system: IssueStatusEntry[];
  custom: IssueStatusEntry[];
  canManage: boolean;
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
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
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
    <section
      aria-labelledby={`issue-status-category-${category}`}
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
        {system.map((entry) => (
          <BuiltInRow
            key={entry.id}
            entry={entry}
            label={labelOf(entry.key)}
            description={t(($) =>
              $.issue_statuses.built_in_descriptions[entry.key as BuiltInIssueStatus],
            )}
          />
        ))}
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
    </section>
  );
}

function BuiltInRow({
  entry,
  label,
  description,
}: {
  entry: IssueStatusEntry;
  label: string;
  description: string;
}) {
  return (
    <div className="flex min-h-12 items-center gap-3 px-4 py-2">
      <StatusIcon
        status={entry.key}
        category={normalizeIssueStatusCategory(entry.category) ?? "unstarted"}
        className="size-4"
      />
      <div className="min-w-0">
        <p className="truncate text-body font-medium">{label}</p>
        <p className="truncate text-caption text-muted-foreground">{description}</p>
      </div>
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
      className={`group/row relative flex min-h-12 items-center gap-3 bg-card px-4 py-2 ${isDragging ? "z-10 shadow-[var(--surface-shadow)]" : ""} ${archived ? "opacity-60" : ""}`}
    >
      {/* The handle rides inside the row's own left padding instead of taking a
          column of its own. A reserved gutter indents every status away from
          the card edge — including the built-in rows, which can never be
          dragged — and that indent is what the list reads as. (MUL-6422) */}
      {canReorder && (
        <button
          type="button"
          aria-label={t(($) => $.issue_statuses.actions.reorder, { name: entry.name })}
          className="absolute left-0 top-1/2 flex w-4 -translate-y-1/2 cursor-grab justify-center text-faint-foreground opacity-0 transition-opacity group-hover/row:opacity-100 focus-visible:opacity-100 active:cursor-grabbing"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-4" />
        </button>
      )}
      <StatusIcon
        status={entry.key}
        category={normalizeIssueStatusCategory(entry.category) ?? "unstarted"}
        color={issueStatusColor(entry)}
        className="size-4"
      />
      {/* Name over description, the way the row is read. The old layout pinned
          the description to the far right, which left a column of em dashes on
          every status nobody had described. */}
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-body font-medium">{entry.name}</span>
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
        {entry.description && (
          <p className="truncate text-caption text-muted-foreground">{entry.description}</p>
        )}
      </div>
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

  const categoryItems = ALL_STATUSES.map((c) => ({
    value: c,
    label: t(($) => $.issue_statuses.category_labels[c]),
  }));

  useEffect(() => {
    if (!open) return;
    setDraft(
      status
        ? {
            name: status.name,
            description: status.description ?? "",
            category: normalizeIssueStatusCategory(status.category) ?? "unstarted",
            color: status.color,
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
                      className="size-5"
                    />
                    <span className="font-mono text-caption uppercase text-muted-foreground">
                      {draft.color}
                    </span>
                  </button>
                }
              />
            </div>
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
