"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Loader2, Plus, Search, Star, StarOff, Trash2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type { AgentConfigTemplate } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
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
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { configTemplateKeys } from "./config-template-keys";
import { TemplateConfigEditor } from "./defaults-detail";
import { useT } from "../../i18n";

// ─── Centered config-template manager ───────────────────────────────────────
// Replaces the old right-side Sheet. A large centered modal: searchable list
// of templates on the left, the selected template's structured config form on
// the right (reused from DefaultsForm — config items unchanged). The default
// template of a scope IS that scope's default config (migration 125), so it is
// just the is_default entry, pinned to the top. Admin can pick which template
// is default. No raw JSON — templates are edited as structured configs.

interface ConfigTemplateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  scope: "system" | "personal";
  /** Editors can create / set-default / delete / edit config. Viewers see a
   *  read-only list + read-only form. System scope → admin only; personal
   *  scope → always the owner. */
  canEdit: boolean;
}

export function ConfigTemplateDialog({
  open,
  onOpenChange,
  scope,
  canEdit,
}: ConfigTemplateDialogProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<AgentConfigTemplate | null>(
    null,
  );

  const { data: templates = [], isLoading } = useQuery({
    queryKey: configTemplateKeys.list(wsId, scope),
    queryFn: () => api.listAgentConfigTemplates(scope),
  });

  // Default template first, then by name. Stable order so the list doesn't
  // jump as the default toggle flips.
  const sorted = useMemo(
    () =>
      [...templates].sort(
        (a, b) =>
          Number(b.is_default) - Number(a.is_default) ||
          a.name.localeCompare(b.name),
      ),
    [templates],
  );

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return sorted;
    return sorted.filter(
      (tpl) =>
        tpl.name.toLowerCase().includes(q) ||
        (tpl.description ?? "").toLowerCase().includes(q),
    );
  }, [sorted, search]);

  const selected = sorted.find((tpl) => tpl.id === selectedId) ?? null;

  // Auto-select the first (default) template on open with nothing selected.
  useEffect(() => {
    if (open && !selectedId) {
      const first = sorted[0];
      if (first) setSelectedId(first.id);
    }
  }, [open, sorted, selectedId]);

  const invalidate = useCallback(() => {
    qc.invalidateQueries({ queryKey: configTemplateKeys.all(wsId) });
  }, [qc, wsId]);

  const toggleDefault = useCallback(
    async (tpl: AgentConfigTemplate) => {
      try {
        await api.updateAgentConfigTemplate(tpl.id, {
          is_default: !tpl.is_default,
        });
        invalidate();
        toast.success(
          tpl.is_default
            ? t(($) => $.template.unset_default_toast)
            : t(($) => $.template.set_default_toast),
        );
      } catch (e) {
        toast.error(
          e instanceof ApiError ? e.message : t(($) => $.template.update_failed),
        );
      }
    },
    [invalidate, t],
  );

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return;
    try {
      await api.deleteAgentConfigTemplate(deleteTarget.id);
      invalidate();
      if (selectedId === deleteTarget.id) setSelectedId(null);
      toast.success(t(($) => $.template.deleted_toast));
    } catch (e) {
      const msg =
        e instanceof ApiError
          ? e.message
          : e instanceof Error
            ? e.message
            : t(($) => $.template.delete_failed_toast);
      toast.error(msg);
    } finally {
      setDeleteTarget(null);
    }
  }, [deleteTarget, invalidate, selectedId, t]);

  const handleCreated = useCallback(
    (id: string) => {
      invalidate();
      setSelectedId(id);
      setShowCreate(false);
    },
    [invalidate],
  );

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="flex h-[80vh] max-h-[calc(100vh-2rem)] max-w-5xl flex-col gap-0 p-0 sm:max-w-5xl">
          <DialogHeader className="flex h-12 shrink-0 flex-row items-center justify-between gap-2 border-b px-4 pr-12">
            <DialogTitle className="text-sm">
              {scope === "system"
                ? t(($) => $.template.dialog_title_system)
                : t(($) => $.template.dialog_title_personal)}
            </DialogTitle>
            {canEdit && (
              <Button size="sm" variant="outline" onClick={() => setShowCreate(true)}>
                <Plus className="mr-1 h-3.5 w-3.5" />
                {t(($) => $.template.create)}
              </Button>
            )}
            <DialogDescription className="sr-only">
              {t(($) => $.template.dialog_desc)}
            </DialogDescription>
          </DialogHeader>

          <div className="flex min-h-0 flex-1">
            {/* List pane */}
            <div className="flex w-64 shrink-0 flex-col border-r">
              <div className="border-b p-2">
                <div className="relative">
                  <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder={t(($) => $.template.search_placeholder)}
                    className="h-8 pl-8 text-xs"
                  />
                </div>
              </div>
              <ScrollArea className="min-h-0 flex-1">
                {isLoading ? (
                  <div className="flex justify-center py-8">
                    <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                  </div>
                ) : filtered.length === 0 ? (
                  <div className="p-4 text-center text-xs text-muted-foreground">
                    {search
                      ? t(($) => $.template.no_results)
                      : t(($) => $.template.empty)}
                  </div>
                ) : (
                  <ul className="space-y-0.5 p-1.5">
                    {filtered.map((tpl) => {
                      const active = tpl.id === selected?.id;
                      return (
                        <li key={tpl.id}>
                          <div
                            role="button"
                            tabIndex={0}
                            onClick={() => setSelectedId(tpl.id)}
                            onKeyDown={(e) => {
                              if (e.key === "Enter" || e.key === " ") {
                                e.preventDefault();
                                setSelectedId(tpl.id);
                              }
                            }}
                            className={`flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-left text-xs transition-colors ${
                              active ? "bg-accent" : "hover:bg-accent/50"
                            }`}
                          >
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-1.5">
                                <span className="truncate font-medium">{tpl.name}</span>
                                {tpl.is_default && (
                                  <Badge variant="secondary" className="h-4 px-1 text-[10px]">
                                    {t(($) => $.template.default_badge)}
                                  </Badge>
                                )}
                              </div>
                              {tpl.description && (
                                <div className="truncate text-muted-foreground">
                                  {tpl.description}
                                </div>
                              )}
                            </div>
                            {canEdit && (
                              <span
                                className="flex shrink-0 items-center gap-0.5"
                                onClick={(e) => e.stopPropagation()}
                              >
                                <Button
                                  size="icon"
                                  variant="ghost"
                                  className="h-6 w-6"
                                  onClick={() => toggleDefault(tpl)}
                                  title={
                                    tpl.is_default
                                      ? t(($) => $.template.unset_default)
                                      : t(($) => $.template.set_default)
                                  }
                                >
                                  {tpl.is_default ? (
                                    <Star className="h-3.5 w-3.5 fill-yellow-400 text-yellow-400" />
                                  ) : (
                                    <StarOff className="h-3.5 w-3.5 text-muted-foreground" />
                                  )}
                                </Button>
                                <Button
                                  size="icon"
                                  variant="ghost"
                                  className="h-6 w-6 text-muted-foreground hover:text-destructive"
                                  onClick={() => setDeleteTarget(tpl)}
                                  title={t(($) => $.template.delete)}
                                >
                                  <Trash2 className="h-3.5 w-3.5" />
                                </Button>
                              </span>
                            )}
                          </div>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </ScrollArea>
            </div>

            {/* Detail pane */}
            <div className="min-w-0 flex-1">
              {selected ? (
                <TemplateConfigEditor
                  template={selected}
                  scope={scope}
                  readOnly={!canEdit}
                />
              ) : (
                <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
                  {canEdit
                    ? t(($) => $.template.select_or_create_hint)
                    : t(($) => $.template.empty)}
                </div>
              )}
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {showCreate && (
        <CreateTemplateDialog
          scope={scope}
          onClose={() => setShowCreate(false)}
          onCreated={handleCreated}
        />
      )}

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(v) => {
          if (!v) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.template.delete_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.template.delete_confirm_desc, { name: deleteTarget?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.template.cancel)}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={handleDelete}>
              {t(($) => $.template.delete)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

// ─── Create template dialog ──────────────────────────────────────────────────
// Structured create: just a name (config starts empty and is filled via the
// structured editor). No raw JSON textarea.

function CreateTemplateDialog({
  scope,
  onClose,
  onCreated,
}: {
  scope: "system" | "personal";
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const { t } = useT("agents");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = useCallback(async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setError(t(($) => $.template.name_required));
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const created = await api.createAgentConfigTemplate({
        scope,
        name: trimmed,
        description: description.trim(),
        is_default: false,
        config: {},
      });
      toast.success(t(($) => $.template.created_toast));
      onCreated(created.id);
    } catch (e) {
      setError(
        e instanceof ApiError
          ? e.message
          : e instanceof Error
            ? e.message
            : t(($) => $.template.create_failed_toast),
      );
    } finally {
      setSubmitting(false);
    }
  }, [name, description, scope, onCreated, t]);

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {scope === "system"
              ? t(($) => $.template.create_dialog_title_system)
              : t(($) => $.template.create_dialog_title_personal)}
          </DialogTitle>
          <DialogDescription>{t(($) => $.template.create_dialog_desc)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">{t(($) => $.template.name_label)}</Label>
            <Input
              autoFocus
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (error) setError(null);
              }}
              placeholder={t(($) => $.template.name_placeholder)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  void handleSubmit();
                }
              }}
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">{t(($) => $.template.description_label)}</Label>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t(($) => $.template.description_placeholder)}
            />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
            {t(($) => $.template.cancel)}
          </Button>
          <Button size="sm" onClick={() => void handleSubmit()} disabled={submitting}>
            {submitting && <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />}
            {t(($) => $.template.create)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
