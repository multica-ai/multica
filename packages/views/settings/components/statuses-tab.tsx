"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, Check, MoreHorizontal, Pencil, Plus } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueStatusCatalogOptions,
  useArchiveIssueStatus,
  useCreateIssueStatus,
  useUpdateIssueStatus,
} from "@multica/core/issue-statuses";
import { STATUS_COLORS, STATUS_ICONS } from "@multica/core/types";
import type { IssueStatusDefinition, StatusCategory } from "@multica/core/types";
import { statusThemeForColor } from "@multica/core/issues/config";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label as FieldLabel } from "@multica/ui/components/ui/label";
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
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "../../issues/components/status-icon";
import { useT } from "../../i18n";
import { SettingsTab } from "./settings-layout";

/**
 * Workspace status catalog management (MUL-4809 §7.1).
 *
 * Statuses are grouped by their Category, which is the ONLY machine semantics
 * and is immutable after create — so Category is a create-time choice presented
 * as a group, never an edit control. The 7 built-ins can be renamed/recolored
 * but never archived, and each Category always keeps exactly one default.
 */

// Fixed presentation order; matches the server's Category ordering.
const CATEGORY_ORDER: StatusCategory[] = [
  "backlog",
  "todo",
  "in_progress",
  "done",
  "cancelled",
];

interface StatusDraft {
  name: string;
  description: string;
  category: StatusCategory;
  icon: string;
  color: string;
  is_default: boolean;
}

const EMPTY_DRAFT: StatusDraft = {
  name: "",
  description: "",
  category: "todo",
  icon: "todo",
  color: "muted-foreground",
  is_default: false,
};

function draftFromStatus(status: IssueStatusDefinition): StatusDraft {
  return {
    name: status.name,
    description: status.description ?? "",
    category: status.category,
    icon: status.icon,
    color: status.color,
    is_default: status.is_default,
  };
}

/** A color swatch row — the allowlist is small and fixed, so no color wheel. */
function ColorChoices({
  value,
  onChange,
}: {
  value: string;
  onChange: (color: string) => void;
}) {
  return (
    <div className="flex flex-wrap gap-2">
      {STATUS_COLORS.map((color) => (
        <button
          key={color}
          type="button"
          aria-label={color}
          aria-pressed={value === color}
          onClick={() => onChange(color)}
          className={cn(
            "flex size-8 items-center justify-center rounded-md border transition",
            value === color
              ? "border-primary ring-2 ring-primary/30"
              : "border-surface-border hover:bg-accent",
          )}
        >
          <span
            className={cn("size-3.5 rounded-full", statusThemeForColor(color).dividerColor)}
          />
        </button>
      ))}
    </div>
  );
}

/** Icon shape picker — the 7 built-in glyphs. */
function IconChoices({
  value,
  color,
  onChange,
}: {
  value: string;
  color: string;
  onChange: (icon: string) => void;
}) {
  return (
    <div className="flex flex-wrap gap-2">
      {STATUS_ICONS.map((icon) => (
        <button
          key={icon}
          type="button"
          aria-label={icon}
          aria-pressed={value === icon}
          onClick={() => onChange(icon)}
          className={cn(
            "flex size-8 items-center justify-center rounded-md border transition",
            value === icon
              ? "border-primary ring-2 ring-primary/30"
              : "border-surface-border hover:bg-accent",
          )}
        >
          <StatusIcon status={icon} icon={icon} color={color} className="size-4" />
        </button>
      ))}
    </div>
  );
}

export function StatusesTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();

  const { data: catalog, isLoading } = useQuery(issueStatusCatalogOptions(wsId));
  const statuses = useMemo(() => catalog?.statuses ?? [], [catalog]);

  const [createFor, setCreateFor] = useState<StatusCategory | null>(null);
  const [editing, setEditing] = useState<IssueStatusDefinition | null>(null);
  const [draft, setDraft] = useState<StatusDraft>(EMPTY_DRAFT);
  const [pendingArchive, setPendingArchive] = useState<IssueStatusDefinition | null>(null);
  const [migrateTo, setMigrateTo] = useState<string>("");

  const createStatus = useCreateIssueStatus();
  const updateStatus = useUpdateIssueStatus();
  const archiveStatus = useArchiveIssueStatus();

  const byCategory = useMemo(() => {
    const grouped = new Map<StatusCategory, IssueStatusDefinition[]>();
    for (const category of CATEGORY_ORDER) grouped.set(category, []);
    for (const status of statuses) {
      if (status.archived) continue;
      grouped.get(status.category)?.push(status);
    }
    for (const list of grouped.values()) list.sort((a, b) => a.position - b.position);
    return grouped;
  }, [statuses]);

  /** Same-Category, non-archived, excluding the one being archived. */
  const migrationTargets = useMemo(() => {
    if (!pendingArchive) return [];
    return statuses.filter(
      (s) => !s.archived && s.category === pendingArchive.category && s.id !== pendingArchive.id,
    );
  }, [statuses, pendingArchive]);

  const openCreate = (category: StatusCategory) => {
    setDraft({ ...EMPTY_DRAFT, category, icon: category === "in_progress" ? "in_progress" : category });
    setCreateFor(category);
  };

  const openEdit = (status: IssueStatusDefinition) => {
    setDraft(draftFromStatus(status));
    setEditing(status);
  };

  const closeDialogs = () => {
    setCreateFor(null);
    setEditing(null);
    setDraft(EMPTY_DRAFT);
  };

  const handleCreate = async () => {
    const name = draft.name.trim();
    if (!name || !createFor) return;
    try {
      await createStatus.mutateAsync({
        name,
        category: createFor,
        description: draft.description.trim(),
        icon: draft.icon,
        color: draft.color,
        is_default: draft.is_default,
      });
      toast.success(t(($) => $.statuses.created));
      closeDialogs();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.statuses.create_failed));
    }
  };

  const handleUpdate = async () => {
    const name = draft.name.trim();
    if (!name || !editing) return;
    try {
      await updateStatus.mutateAsync({
        id: editing.id,
        name,
        description: draft.description.trim(),
        icon: draft.icon,
        color: draft.color,
        // Only ever promote: the server keeps exactly one default per Category,
        // so un-setting has no meaning — you promote a different status instead.
        ...(draft.is_default && !editing.is_default ? { is_default: true } : {}),
      });
      toast.success(t(($) => $.statuses.updated));
      closeDialogs();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.statuses.update_failed));
    }
  };

  const handleSetDefault = async (status: IssueStatusDefinition) => {
    try {
      await updateStatus.mutateAsync({ id: status.id, is_default: true });
      toast.success(t(($) => $.statuses.default_set, { name: status.name }));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.statuses.update_failed));
    }
  };

  const handleArchive = async () => {
    if (!pendingArchive) return;
    try {
      await archiveStatus.mutateAsync({
        id: pendingArchive.id,
        migrateToStatusId: migrateTo || undefined,
      });
      toast.success(t(($) => $.statuses.archived));
      setPendingArchive(null);
      setMigrateTo("");
    } catch (error) {
      // The server 409s when the status still holds issues and no migration
      // target was given; surface its message so the user knows to pick one.
      toast.error(error instanceof Error ? error.message : t(($) => $.statuses.archive_failed));
    }
  };

  const dialogOpen = createFor !== null || editing !== null;
  const isSaving = createStatus.isPending || updateStatus.isPending;

  return (
    <SettingsTab
      title={t(($) => $.statuses.title)}
      description={t(($) => $.statuses.description)}
    >
      {isLoading ? (
        <div className="px-4 py-12 text-center text-sm text-muted-foreground">
          {t(($) => $.statuses.loading)}
        </div>
      ) : (
        <div className="space-y-6">
          {CATEGORY_ORDER.map((category) => {
            const rows = byCategory.get(category) ?? [];
            return (
              <section key={category} className="space-y-2">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <h3 className="text-sm font-semibold">
                      {t(($) => $.statuses.categories[category].label)}
                    </h3>
                    <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
                      {t(($) => $.statuses.categories[category].hint)}
                    </p>
                  </div>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="gap-1.5 shrink-0"
                    onClick={() => openCreate(category)}
                  >
                    <Plus className="size-3.5" />
                    {t(($) => $.statuses.add)}
                  </Button>
                </div>

                <div className="overflow-hidden rounded-lg border border-surface-border bg-card">
                  {rows.length === 0 ? (
                    <p className="px-4 py-6 text-center text-sm text-muted-foreground">
                      {t(($) => $.statuses.empty_category)}
                    </p>
                  ) : (
                    <div className="divide-y divide-surface-border">
                      {rows.map((status) => (
                        <div
                          key={status.id}
                          className="flex items-center gap-3 px-4 py-2.5"
                        >
                          <StatusIcon
                            status={status.icon}
                            icon={status.icon}
                            color={status.color}
                            className="size-4"
                          />
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="truncate text-sm font-medium">{status.name}</span>
                              {status.is_default ? (
                                <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-primary">
                                  {t(($) => $.statuses.badge_default)}
                                </span>
                              ) : null}
                              {status.is_system ? (
                                <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                                  {t(($) => $.statuses.badge_system)}
                                </span>
                              ) : null}
                            </div>
                            {status.description ? (
                              <p className="mt-0.5 truncate text-xs text-muted-foreground">
                                {status.description}
                              </p>
                            ) : null}
                          </div>

                          <DropdownMenu>
                            <DropdownMenuTrigger
                              render={
                                <Button
                                  variant="ghost"
                                  size="icon-sm"
                                  className="shrink-0"
                                  aria-label={t(($) => $.statuses.actions_open, { name: status.name })}
                                >
                                  <MoreHorizontal className="size-4" />
                                </Button>
                              }
                            />
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem onSelect={() => openEdit(status)}>
                                <Pencil className="mr-2 size-3.5" />
                                {t(($) => $.statuses.edit)}
                              </DropdownMenuItem>
                              {!status.is_default ? (
                                <DropdownMenuItem onSelect={() => void handleSetDefault(status)}>
                                  <Check className="mr-2 size-3.5" />
                                  {t(($) => $.statuses.make_default)}
                                </DropdownMenuItem>
                              ) : null}
                              {/* System statuses are permanent (§5.4). */}
                              {!status.is_system ? (
                                <DropdownMenuItem
                                  variant="destructive"
                                  onSelect={() => {
                                    setMigrateTo("");
                                    setPendingArchive(status);
                                  }}
                                >
                                  <Archive className="mr-2 size-3.5" />
                                  {t(($) => $.statuses.archive)}
                                </DropdownMenuItem>
                              ) : null}
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </section>
            );
          })}
        </div>
      )}

      {/* Create / edit */}
      <Dialog open={dialogOpen} onOpenChange={(open) => (open ? null : closeDialogs())}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {editing ? t(($) => $.statuses.edit_title) : t(($) => $.statuses.create_title)}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.statuses.category_locked, {
                category: t(($) => $.statuses.categories[(editing?.category ?? createFor ?? "todo")].label),
              })}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-1.5">
              <FieldLabel htmlFor="status-name">{t(($) => $.statuses.field_name)}</FieldLabel>
              <Input
                id="status-name"
                value={draft.name}
                maxLength={32}
                onChange={(event) => setDraft((d) => ({ ...d, name: event.target.value }))}
                placeholder={t(($) => $.statuses.name_placeholder)}
              />
            </div>

            <div className="space-y-1.5">
              <FieldLabel htmlFor="status-description">
                {t(($) => $.statuses.field_description)}
              </FieldLabel>
              <Textarea
                id="status-description"
                value={draft.description}
                maxLength={500}
                rows={2}
                onChange={(event) => setDraft((d) => ({ ...d, description: event.target.value }))}
                placeholder={t(($) => $.statuses.description_placeholder)}
              />
            </div>

            <div className="space-y-1.5">
              <FieldLabel>{t(($) => $.statuses.field_color)}</FieldLabel>
              <ColorChoices
                value={draft.color}
                onChange={(color) => setDraft((d) => ({ ...d, color }))}
              />
            </div>

            <div className="space-y-1.5">
              <FieldLabel>{t(($) => $.statuses.field_icon)}</FieldLabel>
              <IconChoices
                value={draft.icon}
                color={draft.color}
                onChange={(icon) => setDraft((d) => ({ ...d, icon }))}
              />
            </div>

            {editing?.is_default ? null : (
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={draft.is_default}
                  onChange={(event) =>
                    setDraft((d) => ({ ...d, is_default: event.target.checked }))
                  }
                />
                <span>
                  {t(($) => $.statuses.make_default)}
                  <span className="block text-xs text-muted-foreground">
                    {t(($) => $.statuses.default_hint)}
                  </span>
                </span>
              </label>
            )}
          </div>

          <DialogFooter>
            <Button variant="ghost" onClick={closeDialogs}>
              {t(($) => $.statuses.cancel)}
            </Button>
            <Button
              disabled={!draft.name.trim() || isSaving}
              onClick={() => void (editing ? handleUpdate() : handleCreate())}
            >
              {editing ? t(($) => $.statuses.save) : t(($) => $.statuses.create)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Archive (soft delete) with same-Category migration target */}
      <AlertDialog
        open={pendingArchive !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingArchive(null);
            setMigrateTo("");
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.statuses.archive_title, { name: pendingArchive?.name ?? "" })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.statuses.archive_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>

          <div className="space-y-1.5">
            <FieldLabel>{t(($) => $.statuses.migrate_to)}</FieldLabel>
            <Select
              items={migrationTargets.map((target) => ({ label: target.name, value: target.id }))}
              value={migrateTo}
              onValueChange={(value) => setMigrateTo(value ?? "")}
            >
              <SelectTrigger>
                <SelectValue placeholder={t(($) => $.statuses.migrate_placeholder)} />
              </SelectTrigger>
              <SelectContent>
                {migrationTargets.map((target) => (
                  <SelectItem key={target.id} value={target.id}>
                    {target.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.statuses.migrate_hint)}
            </p>
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.statuses.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              disabled={archiveStatus.isPending}
              onClick={(event) => {
                event.preventDefault();
                void handleArchive();
              }}
            >
              {t(($) => $.statuses.archive)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}
