"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  FolderGit,
  FolderOpen,
  GitBranch,
  Laptop,
  Link2,
  Monitor,
  Pencil,
  Plus,
  Search,
  Trash2,
  TriangleAlert,
} from "@multica/ui/icons";
import { toast } from "sonner";
import {
  projectResourcesOptions,
  useCreateProjectResource,
  useDeleteProjectResource,
  useUpdateProjectResource,
} from "@multica/core/projects";
import { splitGithubUrlRef } from "@multica/core/github";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import type {
  AgentRuntime,
  GithubRepoResourceRef,
  LocalDirectoryExecutionMode,
  LocalDirectoryResourceRef,
  ProjectResource,
} from "@multica/core/types";
import {
  runtimeAdvertisesLocalWorktree,
  runtimeListOptions,
} from "@multica/core/runtimes";
import { useConfigStore } from "@multica/core/config";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import {
  isDesktopShell,
  pickDirectory,
  useLocalDaemonStatus,
  validateLocalDirectory,
  type LocalDaemonStatus,
  type ValidateLocalDirectoryResult,
} from "../../platform";
import {
  LocalDirectoryModeOptions,
  type WorktreeUnavailableReason,
} from "./local-directory-mode-options";
import { GithubRefField, githubRefHasError } from "./github-ref-field";
import { localDirectoryLabel } from "./local-directory-label";
import { buildRuntimeMachines } from "../../runtimes/components/runtime-machines";
import { useT } from "../../i18n";
import { githubShortLabel } from "../../common/github-url";

// Project Resources sidebar section.
//
// Grouped by what a resource applies to: repositories apply to every run on
// every machine, while a local folder only applies to runs on the one machine
// it is pinned to (at most one per machine). Add a new resource_type by:
//   (1) extending the server validator
//   (2) extending ProjectResourceType in @multica/core/types
//   (3) giving it a group here; unknown types fall through to a plain row
type GithubResource = ProjectResource & { resource_ref: GithubRepoResourceRef };
type LocalDirectoryResource = ProjectResource & {
  resource_ref: LocalDirectoryResourceRef;
};

function isGithubRef(r: ProjectResource): r is GithubResource {
  return r.resource_type === "github_repo";
}

function isLocalDirectoryRef(r: ProjectResource): r is LocalDirectoryResource {
  return r.resource_type === "local_directory";
}

/**
 * Reads the execution mode off a stored ref. An absent or unrecognised value is
 * reported as in_place, matching the server: the field is optional, and a mode
 * written by a newer client must not render as anything other than the
 * conservative default here.
 */
function executionModeOf(
  ref: LocalDirectoryResourceRef,
): LocalDirectoryExecutionMode {
  return ref.execution_mode === "worktree" ? "worktree" : "in_place";
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

/**
 * A user-typed clone URL, as opposed to a search for a workspace repository.
 * Accepts the same shapes as the server's isValidGitRepoURL: a URL with a
 * scheme, or the scp-like `[user@]host:path` shorthand — which is also how SSH
 * host aliases (`github-work:team/repo.git`) for multiple accounts are written.
 */
function looksLikeRepoUrl(value: string): boolean {
  const trimmed = value.trim();
  if (trimmed.includes("://")) return true;
  if (/\s/.test(trimmed)) return false;
  const colon = trimmed.indexOf(":");
  if (colon <= 0 || colon === trimmed.length - 1) return false;
  // `@` only separates a user when it comes before the colon, and a host
  // must remain between the two.
  const at = trimmed.indexOf("@");
  if (at >= colon) return false;
  return at + 1 < colon;
}

const chipClassName =
  "inline-flex h-5 max-w-full shrink-0 items-center gap-1 rounded-sm bg-muted px-1.5 text-micro text-muted-foreground transition-colors hover:bg-surface-selected hover:text-foreground data-[popup-open]:bg-surface-selected data-[popup-open]:text-foreground";

export function ProjectResourcesSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const { data: resources = [] } = useQuery(
    projectResourcesOptions(wsId, projectId),
  );

  const repos = resources.filter(isGithubRef);
  const folders = resources.filter(isLocalDirectoryRef);
  const others = resources.filter(
    (r) => !isGithubRef(r) && !isLocalDirectoryRef(r),
  );
  // Picking a folder needs the desktop's daemon_id, so web cannot add one. It
  // still lists existing folders, and changing their mode or removing them
  // only rewrites the resource, which works from anywhere.
  const desktopMode = isDesktopShell();

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.resources.section_header)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="space-y-4 pl-2">
          <RepositoryGroup wsId={wsId} projectId={projectId} repos={repos} />
          {(desktopMode || folders.length > 0) && (
            <LocalFolderGroup
              wsId={wsId}
              projectId={projectId}
              folders={folders}
              desktopMode={desktopMode}
            />
          )}
          {others.map((resource) => (
            <UnknownResourceRow key={resource.id} resource={resource} />
          ))}
        </div>
      )}
    </div>
  );
}

function GroupHeader({
  label,
  action,
}: {
  label: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex h-6 items-center gap-1 text-micro font-medium text-muted-foreground">
      <span className="truncate">{label}</span>
      {action && <span className="ml-auto">{action}</span>}
    </div>
  );
}

function InlineError({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-2.5 py-1.5 text-caption text-destructive">
      <TriangleAlert className="size-3.5 mt-0.5 shrink-0" />
      <span className="min-w-0 break-words">{message}</span>
    </div>
  );
}

function useRemoveResource(resource: ProjectResource) {
  const { t } = useT("projects");
  const deleteResource = useDeleteProjectResource(
    resource.workspace_id,
    resource.project_id,
  );
  const remove = async () => {
    if (deleteResource.isPending) return;
    try {
      await deleteResource.mutateAsync(resource.id);
      toast.success(t(($) => $.resources.toast_removed));
    } catch (err) {
      toast.error(errorMessage(err, t(($) => $.resources.toast_remove_failed)));
    }
  };
  return { remove, removing: deleteResource.isPending };
}

function RemoveButton({
  onRemove,
  removing,
  alwaysVisible = false,
}: {
  onRemove: () => void;
  removing: boolean;
  alwaysVisible?: boolean;
}) {
  const { t } = useT("projects");
  return (
    <TooltipHint label={t(($) => $.resources.remove_tooltip)}>
      <button
        type="button"
        onClick={onRemove}
        disabled={removing}
        aria-busy={removing}
        className={cn(
          "shrink-0 rounded-sm p-0.5 transition-opacity hover:bg-accent",
          !alwaysVisible &&
            "opacity-0 group-hover:opacity-100 focus-visible:opacity-100 disabled:opacity-100",
        )}
        aria-label={t(($) => $.resources.remove_tooltip)}
      >
        {removing ? (
          <Spinner className="size-3" aria-hidden="true" />
        ) : (
          <Trash2 className="size-3 text-muted-foreground" />
        )}
      </button>
    </TooltipHint>
  );
}

// ---------------------------------------------------------------------------
// Repositories

function RepositoryGroup({
  wsId,
  projectId,
  repos,
}: {
  wsId: string;
  projectId: string;
  repos: GithubResource[];
}) {
  const { t } = useT("projects");
  const workspace = useCurrentWorkspace();
  const createResource = useCreateProjectResource(wsId, projectId);
  const [addOpen, setAddOpen] = useState(false);
  const attachedUrls = new Set(repos.map((r) => r.resource_ref.url));

  const handleAttach = async (url: string, ref?: string) => {
    try {
      await createResource.mutateAsync({
        resource_type: "github_repo",
        // Omit the key entirely when empty rather than sending "": an absent
        // ref is what "use the default branch" looks like on the wire.
        resource_ref: ref ? { url, ref } : { url },
      });
      toast.success(t(($) => $.resources.toast_attached));
      setAddOpen(false);
    } catch (err) {
      toast.error(errorMessage(err, t(($) => $.resources.toast_attach_failed)));
    }
  };

  return (
    <div className="space-y-1.5">
      <GroupHeader
        label={t(($) => $.resources.repos_group)}
        action={
          <Popover open={addOpen} onOpenChange={setAddOpen}>
            <PopoverTrigger
              render={
                <IconButton
                  size="icon-xs"
                  label={t(($) => $.resources.repos_add)}
                >
                  <Plus />
                </IconButton>
              }
            />
            <PopoverContent align="end" className="w-72 p-2">
              <AddRepositoryPanel
                workspaceRepos={workspace?.repos ?? []}
                attachedUrls={attachedUrls}
                busy={createResource.isPending}
                onAttach={handleAttach}
              />
            </PopoverContent>
          </Popover>
        }
      />
      {repos.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.resources.repos_empty)}
        </p>
      ) : (
        repos.map((resource) => (
          <GithubRepoRow
            key={resource.id}
            resource={resource}
            description={
              workspace?.repos?.find((repo) => repo.url === resource.resource_ref.url)
                ?.description
            }
          />
        ))
      )}
    </div>
  );
}

/**
 * One field for both ways of adding a repository: typing filters the
 * workspace's repositories, and a pasted clone URL turns the panel into an
 * attach form for that URL.
 */
function AddRepositoryPanel({
  workspaceRepos,
  attachedUrls,
  busy,
  onAttach,
}: {
  workspaceRepos: { url: string; description?: string | null }[];
  attachedUrls: Set<string>;
  busy: boolean;
  onAttach: (url: string, ref?: string) => Promise<void>;
}) {
  const { t } = useT("projects");
  const [query, setQuery] = useState("");
  const [ref, setRef] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const isUrl = looksLikeRepoUrl(query);
  const refInvalid = isUrl && githubRefHasError(ref);

  // Someone who wants a branch copies it out of the address bar, so a pasted
  // .../tree/<branch> URL is split into its two halves rather than stored
  // whole as a clone URL that does not exist. A pasted pair always wins the
  // branch field: it only shows while a URL is present, so a value already in
  // it came from the previous URL rather than from something typed ahead.
  const handleQueryChange = (next: string) => {
    if (!looksLikeRepoUrl(next)) {
      setQuery(next);
      setRef("");
      return;
    }
    const split = splitGithubUrlRef(next);
    setQuery(split.url);
    if (split.ref) setRef(split.ref);
  };

  const submitUrl = async () => {
    const url = query.trim();
    if (!url || refInvalid || submitting) return;
    setSubmitting(true);
    try {
      await onAttach(url, ref.trim() || undefined);
    } finally {
      setSubmitting(false);
    }
  };

  const search = query.trim().toLowerCase();
  const filtered = workspaceRepos.filter((repo) =>
    repo.url.toLowerCase().includes(search),
  );
  const InputIcon = isUrl ? Link2 : Search;

  return (
    <div className="space-y-2">
      <div className="relative">
        <InputIcon className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <input
          type="text"
          value={query}
          autoFocus
          onChange={(e) => handleQueryChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && isUrl) {
              e.preventDefault();
              void submitUrl();
            }
          }}
          spellCheck={false}
          aria-label={t(($) => $.resources.repos_input_placeholder)}
          placeholder={t(($) => $.resources.repos_input_placeholder)}
          className="h-8 w-full rounded-md border bg-transparent pl-7 pr-2 text-caption outline-none placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring"
        />
      </div>
      {isUrl ? (
        <div className="space-y-2">
          {/* The default branch is the right answer often enough that this
              must not read as a second required step — it is prefilled from
              a /tree/ URL and otherwise left empty. */}
          <GithubRefField
            id="attach-repo-ref"
            value={ref}
            onChange={setRef}
            onSubmit={() => void submitUrl()}
          />
          <Button
            size="sm"
            className="w-full"
            disabled={refInvalid || submitting || busy}
            onClick={() => void submitUrl()}
          >
            <Plus />
            <span className="truncate">
              {t(($) => $.resources.repos_add_url, {
                repo: githubShortLabel(query.trim()),
              })}
            </span>
          </Button>
        </div>
      ) : (
        workspaceRepos.length > 0 && (
          <div className="max-h-56 space-y-1 overflow-y-auto">
            {filtered.length === 0 && search && (
              <p className="py-2 text-center text-caption text-muted-foreground">
                {t(($) => $.resources.repos_search_empty)}
              </p>
            )}
            {filtered.map((repo) => {
              const isAttached = attachedUrls.has(repo.url);
              const isDisabled = isAttached || busy;
              return (
                // aria-disabled instead of `disabled` so hover still reaches
                // the tooltip trigger on attached rows (browsers suppress
                // pointer events on disabled form controls).
                <button
                  key={repo.url}
                  type="button"
                  aria-disabled={isDisabled}
                  onClick={() => {
                    if (isDisabled) return;
                    void onAttach(repo.url);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-caption text-left hover:bg-accent transition-colors aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:hover:bg-transparent"
                >
                  <FolderGit className="size-3.5 shrink-0" />
                  <span className="min-w-0 flex-1">
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <span className="block truncate">
                            {githubShortLabel(repo.url)}
                          </span>
                        }
                      />
                      <TooltipContent side="top">{repo.url}</TooltipContent>
                    </Tooltip>
                    {repo.description?.trim() && (
                      <span className="block truncate text-micro text-muted-foreground">
                        {repo.description.trim()}
                      </span>
                    )}
                  </span>
                  {isAttached && (
                    <span className="text-micro text-muted-foreground">
                      {t(($) => $.resources.attached_badge)}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        )
      )}
    </div>
  );
}

function GithubRepoRow({
  resource,
  description,
}: {
  resource: GithubResource;
  description?: string | null;
}) {
  const { remove, removing } = useRemoveResource(resource);
  const ref = resource.resource_ref;
  const display = resource.label || githubShortLabel(ref.url);
  // Workspace repository descriptions are source-of-truth metadata. They stay
  // live here rather than being copied into the resource label, while an
  // explicit resource label remains the user's preferred primary text.
  const liveDescription = resource.label ? undefined : description?.trim();

  return (
    <div className="group text-caption">
      <div className="flex items-center gap-2">
        <FolderGit className="size-3.5 shrink-0 text-muted-foreground" />
        <Tooltip>
          <TooltipTrigger
            render={
              <a
                href={ref.url}
                target="_blank"
                rel="noopener noreferrer"
                className="min-w-0 flex-1 truncate hover:underline"
              >
                {display}
              </a>
            }
          />
          <TooltipContent side="top">{ref.url}</TooltipContent>
        </Tooltip>
        <RemoveButton onRemove={remove} removing={removing} />
      </div>
      {/* The branch gets its own line rather than a badge beside the name: a
          custom label takes the name slot, and this panel is narrow enough
          that an inline badge truncates the repository away. */}
      <div className="flex min-w-0 items-center gap-1.5 pl-[1.375rem] pt-0.5">
        <GithubRefChip resource={resource} />
        {liveDescription && (
          <span className="min-w-0 truncate text-micro text-muted-foreground">
            {liveDescription}
          </span>
        )}
      </div>
    </div>
  );
}

/**
 * The branch a repository's tasks start from, editable in place.
 *
 * Always rendered, showing "Default branch" when nothing is pinned: without
 * it, clearing a branch makes the line vanish, which reads the same as the
 * setting never having existed.
 */
function GithubRefChip({ resource }: { resource: GithubResource }) {
  const { t } = useT("projects");
  const updateResource = useUpdateProjectResource(
    resource.workspace_id,
    resource.project_id,
  );
  const ref = resource.resource_ref;
  const current = ref.ref ?? "";
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(current);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const invalid = githubRefHasError(draft);

  const handleOpenChange = (next: boolean) => {
    if (saving) return;
    setOpen(next);
    if (next) {
      setDraft(current);
      setError(null);
    }
  };

  const save = async () => {
    if (saving || invalid) return;
    const next = draft.trim();
    if (next === current) {
      setOpen(false);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await updateResource.mutateAsync({
        resourceId: resource.id,
        data: {
          // Spread first so the url and any other ref field survive the edit —
          // the server replaces the whole ref, it does not deep-merge. Clearing
          // drops the key, which is how the default branch is restored.
          resource_ref: next ? { ...ref, ref: next } : { ...ref, ref: undefined },
        },
      });
      toast.success(
        next
          ? t(($) => $.resources.toast_ref_updated)
          : t(($) => $.resources.toast_ref_cleared),
      );
      setOpen(false);
    } catch (err) {
      // Stay open so the rejected value is still there to fix.
      setError(errorMessage(err, t(($) => $.resources.toast_ref_update_failed)));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <TooltipHint label={t(($) => $.resources.ref_edit_tooltip)}>
        <PopoverTrigger
          render={
            <button
              type="button"
              className={cn(chipClassName, current && "text-foreground")}
            >
              <GitBranch className="size-3 shrink-0" />
              <span className={cn("max-w-32 truncate", current && "font-mono")}>
                {current || t(($) => $.resources.ref_default_label)}
              </span>
            </button>
          }
        />
      </TooltipHint>
      <PopoverContent align="start" className="w-72 gap-3 p-3">
        <GithubRefField
          value={draft}
          onChange={setDraft}
          onSubmit={() => void save()}
          autoFocus
        />
        {/* The ref is read when a task is claimed, so this is the one thing
            the change does NOT do — and the first thing people worry about. */}
        <p className="text-micro text-muted-foreground">
          {t(($) => $.resources.ref_running_note)}
        </p>
        {error && <InlineError message={error} />}
        <div className="flex justify-end gap-1.5">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setOpen(false)}
            disabled={saving}
          >
            {t(($) => $.resources.ref_cancel)}
          </Button>
          <Button size="sm" onClick={() => void save()} disabled={saving || invalid}>
            {t(($) => $.resources.ref_save)}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

// ---------------------------------------------------------------------------
// Local folders

function LocalFolderGroup({
  wsId,
  projectId,
  folders,
  desktopMode,
}: {
  wsId: string;
  projectId: string;
  folders: LocalDirectoryResource[];
  desktopMode: boolean;
}) {
  const { t } = useT("projects");
  const daemonStatus = useLocalDaemonStatus();
  const localDaemonId = daemonStatus.daemonId;
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  // Whether this server checks execution_mode on save at all. One declared
  // boolean, no inference — servers that predate it drop execution_mode and
  // answer 201.
  const serverValidatesWorktree = useConfigStore(
    (state) => state.localWorktreeSupported,
  );

  // Machine names and liveness come from the runtimes each daemon registers,
  // titled the same way as the runtimes page.
  const machines = useMemo(
    () =>
      buildRuntimeMachines(runtimes, {
        now: Date.now(),
        localDaemonId,
        localMachineName: daemonStatus.deviceName,
      }),
    [runtimes, localDaemonId, daemonStatus.deviceName],
  );

  const foldersByDaemon = new Map<string, LocalDirectoryResource[]>();
  for (const folder of folders) {
    const id = folder.resource_ref.daemon_id;
    foldersByDaemon.set(id, [...(foldersByDaemon.get(id) ?? []), folder]);
  }
  const currentFolders = localDaemonId
    ? (foldersByDaemon.get(localDaemonId) ?? [])
    : [];
  const otherDaemonIds = [...foldersByDaemon.keys()].filter(
    (id) => id !== localDaemonId,
  );
  const currentMachine = localDaemonId
    ? machines.find((m) => m.daemonId === localDaemonId)
    : undefined;
  const currentTitle = currentMachine?.title ?? daemonStatus.deviceName;

  return (
    <div className="space-y-1.5">
      <GroupHeader label={t(($) => $.resources.local_group)} />
      <p className="-mt-1 text-micro text-muted-foreground">
        {t(($) => $.resources.local_group_hint)}
      </p>
      {desktopMode && (
        <MachineBlock
          current
          title={currentTitle ?? t(($) => $.resources.local_this_computer)}
          showCurrentTag={!!currentTitle}
        >
          {currentFolders.length > 0 ? (
            currentFolders.map((folder) => (
              <LocalFolderRow
                key={folder.id}
                resource={folder}
                current
                serverValidatesWorktree={serverValidatesWorktree}
              />
            ))
          ) : (
            <LocalFolderSlot
              wsId={wsId}
              projectId={projectId}
              daemonStatus={daemonStatus}
              runtimes={runtimes}
              serverValidatesWorktree={serverValidatesWorktree}
            />
          )}
        </MachineBlock>
      )}
      {otherDaemonIds.map((daemonId) => {
        const machine = machines.find((m) => m.daemonId === daemonId);
        return (
          <MachineBlock
            key={daemonId}
            title={machine?.title ?? t(($) => $.resources.local_unknown_computer)}
            offline={machine !== undefined && machine.health !== "online"}
          >
            {(foldersByDaemon.get(daemonId) ?? []).map((folder) => (
              <LocalFolderRow
                key={folder.id}
                resource={folder}
                current={false}
                serverValidatesWorktree={serverValidatesWorktree}
              />
            ))}
          </MachineBlock>
        );
      })}
    </div>
  );
}

function MachineBlock({
  title,
  current = false,
  showCurrentTag = false,
  offline = false,
  children,
}: {
  title: string;
  current?: boolean;
  showCurrentTag?: boolean;
  offline?: boolean;
  children: React.ReactNode;
}) {
  const { t } = useT("projects");
  const Icon = current ? Laptop : Monitor;
  return (
    <div className="space-y-1">
      <div className="flex min-w-0 items-center gap-1.5 text-micro text-muted-foreground">
        <Icon className="size-3 shrink-0" />
        <span className={cn("truncate", current && "text-foreground")}>{title}</span>
        {showCurrentTag && (
          <span className="shrink-0">{t(($) => $.resources.local_this_computer)}</span>
        )}
        {offline && (
          <span className="shrink-0">· {t(($) => $.resources.local_offline)}</span>
        )}
      </div>
      <div className="space-y-1.5 pl-[1.125rem]">{children}</div>
    </div>
  );
}

function LocalFolderRow({
  resource,
  current,
  serverValidatesWorktree,
}: {
  resource: LocalDirectoryResource;
  current: boolean;
  serverValidatesWorktree: boolean;
}) {
  const { remove, removing } = useRemoveResource(resource);
  // The name is the folder's own (or whatever a label update stored); there is
  // deliberately no rename. A folder is identified by its path (MUL-7525).
  const display = localDirectoryLabel(resource);

  return (
    // Folders on other machines are de-emphasised but stay editable: dropping
    // or reconfiguring a stale registration only rewrites the resource, which
    // works from any device.
    <div className={cn("group text-caption", !current && "opacity-60")}>
      <div className="flex items-center gap-2">
        <FolderOpen className="size-3.5 shrink-0 text-muted-foreground" />
        <Tooltip>
          <TooltipTrigger
            render={<span className="min-w-0 flex-1 truncate">{display}</span>}
          />
          <TooltipContent side="top">
            <span className="font-mono text-micro">{resource.resource_ref.local_path}</span>
          </TooltipContent>
        </Tooltip>
        <RemoveButton onRemove={remove} removing={removing} />
      </div>
      <div className="pl-[1.375rem] pt-0.5">
        <ExecutionModeChip
          resource={resource}
          serverValidatesWorktree={serverValidatesWorktree}
        />
      </div>
    </div>
  );
}

/**
 * How runs use a folder, always visible — "direct" included — because it is
 * the first thing someone asks when a task queues (or does not). Picking an
 * option saves it; a server rejection stays inline so the choice can be fixed.
 */
function ExecutionModeChip({
  resource,
  serverValidatesWorktree,
}: {
  resource: LocalDirectoryResource;
  serverValidatesWorktree: boolean;
}) {
  const { t } = useT("projects");
  const updateResource = useUpdateProjectResource(
    resource.workspace_id,
    resource.project_id,
  );
  const ref = resource.resource_ref;
  const mode = executionModeOf(ref);
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleOpenChange = (next: boolean) => {
    if (saving) return;
    setOpen(next);
    if (next) setError(null);
  };

  const choose = async (next: LocalDirectoryExecutionMode) => {
    if (saving) return;
    if (next === mode) {
      setOpen(false);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await updateResource.mutateAsync({
        resourceId: resource.id,
        // Spread first so every other ref field survives the edit — the server
        // replaces the whole ref, it does not deep-merge.
        data: { resource_ref: { ...ref, execution_mode: next } },
      });
      toast.success(t(($) => $.resources.toast_local_mode_updated));
      setOpen(false);
    } catch (err) {
      // The most likely failure is the server's daemon-version gate; closing
      // would leave a toast and no way to act on it.
      setError(errorMessage(err, t(($) => $.resources.toast_local_mode_update_failed)));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <TooltipHint label={t(($) => $.resources.mode_edit_tooltip)}>
        <PopoverTrigger
          render={
            <button type="button" className={cn(chipClassName, "text-foreground")}>
              {mode === "worktree" ? (
                <GitBranch className="size-3 shrink-0" />
              ) : (
                <Pencil className="size-3 shrink-0" />
              )}
              {mode === "worktree"
                ? t(($) => $.resources.mode_badge_worktree)
                : t(($) => $.resources.mode_badge_in_place)}
            </button>
          }
        />
      </TooltipHint>
      <PopoverContent align="start" className="w-80 gap-2 p-2">
        {/* The path is already saved, so there is nothing to re-validate from
            the browser; unknown git status keeps the option available and the
            daemon has the final say. */}
        <LocalDirectoryModeOptions
          value={mode}
          onChange={(next) => void choose(next)}
          unavailableReason={worktreeUnavailableReason(undefined, serverValidatesWorktree)}
        />
        {error && <InlineError message={error} />}
      </PopoverContent>
    </Popover>
  );
}

type PendingFolder = {
  path: string;
  label: string;
  mode: LocalDirectoryExecutionMode;
  /** undefined = unknown (older desktop build); treated as "cannot verify". */
  isGitRepo: boolean | undefined;
};

/**
 * This machine's place in the group while it has no folder. Choosing one asks
 * for the execution mode before anything is created: it is part of what the
 * user is choosing — whether tasks edit this folder or hand back a branch —
 * not a setting to discover afterwards.
 */
function LocalFolderSlot({
  wsId,
  projectId,
  daemonStatus,
  runtimes,
  serverValidatesWorktree,
}: {
  wsId: string;
  projectId: string;
  daemonStatus: LocalDaemonStatus;
  runtimes: AgentRuntime[];
  serverValidatesWorktree: boolean;
}) {
  const { t } = useT("projects");
  const createResource = useCreateProjectResource(wsId, projectId);
  const [picking, setPicking] = useState(false);
  const [pending, setPending] = useState<PendingFolder | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const daemonId = daemonStatus.daemonId;
  const ready = daemonStatus.running && daemonId !== null;

  const pick = async () => {
    if (picking || !ready) return;
    setPicking(true);
    try {
      const picked = await pickDirectory();
      if (!picked.ok) {
        if (picked.reason && picked.reason !== "cancelled") {
          toast.error(picked.error ?? t(($) => $.resources.toast_local_pick_failed));
        }
        return;
      }
      const path = picked.path ?? "";
      const validation = await validateLocalDirectory(path);
      if (!validation.ok) {
        toast.error(
          localValidationMessage(validation, {
            not_absolute: t(($) => $.resources.local_validate_not_absolute),
            not_found: t(($) => $.resources.local_validate_not_found),
            not_a_directory: t(($) => $.resources.local_validate_not_a_directory),
            not_readable: t(($) => $.resources.local_validate_not_readable),
            not_writable: t(($) => $.resources.local_validate_not_writable),
            unsupported: t(($) => $.resources.local_validate_unsupported),
            fallback: t(($) => $.resources.toast_local_pick_failed),
          }),
        );
        return;
      }
      setError(null);
      setPending({
        path,
        label: picked.basename ?? path,
        // Same preselection rule as the create-project flow: a git repo this
        // daemon can actually run worktree mode on starts on parallel,
        // anything else starts on direct. The user still confirms.
        mode:
          validation.is_git_repo === true &&
          serverValidatesWorktree &&
          runtimeAdvertisesLocalWorktree(runtimes, daemonId)
            ? "worktree"
            : "in_place",
        isGitRepo: validation.is_git_repo,
      });
    } catch (err) {
      toast.error(errorMessage(err, t(($) => $.resources.toast_local_pick_failed)));
    } finally {
      setPicking(false);
    }
  };

  const add = async () => {
    if (!pending || !daemonId || saving) return;
    setSaving(true);
    setError(null);
    try {
      await createResource.mutateAsync({
        resource_type: "local_directory",
        resource_ref: {
          local_path: pending.path,
          daemon_id: daemonId,
          label: pending.label,
          execution_mode: pending.mode,
        },
      });
      toast.success(t(($) => $.resources.toast_local_attached));
      setPending(null);
    } catch (err) {
      // Keep the choice on screen: the likely failures (the one-per-machine
      // conflict, the daemon-version gate) are things the user acts on here.
      setError(errorMessage(err, t(($) => $.resources.toast_attach_failed)));
    } finally {
      setSaving(false);
    }
  };

  if (pending) {
    return (
      <div className="space-y-2 rounded-md border p-2">
        <div className="flex items-start gap-2 text-caption">
          <FolderOpen className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium">{pending.label}</div>
            <div className="break-all font-mono text-micro text-muted-foreground">
              {pending.path}
            </div>
          </div>
        </div>
        <LocalDirectoryModeOptions
          value={pending.mode}
          onChange={(mode) => setPending({ ...pending, mode })}
          unavailableReason={worktreeUnavailableReason(
            pending.isGitRepo,
            serverValidatesWorktree,
          )}
        />
        {error && <InlineError message={error} />}
        <div className="flex justify-end gap-1.5">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setPending(null)}
            disabled={saving}
          >
            {t(($) => $.resources.mode_cancel)}
          </Button>
          <Button size="sm" onClick={() => void add()} disabled={saving}>
            {t(($) => $.resources.mode_add)}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-2 rounded-md border border-dashed px-2.5 py-2">
      <p className="text-micro text-muted-foreground">
        {ready
          ? t(($) => $.resources.local_slot_description)
          : t(($) => $.resources.local_daemon_offline_hint)}
      </p>
      <Button
        variant="outline"
        size="sm"
        className="h-7 text-caption"
        disabled={!ready || picking}
        onClick={() => void pick()}
      >
        <FolderOpen className="size-3" />
        {t(($) => $.resources.local_pick)}
      </Button>
    </div>
  );
}

function UnknownResourceRow({ resource }: { resource: ProjectResource }) {
  const { remove, removing } = useRemoveResource(resource);
  return (
    <div className="flex items-center gap-2 text-caption text-muted-foreground">
      <span className="truncate flex-1">
        {resource.label || resource.resource_type}
      </span>
      <RemoveButton onRemove={remove} removing={removing} alwaysVisible />
    </div>
  );
}

/**
 * Which blocker (if any) applies to the worktree option.
 *
 * `isGitRepo === false` is a hard no — the daemon would fail every task on that
 * folder. `undefined` means we could not check (an older desktop build, or an
 * existing row whose path was validated at pick time), and is deliberately
 * permissive: the daemon re-checks authoritatively, so guessing "not a repo"
 * here would block a perfectly valid setup.
 *
 * Daemon capability is deliberately absent. It is the server's question, asked
 * on save; predicting it here is what produced an unfixable blocker for a user
 * already on the newest release (#7113). Deferring to the server does require
 * knowing it will answer, though — `serverValidates` is the server saying so.
 */
function worktreeUnavailableReason(
  isGitRepo: boolean | undefined,
  serverValidates: boolean,
): WorktreeUnavailableReason | undefined {
  if (isGitRepo === false) return "not_git";
  if (!serverValidates) return "server_outdated";
  return undefined;
}

function localValidationMessage(
  result: ValidateLocalDirectoryResult,
  strings: {
    not_absolute: string;
    not_found: string;
    not_a_directory: string;
    not_readable: string;
    not_writable: string;
    unsupported: string;
    fallback: string;
  },
): string {
  switch (result.reason) {
    case "not_absolute":
      return strings.not_absolute;
    case "not_found":
      return strings.not_found;
    case "not_a_directory":
      return strings.not_a_directory;
    case "not_readable":
      return strings.not_readable;
    case "not_writable":
      return strings.not_writable;
    case "unsupported":
      return strings.unsupported;
    case "error":
    default:
      return result.error ?? strings.fallback;
  }
}
