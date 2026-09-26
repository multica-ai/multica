"use client";

import { useEffect, useMemo, useState } from "react";
import {
  ChevronDown,
  FolderGit2,
  LoaderCircle,
  MoreHorizontal,
  Pencil,
  Plus,
  Search,
  Trash2,
} from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { toast } from "sonner";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
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
import { SettingsCard, SettingsSection } from "./settings-layout";
import { GitHubMark } from "./github-mark";

const EMPTY_REPOSITORIES: WorkspaceRepo[] = [];

/**
 * Host + path identity of a clone URL, so HTTPS and SSH forms of the same
 * repository compare equal. Path casing is preserved: hosts are
 * case-insensitive, repository paths are not guaranteed to be.
 */
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
    .replace(/\.git$/i, "");
  if (!host || !normalizedPath) return null;
  return `${host.toLowerCase()}/${normalizedPath}`;
}

interface RepositoryDraft {
  /** Index being edited, or null when adding. */
  index: number | null;
  url: string;
  description: string;
}

/**
 * The repositories agents may clone and push to. Rows are read-only; adding
 * and editing happen in a dialog that saves on confirm, so a half-typed URL
 * is never persisted and every change is one deliberate request.
 */
export function RepositoriesSection() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const navigation = useNavigation();
  const { role } = useCurrentMember(wsId);
  const canManageWorkspace = role === "owner" || role === "admin";
  const repositories = workspace?.repos ?? EMPTY_REPOSITORIES;

  const [draft, setDraft] = useState<RepositoryDraft | null>(null);
  const [saving, setSaving] = useState(false);
  const [pendingRemovalIndex, setPendingRemovalIndex] = useState<number | null>(null);
  const [connectingGitHub, setConnectingGitHub] = useState(false);
  const [githubPickerOpen, setGitHubPickerOpen] = useState(false);
  const [selectedInstallationID, setSelectedInstallationID] = useState("");
  const [selectedRepositories, setSelectedRepositories] = useState<
    Map<number, GitHubRepository>
  >(new Map());
  const [repositorySearch, setRepositorySearch] = useState("");

  const {
    data: githubData,
    isPending: githubInstallationsPending,
    isFetching: githubInstallationsFetching,
  } = useQuery({
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

  // The GitHub App install flow returns with `github_connected=1` (or an
  // error). Open the picker once the installation list reflects the new
  // install, then drop the one-shot params so a refresh does not repeat it.
  useEffect(() => {
    const connected = navigation.searchParams.get("github_connected") === "1";
    const githubError = navigation.searchParams.get("github_error");
    if ((!connected && !githubError) || !canManageWorkspace) return;
    if (
      !githubError &&
      (githubInstallationsPending || githubInstallationsFetching)
    ) {
      return;
    }

    if (githubError) {
      toast.error(t(($) => $.repositories.github_connect_failed));
    } else if (githubInstallations.length > 0 && githubBrowseConfigured) {
      setSelectedInstallationID(githubInstallations[0]!.id);
      setGitHubPickerOpen(true);
    } else if (githubInstallations.length > 0) {
      toast.error(t(($) => $.repositories.github_browse_not_configured));
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
    githubInstallationsFetching,
    githubInstallationsPending,
    navigation,
    t,
  ]);

  const persist = async (next: WorkspaceRepo[]) => {
    if (!workspace) return false;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, { repos: next });
      queryClient.setQueryData(
        workspaceKeys.list(),
        (old: Workspace[] | undefined) =>
          old?.map((item) => (item.id === updated.id ? updated : item)),
      );
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

  const draftIdentity = draft ? repositoryIdentity(draft.url) : null;
  const draftDuplicate =
    !!draftIdentity &&
    repositories.some(
      (repository, index) =>
        index !== draft?.index &&
        repositoryIdentity(repository.url) === draftIdentity,
    );

  const saveDraft = async () => {
    if (!draft || !draft.url.trim() || draftDuplicate) return;
    const entry: WorkspaceRepo = {
      url: draft.url.trim(),
      ...(draft.description.trim() ? { description: draft.description.trim() } : {}),
    };
    const next =
      draft.index === null
        ? [...repositories, entry]
        : repositories.map((repository, index) =>
            index === draft.index ? entry : repository,
          );
    if (await persist(next)) setDraft(null);
  };

  const removeRepository = async (index: number) => {
    const ok = await persist(
      repositories.filter((_, repoIndex) => repoIndex !== index),
    );
    if (ok) setPendingRemovalIndex(null);
  };

  const openGitHubPicker = () => {
    setSelectedInstallationID(
      selectedInstallationID || githubInstallations[0]?.id || "",
    );
    setGitHubPickerOpen(true);
  };

  const connectGitHub = async () => {
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

  const importGitHubRepositories = async () => {
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
    if (await persist([...repositories, ...additions])) closeGitHubPicker();
  };

  if (!workspace) return null;

  const githubItemDisabled =
    connectingGitHub ||
    !githubBrowseConfigured ||
    (!githubConnectConfigured && githubInstallations.length === 0);

  return (
    <SettingsSection
      title={t(($) => $.repositories.section_title)}
      description={t(($) => $.repositories.description)}
      anchor="repositories"
      action={
        canManageWorkspace ? (
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button size="sm" />}>
              <Plus />
              {t(($) => $.repositories.add)}
              <ChevronDown />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-auto min-w-56">
              <DropdownMenuItem
                disabled={githubItemDisabled}
                onClick={() =>
                  githubInstallations.length > 0
                    ? openGitHubPicker()
                    : void connectGitHub()
                }
              >
                {connectingGitHub ? (
                  <LoaderCircle className="animate-spin" />
                ) : (
                  <GitHubMark className="size-4" />
                )}
                <span className="flex flex-col">
                  <span>
                    {githubInstallations.length > 0
                      ? t(($) => $.repositories.choose_from_github)
                      : t(($) => $.repositories.connect_github)}
                  </span>
                  {!githubBrowseConfigured ? (
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.repositories.github_browse_not_configured)}
                    </span>
                  ) : null}
                </span>
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => setDraft({ index: null, url: "", description: "" })}
              >
                <Pencil />
                {t(($) => $.repositories.add_manually)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null
      }
    >
      <SettingsCard>
        {repositories.length === 0 ? (
          <div className="px-4 py-8 text-center text-caption text-muted-foreground">
            {canManageWorkspace
              ? t(($) => $.repositories.empty)
              : t(($) => $.repositories.manage_hint)}
          </div>
        ) : (
          repositories.map((repository, index) => {
            const identity = repositoryIdentity(repository.url);
            const onGitHub = identity?.startsWith("github.com/") ?? false;
            return (
              <div
                key={`${index}:${repository.url}`}
                className="flex min-h-14 items-center gap-3 px-4 py-2.5"
              >
                <span
                  aria-hidden="true"
                  className="flex size-5 shrink-0 items-center justify-center text-muted-foreground"
                >
                  {onGitHub ? (
                    <GitHubMark className="size-4" />
                  ) : (
                    <FolderGit2 className="size-4" />
                  )}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate font-mono text-label" title={repository.url}>
                    {repository.url}
                  </p>
                  {repository.description ? (
                    <p className="mt-0.5 truncate text-caption text-muted-foreground">
                      {repository.description}
                    </p>
                  ) : null}
                </div>
                {canManageWorkspace ? (
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={t(($) => $.repositories.actions_aria, {
                            url: repository.url,
                          })}
                        />
                      }
                    >
                      <MoreHorizontal />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-auto">
                      <DropdownMenuItem
                        onClick={() =>
                          setDraft({
                            index,
                            url: repository.url,
                            description: repository.description ?? "",
                          })
                        }
                      >
                        <Pencil />
                        {t(($) => $.repositories.edit)}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => setPendingRemovalIndex(index)}
                      >
                        <Trash2 />
                        {t(($) => $.repositories.delete_confirm_action)}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                ) : null}
              </div>
            );
          })
        )}
      </SettingsCard>

      <Dialog
        open={draft !== null}
        onOpenChange={(open) => {
          if (!open && !saving) setDraft(null);
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {draft?.index === null
                ? t(($) => $.repositories.add_title)
                : t(($) => $.repositories.edit_title)}
            </DialogTitle>
            <DialogDescription>{t(($) => $.repositories.description)}</DialogDescription>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              void saveDraft();
            }}
          >
            <div className="space-y-1.5">
              <Label htmlFor="repository-url">{t(($) => $.repositories.url_label)}</Label>
              <Input
                id="repository-url"
                autoComplete="off"
                spellCheck={false}
                value={draft?.url ?? ""}
                onChange={(event) =>
                  setDraft((current) =>
                    current ? { ...current, url: event.target.value } : current,
                  )
                }
                aria-invalid={draftDuplicate || undefined}
                placeholder={t(($) => $.repositories.url_placeholder)}
                className="font-mono text-caption"
              />
              {draftDuplicate ? (
                <p className="text-caption text-destructive">
                  {t(($) => $.repositories.duplicate)}
                </p>
              ) : null}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="repository-description">
                {t(($) => $.repositories.description_label)}
              </Label>
              <Input
                id="repository-description"
                autoComplete="off"
                value={draft?.description ?? ""}
                onChange={(event) =>
                  setDraft((current) =>
                    current ? { ...current, description: event.target.value } : current,
                  )
                }
                placeholder={t(($) => $.repositories.description_placeholder)}
              />
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setDraft(null)}
                disabled={saving}
              >
                {t(($) => $.repositories.github_cancel)}
              </Button>
              <Button
                type="submit"
                disabled={saving || !draft?.url.trim() || draftDuplicate}
                aria-busy={saving || undefined}
              >
                {saving ? t(($) => $.repositories.saving) : t(($) => $.repositories.save)}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={githubPickerOpen}
        onOpenChange={(open) => {
          if (!open && !saving) closeGitHubPicker();
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
              <p className="text-caption text-muted-foreground">
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
              <div className="flex items-center justify-center gap-2 px-6 py-12 text-body text-muted-foreground">
                <LoaderCircle className="size-4 animate-spin" />
                {t(($) => $.repositories.github_loading)}
              </div>
            ) : githubRepositoriesQuery.isError ? (
              <div className="px-6 py-12 text-center text-body text-muted-foreground">
                {t(($) => $.repositories.github_load_failed)}
              </div>
            ) : filteredGitHubRepositories.length === 0 ? (
              <div className="px-6 py-12 text-center text-body text-muted-foreground">
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
                          <span className="truncate text-body font-medium">
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
                          <span className="block truncate text-caption text-muted-foreground">
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
            <p className="mr-auto text-caption text-muted-foreground">
              {t(($) => $.repositories.github_selected_count, {
                count: selectedRepositories.size,
              })}
            </p>
            <Button variant="ghost" onClick={closeGitHubPicker} disabled={saving}>
              {t(($) => $.repositories.github_cancel)}
            </Button>
            <Button
              onClick={() => void importGitHubRepositories()}
              disabled={selectedRepositories.size === 0 || saving}
              aria-busy={saving || undefined}
            >
              {t(($) => $.repositories.github_import)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
              onClick={() => {
                if (pendingRemovalIndex !== null) {
                  void removeRepository(pendingRemovalIndex);
                }
              }}
            >
              {t(($) => $.repositories.delete_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}
