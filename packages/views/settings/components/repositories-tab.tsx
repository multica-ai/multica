"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
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
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { Workspace, WorkspaceRepo } from "@multica/core/types";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsSaveState,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";
import { useAutoSave } from "./use-auto-save";

const EMPTY_REPOSITORIES: WorkspaceRepo[] = [];

function repositoriesEqual(left: WorkspaceRepo[], right: WorkspaceRepo[]) {
  if (left.length !== right.length) return false;
  return left.every(
    (repo, index) =>
      repo.url === right[index]?.url &&
      (repo.description ?? "") === (right[index]?.description ?? ""),
  );
}

export function RepositoriesTab() {
  const { t } = useT("settings");
  const user = useAuthStore((state) => state.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [repositories, setRepositories] = useState<WorkspaceRepo[]>(
    workspace?.repos ?? EMPTY_REPOSITORIES,
  );
  const [pendingRemovalIndex, setPendingRemovalIndex] = useState<number | null>(null);

  const currentMember = members.find((member) => member.user_id === user?.id) ?? null;
  const canManageWorkspace =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  useEffect(() => {
    setRepositories(workspace?.repos ?? EMPTY_REPOSITORIES);
    // A cache update after auto-save replaces the Workspace object. Keying on
    // identity prevents that response from wiping a newer local keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally keyed on workspace identity
  }, [workspace?.id]);

  const savedRepositories = workspace?.repos ?? EMPTY_REPOSITORIES;
  const draft = useMemo(() => repositories, [repositories]);
  const saveRepositories = useCallback(
    async (next: WorkspaceRepo[]) => {
      if (!workspace) return;
      const updated = await api.updateWorkspace(workspace.id, { repos: next });
      queryClient.setQueryData(
        workspaceKeys.list(),
        (old: Workspace[] | undefined) =>
          old?.map((item) => (item.id === updated.id ? updated : item)),
      );
    },
    [queryClient, workspace],
  );
  const allUrlsValid = repositories.every((repo) => repo.url.trim().length > 0);
  const autoSave = useAutoSave({
    value: draft,
    savedValue: savedRepositories,
    onSave: saveRepositories,
    onSuccess: () =>
      toast.success(t(($) => $.repositories.toast_saved), {
        id: "settings-auto-save",
      }),
    onError: (error) =>
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.repositories.toast_save_failed),
      ),
    enabled: !!workspace && canManageWorkspace && allUrlsValid,
    isEqual: repositoriesEqual,
  });

  const updateRepository = (
    index: number,
    field: keyof WorkspaceRepo,
    value: string,
  ) => {
    setRepositories((current) =>
      current.map((repo, repoIndex) =>
        repoIndex === index ? { ...repo, [field]: value } : repo,
      ),
    );
  };

  const addRepository = () => {
    setRepositories((current) => [...current, { url: "" }]);
  };

  const removeRepository = (index: number) => {
    const next = repositories.filter((_, repoIndex) => repoIndex !== index);
    setRepositories(next);
    autoSave.saveNow(next);
  };

  if (!workspace) return null;

  return (
    <SettingsTab title={t(($) => $.page.tabs.repositories)}>
      <SettingsSection
        description={t(($) => $.repositories.description)}
        action={
          <SettingsSaveState
            status={autoSave.status}
            savingLabel={t(($) => $.auto_save.saving)}
            savedLabel={t(($) => $.auto_save.saved)}
            errorLabel={t(($) => $.auto_save.failed)}
          />
        }
      >
        <SettingsCard>
          {repositories.length === 0 ? (
            <div className="px-4 py-8 text-center text-xs text-muted-foreground">
              {t(($) => $.repositories.empty)}
            </div>
          ) : null}

          {repositories.map((repository, index) => (
            <div
              key={index}
              className="grid gap-2 px-4 py-3.5 sm:grid-cols-[minmax(0,1fr)_minmax(0,0.8fr)_auto] sm:items-center"
            >
              <Input
                type="text"
                name={`repository-${index}-url`}
                autoComplete="off"
                spellCheck={false}
                aria-label={t(($) => $.repositories.url_placeholder)}
                value={repository.url}
                onChange={(event) =>
                  updateRepository(index, "url", event.target.value)
                }
                onBlur={autoSave.flush}
                disabled={!canManageWorkspace}
                aria-invalid={!repository.url.trim()}
                placeholder={t(($) => $.repositories.url_placeholder)}
                className="font-mono text-xs"
              />
              <Input
                type="text"
                name={`repository-${index}-description`}
                autoComplete="off"
                aria-label={t(($) => $.repositories.description_placeholder)}
                value={repository.description ?? ""}
                onChange={(event) =>
                  updateRepository(index, "description", event.target.value)
                }
                onBlur={autoSave.flush}
                disabled={!canManageWorkspace}
                placeholder={t(($) => $.repositories.description_placeholder)}
              />
              {canManageWorkspace ? (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(($) => $.repositories.delete_aria)}
                  className="justify-self-end text-muted-foreground hover:text-destructive"
                  onClick={() => setPendingRemovalIndex(index)}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              ) : null}
            </div>
          ))}

          {canManageWorkspace ? (
            <div className="flex items-center justify-between gap-3 px-4 py-3.5">
              <Button variant="outline" size="sm" onClick={addRepository}>
                <Plus className="size-3.5" />
                {t(($) => $.repositories.add)}
              </Button>
              {!allUrlsValid ? (
                <span className="text-xs text-muted-foreground">
                  {t(($) => $.repositories.url_empty)}
                </span>
              ) : null}
            </div>
          ) : (
            <div className="px-4 py-3 text-xs text-muted-foreground">
              {t(($) => $.repositories.manage_hint)}
            </div>
          )}
        </SettingsCard>
      </SettingsSection>

      <AlertDialog
        open={pendingRemovalIndex !== null}
        onOpenChange={(open) => {
          if (!open) setPendingRemovalIndex(null);
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
            <AlertDialogCancel>
              {t(($) => $.repositories.delete_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (pendingRemovalIndex !== null) {
                  removeRepository(pendingRemovalIndex);
                }
                setPendingRemovalIndex(null);
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
