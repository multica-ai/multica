"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { MoreHorizontal, Pencil, Plus, Search, Tag, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useFeatureEnabled } from "@multica/core/config";
import { RESOURCE_LABELS_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  labelListOptions,
  useCreateLabel,
  useDeleteLabel,
  useUpdateLabel,
} from "@multica/core/labels";
import type { Label, LabelResourceType } from "@multica/core/types";
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
import { cn } from "@multica/ui/lib/utils";
import { ColorPicker, COLOR_PICKER_PRESETS } from "../../common/color-picker";
import { useT } from "../../i18n";
import { SettingsTab } from "./settings-layout";

const RESOURCE_TYPES: LabelResourceType[] = ["issue", "agent", "skill"];
const ISSUE_RESOURCE_TYPES: LabelResourceType[] = ["issue"];

interface LabelDraft {
  name: string;
  description: string;
  color: string;
}

const EMPTY_DRAFT: LabelDraft = {
  name: "",
  description: "",
  color: COLOR_PICKER_PRESETS[6],
};

export function LabelsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const resourceLabelsEnabled = useFeatureEnabled(RESOURCE_LABELS_FLAG, false);

  const [resourceType, setResourceType] = useState<LabelResourceType>("issue");
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<Label | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Label | null>(null);

  const resourceTypes = resourceLabelsEnabled ? RESOURCE_TYPES : ISSUE_RESOURCE_TYPES;

  useEffect(() => {
    if (!resourceLabelsEnabled && resourceType !== "issue") {
      setResourceType("issue");
      setQuery("");
    }
  }, [resourceLabelsEnabled, resourceType]);

  const { data: labels = [], isLoading } = useQuery(
    labelListOptions(wsId, resourceType),
  );
  const filteredLabels = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) return labels;
    return labels.filter(
      (label) =>
        label.name.toLowerCase().includes(normalized) ||
        (label.description ?? "").toLowerCase().includes(normalized),
    );
  }, [labels, query]);

  const scopeLabel = t(($) => $.labels.scopes[resourceType]);

  return (
    <SettingsTab
      title={t(($) => $.labels.title)}
      description={t(($) => $.labels.description)}
    >
      <div className="space-y-5">
        <div className="flex flex-wrap items-center gap-2 border-b border-surface-border pb-3">
          {resourceTypes.map((type) => (
            <Button
              key={type}
              type="button"
              size="sm"
              variant={resourceType === type ? "secondary" : "ghost"}
              className={cn(
                "gap-2",
                resourceType === type && "bg-surface-selected text-surface-selected-foreground",
              )}
              onClick={() => {
                setResourceType(type);
                setQuery("");
              }}
            >
              <Tag className="size-3.5" />
              {t(($) => $.labels.scopes[type])}
              <span className="text-xs tabular-nums text-muted-foreground">
                {type === resourceType ? labels.length : ""}
              </span>
            </Button>
          ))}
        </div>

        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="relative w-full sm:max-w-sm">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t(($) => $.labels.search_placeholder)}
              className="pl-9"
            />
          </div>
          <Button className="gap-2" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" />
            {t(($) => $.labels.new_label)}
          </Button>
        </div>

        <div className="overflow-hidden rounded-lg border border-surface-border bg-card">
          <div className="hidden grid-cols-[minmax(11rem,1fr)_minmax(12rem,1.4fr)_6rem_7rem_2rem] gap-4 border-b border-surface-border bg-muted/20 px-4 py-2.5 text-xs font-medium text-muted-foreground md:grid">
            <span>{t(($) => $.labels.columns.name)}</span>
            <span>{t(($) => $.labels.columns.description)}</span>
            <span>{t(($) => $.labels.columns.usage)}</span>
            <span>{t(($) => $.labels.columns.updated)}</span>
            <span />
          </div>

          {isLoading ? (
            <div className="px-4 py-12 text-center text-sm text-muted-foreground">
              {t(($) => $.labels.loading)}
            </div>
          ) : filteredLabels.length === 0 ? (
            <div className="px-4 py-12 text-center">
              <Tag className="mx-auto size-6 text-muted-foreground/60" />
              <p className="mt-3 text-sm font-medium">
                {query
                  ? t(($) => $.labels.no_results)
                  : t(($) => $.labels.empty, { scope: scopeLabel })}
              </p>
            </div>
          ) : (
            <div className="divide-y divide-surface-border">
              {filteredLabels.map((label) => (
                <div
                  key={label.id}
                  className="grid gap-2 px-4 py-3 md:grid-cols-[minmax(11rem,1fr)_minmax(12rem,1.4fr)_6rem_7rem_2rem] md:items-center md:gap-4"
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <span
                      className="size-2.5 shrink-0 rounded-full"
                      style={{ backgroundColor: label.color }}
                    />
                    <span className="truncate text-sm font-medium">{label.name}</span>
                  </div>
                  <p className="min-w-0 truncate text-xs text-muted-foreground md:text-sm">
                    {label.description || "—"}
                  </p>
                  <span className="text-xs tabular-nums text-muted-foreground md:text-sm">
                    {t(($) => $.labels.usage_count, { count: label.usage_count ?? 0 })}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {new Date(label.updated_at).toLocaleDateString()}
                  </span>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={t(($) => $.labels.actions.open, { name: label.name })}
                        >
                          <MoreHorizontal className="size-4" />
                        </Button>
                      }
                    />
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onClick={() => setEditing(label)}>
                        <Pencil className="size-4" />
                        {t(($) => $.labels.actions.edit)}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => setPendingDelete(label)}
                      >
                        <Trash2 className="size-4" />
                        {t(($) => $.labels.actions.delete)}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      <LabelEditorDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        resourceType={resourceType}
      />
      <LabelEditorDialog
        open={Boolean(editing)}
        onOpenChange={(open) => !open && setEditing(null)}
        resourceType={resourceType}
        label={editing}
      />
      <DeleteLabelDialog
        label={pendingDelete}
        onClose={() => setPendingDelete(null)}
      />
    </SettingsTab>
  );
}

function LabelEditorDialog({
  open,
  onOpenChange,
  resourceType,
  label,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  resourceType: LabelResourceType;
  label?: Label | null;
}) {
  const { t } = useT("settings");
  const create = useCreateLabel();
  const update = useUpdateLabel();
  const [draft, setDraft] = useState<LabelDraft>(EMPTY_DRAFT);

  useEffect(() => {
    if (!open) return;
    setDraft(
      label
        ? {
            name: label.name,
            description: label.description ?? "",
            color: label.color,
          }
        : EMPTY_DRAFT,
    );
  }, [label, open]);

  const submit = () => {
    const name = draft.name.trim();
    if (!name) return;
    if (label) {
      update.mutate(
        {
          id: label.id,
          resource_type: label.resource_type ?? resourceType,
          name,
          description: draft.description.trim(),
          color: draft.color,
        },
        {
          onSuccess: () => onOpenChange(false),
          onError: (error) =>
            toast.error(error instanceof Error ? error.message : t(($) => $.labels.save_failed)),
        },
      );
      return;
    }
    create.mutate(
      {
        resource_type: resourceType,
        name,
        description: draft.description.trim(),
        color: draft.color,
      },
      {
        onSuccess: () => onOpenChange(false),
        onError: (error) =>
          toast.error(error instanceof Error ? error.message : t(($) => $.labels.save_failed)),
      },
    );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {label ? t(($) => $.labels.editor.edit_title) : t(($) => $.labels.editor.create_title)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.labels.editor.scope_hint, {
              scope: t(($) => $.labels.scopes[resourceType]),
            })}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-5 py-2">
          <div className="space-y-2">
            <FieldLabel htmlFor="label-name">{t(($) => $.labels.editor.name)}</FieldLabel>
            <Input
              id="label-name"
              autoFocus
              maxLength={32}
              value={draft.name}
              onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))}
              placeholder={t(($) => $.labels.editor.name_placeholder)}
            />
          </div>
          <div className="space-y-2">
            <FieldLabel htmlFor="label-description">
              {t(($) => $.labels.editor.description)}
            </FieldLabel>
            <Textarea
              id="label-description"
              rows={3}
              value={draft.description}
              onChange={(event) =>
                setDraft((current) => ({ ...current, description: event.target.value }))
              }
              placeholder={t(($) => $.labels.editor.description_placeholder)}
            />
          </div>
          <div className="space-y-2">
            <FieldLabel>{t(($) => $.labels.editor.color)}</FieldLabel>
            <ColorPicker
              value={draft.color}
              onChange={(color) => setDraft((current) => ({ ...current, color }))}
              trigger={
                <button
                  type="button"
                  aria-label={t(($) => $.labels.editor.color)}
                  className="flex h-9 items-center gap-2.5 rounded-md border border-surface-border px-2.5 transition-colors hover:bg-surface-hover"
                >
                  <span
                    className="size-5 rounded-full"
                    style={{ backgroundColor: draft.color }}
                  />
                  <span className="font-mono text-xs uppercase text-muted-foreground">
                    {draft.color}
                  </span>
                </button>
              }
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.labels.editor.cancel)}
          </Button>
          <Button
            onClick={submit}
            disabled={!draft.name.trim() || create.isPending || update.isPending}
          >
            {create.isPending || update.isPending
              ? t(($) => $.labels.editor.saving)
              : t(($) => $.labels.editor.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DeleteLabelDialog({
  label,
  onClose,
}: {
  label: Label | null;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const remove = useDeleteLabel();
  return (
    <AlertDialog open={Boolean(label)} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.labels.delete_dialog.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.labels.delete_dialog.description, {
              name: label?.name ?? "",
              count: label?.usage_count ?? 0,
            })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.labels.delete_dialog.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              if (!label) return;
              remove.mutate(
                { id: label.id, resource_type: label.resource_type ?? "issue" },
                {
                  onSuccess: onClose,
                  onError: (error) =>
                    toast.error(
                      error instanceof Error
                        ? error.message
                        : t(($) => $.labels.delete_dialog.failed),
                    ),
                },
              );
            }}
          >
            {t(($) => $.labels.delete_dialog.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
