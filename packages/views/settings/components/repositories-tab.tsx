"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { LoaderCircle, Plus, Search, Trash2 } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
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
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import {
  githubInstallationRepositoriesOptions,
  githubInstallationsOptions,
} from "@multica/core/github";
import { api } from "@multica/core/api";
import type {
  GitHubRepository,
  Workspace,
  WorkspaceRepo,
} from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsSaveState,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";
import { useAutoSave } from "./use-auto-save";
import { GitHubMark } from "./github-mark";

const EMPTY_REPOSITORIES: WorkspaceRepo[] = [];

function repositoriesEqual(left: WorkspaceRepo[], right: WorkspaceRepo[]) {
  if (left.length !== right.length) return false;
  return left.every(
    (repo, index) =>
      repo.url === right[index]?.url &&
      (repo.description ?? "") === (right[index]?.description ?? ""),
  );
}

export function repositoryIdentity(rawURL: string): string | null {
  const value = rawURL.trim();
  if (!value) return null;

  let host = "";
  let path = "";
  if (!value.includes("://")) {
    const scpLike = value.match(/^(?:[^@\s/]+@)?([^:\s/]+):(.+)$/);
    if (scpLike) {
      host = scpLike[1] ?? "";
      path = scpLike[2] ?? "";
    }
  }
  if (!host) {
    try {
      const parsed = new URL(value);
      host = parsed.hostname;
      path = parsed.pathname;
    } catch {
      return null;
    }
  }

  const normalizedPath = path
    .replace(/^\/+|\/+$/g, "")
    .replace(/\.git$/i, "")
    .toLowerCase();
  if (!host || !normalizedPath) return null;
  return `${host.toLowerCase()}/${normalizedPath}`;
}

export function RepositoriesTab() {
  const { t } = useT("settings");
  const user = useAuthStore((state) => state.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const navigation = useNavigation();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [repositories, setRepositories] = useState<WorkspaceRepo[]>(
    workspace?.repos ?? EMPTY_REPOSITORIES,
  );
  const [pendingRemovalIndex, setPendingRemovalIndex] = useState<number | null>(null);
  const [connectingGitHub, setConnectingGitHub] = useState(false);
  const [githubPickerOpen, setGitHubPickerOpen] = useState(false);
  const [selectedInstallationID, setSelectedInstallationID] = useState("");
  const [selectedRepositories, setSelectedRepositories] = useState<
    Map<number, GitHubRepository>
  >(new Map());
  const [repositorySearch, setRepositorySearch] = useState("");

  const currentMember = members.find((member) => member.user_id === user?.id) ?? null;
  const canManageWorkspace =
    currentMember?.role === "owner" || currentMember?.role === "admin";
  const { data: githubData } = useQuery({
    ...githubInstallationsOptions(wsId),
    enabled: !!wsId && canManageWorkspace,
  });
  const githubInstallations = useMemo(
    () => githubData?.installations ?? [],
    [githubData?.installations],
  );
  const githubConnectConfigured = githubData?.configured === true;
  const githubBrowseConfigured =
    githubData?.repository_browse_configured === true;
  const githubRepositoriesQuery = useInfiniteQuery({
    ...githubInstallationRepositoriesOptions(wsId, selectedInstallationID),
    enabled:
      githubPickerOpen &&
      canManageWorkspace &&
      githubBrowseConfigured &&
      !!selectedInstallationID,
  });
  const githubRepositories = useMemo(
    () =>
      githubRepositoriesQuery.data?.pages.flatMap(
        (page) => page.repositories,
      ) ?? [],
    [githubRepositoriesQuery.data?.pages],
  );
  const existingRepositoryIdentities = useMemo(
    () =>
      new Set(
        repositories
          .map((repository) => repositoryIdentity(repository.url))
          .filter((identity): identity is string => !!identity),
      ),
    [repositories],
  );
  const filteredGitHubRepositories = useMemo(() => {
    const search = repositorySearch.trim().toLowerCase();
    if (!search) return githubRepositories;
    return githubRepositories.filter((repository) =>
      repository.full_name.toLowerCase().includes(search),
    );
  }, [githubRepositories, repositorySearch]);

  useEffect(() => {
    setRepositories(workspace?.repos ?? EMPTY_REPOSITORIES);
    // A cache update after auto-save replaces the Workspace object. Keying on
    // identity prevents that response from wiping a newer local keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentionally keyed on workspace identity
  }, [workspace?.id]);

  useEffect(() => {
    if (
      selectedInstallationID &&
      githubInstallations.some(
        (installation) => installation.id === selectedInstallationID,
      )
    ) {
      return;
    }
    setSelectedInstallationID(githubInstallations[0]?.id ?? "");
  }, [githubInstallations, selectedInstallationID]);

  useEffect(() => {
    const connected = navigation.searchParams.get("github_connected") === "1";
    const githubError = navigation.searchParams.get("github_error");
    if ((!connected && !githubError) || !canManageWorkspace) return;

    if (githubError) {
      toast.error(t(($) => $.repositories.github_connect_failed));
    } else if (githubInstallations.length > 0 && githubBrowseConfigured) {
      setSelectedInstallationID(githubInstallations[0]!.id);
      setGitHubPickerOpen(true);
    } else if (githubInstallations.length > 0) {
      toast.error(t(($) => $.repositories.github_browse_not_configured));
    } else {
      return;
    }

    const next = new URLSearchParams(navigation.searchParams);
    next.delete("github_connected");
    next.delete("github_error");
    const search = next.toString();
    navigation.replace(`${navigation.pathname}${search ? `?${search}` : ""}`);
  }, [
    canManageWorkspace,
    githubBrowseConfigured,
    githubInstallations,
    navigation,
    t,
  ]);

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

  const openGitHubPicker = () => {
    setSelectedInstallationID(
      selectedInstallationID || githubInstallations[0]?.id || "",
    );
    setGitHubPickerOpen(true);
  };

  const handleGitHubAction = async () => {
    if (githubInstallations.length > 0) {
      openGitHubPicker();
      return;
    }
    setConnectingGitHub(true);
    try {
      const response = await api.getGitHubConnectURL(wsId, "repositories");
      if (!response.configured || !response.url) {
        toast.error(t(($) => $.repositories.github_not_configured));
        return;
      }
      window.open(response.url, "_blank", "noopener");
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.repositories.github_connect_failed),
      );
    } finally {
      setConnectingGitHub(false);
    }
  };

  const closeGitHubPicker = () => {
    setGitHubPickerOpen(false);
    setSelectedRepositories(new Map());
    setRepositorySearch("");
  };

  const toggleGitHubRepository = (
    repository: GitHubRepository,
    checked: boolean,
  ) => {
    setSelectedRepositories((current) => {
      const next = new Map(current);
      if (checked) next.set(repository.id, repository);
      else next.delete(repository.id);
      return next;
    });
  };

  const importGitHubRepositories = () => {
    if (!allUrlsValid) {
      toast.error(t(($) => $.repositories.complete_manual_entry_first));
      return;
    }
    const additions: WorkspaceRepo[] = [];
    const known = new Set(existingRepositoryIdentities);
    for (const repository of selectedRepositories.values()) {
      const identity = repositoryIdentity(repository.clone_url);
      if (!identity || known.has(identity) || repository.archived) continue;
      known.add(identity);
      additions.push({
        url: repository.clone_url,
        ...(repository.description?.trim()
          ? { description: repository.description.trim() }
          : {}),
      });
    }
    if (additions.length === 0) {
      closeGitHubPicker();
      return;
    }
    const next = [...repositories, ...additions];
    setRepositories(next);
    autoSave.saveNow(next);
    closeGitHubPicker();
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
            <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3.5">
              <div className="flex flex-wrap items-center gap-2">
                <Button variant="outline" size="sm" onClick={addRepository}>
                  <Plus className="size-3.5" />
                  {t(($) => $.repositories.add)}
                </Button>
                <Button
                  size="sm"
                  onClick={handleGitHubAction}
                  disabled={
                    connectingGitHub ||
                    !githubBrowseConfigured ||
                    (!githubConnectConfigured &&
                      githubInstallations.length === 0)
                  }
                  title={
                    !githubBrowseConfigured
                      ? t(($) => $.repositories.github_browse_not_configured)
                      : undefined
                  }
                >
                  {connectingGitHub ? (
                    <LoaderCircle className="size-3.5 animate-spin" />
                  ) : (
                    <GitHubMark className="size-3.5" />
                  )}
                  {githubInstallations.length > 0
                    ? t(($) => $.repositories.choose_from_github)
                    : t(($) => $.repositories.connect_github)}
                </Button>
              </div>
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

      <Dialog
        open={githubPickerOpen}
        onOpenChange={(open) => {
          if (!open) closeGitHubPicker();
        }}
      >
        <DialogContent className="flex max-h-[85vh] flex-col gap-0 p-0 sm:max-w-2xl">
          <DialogHeader className="border-b px-6 py-5">
            <DialogTitle>
              {t(($) => $.repositories.github_picker_title)}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.repositories.github_picker_description)}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3 px-6 py-4">
            {githubInstallations.length > 1 ? (
              <Select
                items={githubInstallations.map((installation) => ({
                  value: installation.id,
                  label: installation.account_login,
                }))}
                value={selectedInstallationID}
                onValueChange={(value) =>
                  setSelectedInstallationID(value ?? "")
                }
              >
                <SelectTrigger
                  aria-label={t(($) => $.repositories.github_account)}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {githubInstallations.map((installation) => (
                    <SelectItem
                      key={installation.id}
                      value={installation.id}
                    >
                      {installation.account_login}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : githubInstallations[0] ? (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.repositories.github_account)}:{" "}
                <span className="font-medium text-foreground">
                  {githubInstallations[0].account_login}
                </span>
              </p>
            ) : null}

            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={repositorySearch}
                onChange={(event) => setRepositorySearch(event.target.value)}
                placeholder={t(
                  ($) => $.repositories.github_search_placeholder,
                )}
                aria-label={t(
                  ($) => $.repositories.github_search_placeholder,
                )}
                className="pl-8"
              />
            </div>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto border-y">
            {githubRepositoriesQuery.isPending ? (
              <div className="flex items-center justify-center gap-2 px-6 py-12 text-sm text-muted-foreground">
                <LoaderCircle className="size-4 animate-spin" />
                {t(($) => $.repositories.github_loading)}
              </div>
            ) : githubRepositoriesQuery.isError ? (
              <div className="px-6 py-12 text-center text-sm text-muted-foreground">
                {t(($) => $.repositories.github_load_failed)}
              </div>
            ) : filteredGitHubRepositories.length === 0 ? (
              <div className="px-6 py-12 text-center text-sm text-muted-foreground">
                {repositorySearch
                  ? t(($) => $.repositories.github_no_search_results)
                  : t(($) => $.repositories.github_empty)}
              </div>
            ) : (
              <div className="divide-y">
                {filteredGitHubRepositories.map((repository) => {
                  const identity = repositoryIdentity(repository.clone_url);
                  const alreadyAdded =
                    !!identity && existingRepositoryIdentities.has(identity);
                  const disabled = alreadyAdded || repository.archived;
                  return (
                    <label
                      key={repository.id}
                      htmlFor={`github-repository-${repository.id}`}
                      className="flex items-start gap-3 px-6 py-3.5"
                    >
                      <Checkbox
                        id={`github-repository-${repository.id}`}
                        checked={
                          alreadyAdded ||
                          selectedRepositories.has(repository.id)
                        }
                        disabled={disabled}
                        onCheckedChange={(checked) =>
                          toggleGitHubRepository(
                            repository,
                            checked === true,
                          )
                        }
                        className="mt-0.5"
                      />
                      <span className="min-w-0 flex-1 space-y-1">
                        <span className="flex flex-wrap items-center gap-2">
                          <span className="truncate text-sm font-medium">
                            {repository.full_name}
                          </span>
                          {repository.private ? (
                            <Badge variant="secondary">
                              {t(($) => $.repositories.github_private)}
                            </Badge>
                          ) : null}
                          {repository.archived ? (
                            <Badge variant="outline">
                              {t(($) => $.repositories.github_archived)}
                            </Badge>
                          ) : null}
                          {alreadyAdded ? (
                            <Badge variant="outline">
                              {t(($) => $.repositories.github_added)}
                            </Badge>
                          ) : null}
                        </span>
                        {repository.description ? (
                          <span className="block truncate text-xs text-muted-foreground">
                            {repository.description}
                          </span>
                        ) : null}
                      </span>
                    </label>
                  );
                })}
              </div>
            )}

            {githubRepositoriesQuery.hasNextPage ? (
              <div className="flex justify-center border-t p-3">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => githubRepositoriesQuery.fetchNextPage()}
                  disabled={githubRepositoriesQuery.isFetchingNextPage}
                >
                  {githubRepositoriesQuery.isFetchingNextPage
                    ? t(($) => $.repositories.github_loading)
                    : t(($) => $.repositories.github_load_more)}
                </Button>
              </div>
            ) : null}
          </div>

          <DialogFooter className="m-0 border-t bg-muted/30 px-6 py-4">
            <p className="mr-auto text-xs text-muted-foreground">
              {t(($) => $.repositories.github_selected_count, {
                count: selectedRepositories.size,
              })}
            </p>
            <Button variant="ghost" onClick={closeGitHubPicker}>
              {t(($) => $.repositories.github_cancel)}
            </Button>
            <Button
              onClick={importGitHubRepositories}
              disabled={selectedRepositories.size === 0 || !allUrlsValid}
            >
              {t(($) => $.repositories.github_import)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
