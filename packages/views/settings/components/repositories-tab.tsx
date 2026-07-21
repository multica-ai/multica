"use client";

import { useState } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
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
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { Workspace, WorkspaceRepo } from "@multica/core/types";
import { useT } from "../../i18n";
import { SettingsCard, SettingsTab } from "./settings-layout";

interface RepositoryEditorState {
  index: number | null;
  repository: WorkspaceRepo;
}

function normalizeRepository(repository: WorkspaceRepo): WorkspaceRepo {
  const description = repository.description?.trim();
  return {
    url: repository.url.trim(),
    ...(description ? { description } : {}),
  };
}

export function RepositoriesTab() {
  const { t } = useT("settings");
  const user = useAuthStore((state) => state.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [editor, setEditor] = useState<RepositoryEditorState | null>(null);
  const [pendingRemovalIndex, setPendingRemovalIndex] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);

  const currentMember = members.find((member) => member.user_id === user?.id) ?? null;
  const canManageWorkspace =
    currentMember?.role === "owner" || currentMember?.role === "admin";
  const repositories = workspace?.repos ?? [];

  const saveRepositories = async (next: WorkspaceRepo[]) => {
    if (!workspace) return false;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, { repos: next });
      queryClient.setQueryData(
        workspaceKeys.list(),
        (old: Workspace[] | undefined) =>
          old?.map((item) => (item.id === updated.id ? updated : item)),
      );
      toast.success(t(($) => $.repositories.toast_saved));
      return true;
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.repositories.toast_save_failed),
      );
      return false;
    } finally {
      setSaving(false);
    }
  };

  const saveRepository = async (repository: WorkspaceRepo) => {
    if (!editor) return;
    const normalized = normalizeRepository(repository);
    const next =
      editor.index === null
        ? [...repositories, normalized]
        : repositories.map((item, index) =>
            index === editor.index ? normalized : item,
          );
    if (await saveRepositories(next)) setEditor(null);
  };

  const removeRepository = async () => {
    if (pendingRemovalIndex === null) return;
    const next = repositories.filter((_, index) => index !== pendingRemovalIndex);
    if (await saveRepositories(next)) setPendingRemovalIndex(null);
  };

  if (!workspace) return null;

  return (
    <SettingsTab
      title={t(($) => $.page.tabs.repositories)}
      description={t(($) => $.repositories.description)}
    >
      <SettingsCard>
        {repositories.length === 0 ? (
          <div className="px-4 py-10 text-center text-sm text-muted-foreground">
            {t(($) => $.repositories.empty)}
          </div>
        ) : null}

        {repositories.map((repository, index) => (
          <div
            key={`${repository.url}-${index}`}
            className="group flex min-h-16 items-center gap-3 px-4 py-3.5"
          >
            <div className="min-w-0 flex-1">
              <div
                className="truncate font-mono text-sm"
                title={repository.url}
              >
                {repository.url}
              </div>
              {repository.description ? (
                <div
                  className="mt-1 truncate text-xs text-muted-foreground"
                  title={repository.description}
                >
                  {repository.description}
                </div>
              ) : null}
            </div>
            {canManageWorkspace ? (
              <div className="flex shrink-0 items-center gap-1">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(($) => $.repositories.edit_aria)}
                  className="text-muted-foreground hover:text-foreground"
                  onClick={() => setEditor({ index, repository })}
                >
                  <Pencil className="size-3.5" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(($) => $.repositories.delete_aria)}
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => setPendingRemovalIndex(index)}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </div>
            ) : null}
          </div>
        ))}

        {canManageWorkspace ? (
          <div className="px-4 py-3.5">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setEditor({ index: null, repository: { url: "" } })}
            >
              <Plus className="size-3.5" />
              {t(($) => $.repositories.add)}
            </Button>
          </div>
        ) : (
          <div className="px-4 py-3.5 text-xs text-muted-foreground">
            {t(($) => $.repositories.manage_hint)}
          </div>
        )}
      </SettingsCard>

      <RepositoryEditorDialog
        key={editor ? `${editor.index ?? "new"}-${editor.repository.url}` : "closed"}
        open={editor !== null}
        repository={editor?.repository}
        saving={saving}
        onOpenChange={(open) => {
          if (!open && !saving) setEditor(null);
        }}
        onSave={saveRepository}
      />

      <AlertDialog
        open={pendingRemovalIndex !== null}
        onOpenChange={(open) => {
          if (!open && !saving) setPendingRemovalIndex(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.repositories.delete_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.repositories.delete_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={saving}>
              {t(($) => $.repositories.delete_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={saving}
              onClick={(event) => {
                event.preventDefault();
                void removeRepository();
              }}
            >
              {t(($) => $.repositories.delete_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}

function RepositoryEditorDialog({
  open,
  repository,
  saving,
  onOpenChange,
  onSave,
}: {
  open: boolean;
  repository?: WorkspaceRepo;
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (repository: WorkspaceRepo) => Promise<void>;
}) {
  const { t } = useT("settings");
  const [url, setUrl] = useState(repository?.url ?? "");
  const [description, setDescription] = useState(repository?.description ?? "");
  const isEditing = Boolean(repository?.url);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {isEditing
              ? t(($) => $.repositories.edit_title)
              : t(($) => $.repositories.add_title)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.repositories.editor_description)}
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault();
            if (!url.trim() || saving) return;
            void onSave({ url, description });
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="repository-url">
              {t(($) => $.repositories.url_label)}
            </Label>
            <Input
              id="repository-url"
              type="text"
              autoFocus
              autoComplete="off"
              spellCheck={false}
              value={url}
              onChange={(event) => setUrl(event.target.value)}
              placeholder={t(($) => $.repositories.url_placeholder)}
              aria-invalid={!url.trim()}
              className="font-mono text-xs"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="repository-description">
              {t(($) => $.repositories.description_label)}
            </Label>
            <Input
              id="repository-description"
              type="text"
              autoComplete="off"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder={t(($) => $.repositories.description_placeholder)}
            />
          </div>
          <DialogFooter>
            <DialogClose
              render={<Button type="button" variant="outline" disabled={saving} />}
            >
              {t(($) => $.repositories.cancel)}
            </DialogClose>
            <Button type="submit" disabled={saving || !url.trim()}>
              {saving
                ? t(($) => $.repositories.saving)
                : t(($) => $.repositories.save)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
