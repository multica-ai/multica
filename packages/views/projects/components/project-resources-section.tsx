"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  FolderGit,
  FolderOpen,
  GitBranch,
  Plus,
  Search,
  Trash2,
} from "lucide-react";
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
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
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
import {
  isDesktopShell,
  pickDirectory,
  useLocalDaemonStatus,
  validateLocalDirectory,
  type ValidateLocalDirectoryResult,
} from "../../platform";
import {
  LocalDirectoryModeDialog,
  type WorktreeUnavailableReason,
} from "./local-directory-mode-dialog";
import { GithubRefDialog } from "./github-ref-dialog";
import { GithubRefField, githubRefHasError } from "./github-ref-field";
import { localDirectoryLabel } from "./local-directory-label";
import { useT } from "../../i18n";
import { githubShortLabel } from "../../common/github-url";

// Project Resources sidebar section.
//
// Type-dispatched at the row + add-flow level. Add a new resource_type by:
//   (1) extending the server validator
//   (2) extending ProjectResourceType in @multica/core/types
//   (3) adding a render case in ResourceRow and an add-control here
function isGithubRef(r: ProjectResource): r is ProjectResource & {
  resource_ref: GithubRepoResourceRef;
} {
  return r.resource_type === "github_repo";
}

function isLocalDirectoryRef(r: ProjectResource): r is ProjectResource & {
  resource_ref: LocalDirectoryResourceRef;
} {
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

/** Pending mode edit — either for a directory being added, or an existing row. */
type ModeDialogState = {
  path: string;
  daemonId: string | null;
  mode: LocalDirectoryExecutionMode;
  /** undefined = unknown (older desktop build); treated as "cannot verify". */
  isGitRepo: boolean | undefined;
  /** Set for an edit; absent when adding a new resource. */
  resource?: ProjectResource & { resource_ref: LocalDirectoryResourceRef };
  /** Only used when adding. */
  label?: string;
};

type RefDialogState = {
  resource: ProjectResource & { resource_ref: GithubRepoResourceRef };
};

export function ProjectResourcesSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const daemonStatus = useLocalDaemonStatus();
  const [open, setOpen] = useState(true);
  const [addOpen, setAddOpen] = useState(false);
  const [repoSearch, setRepoSearch] = useState("");
  const [picking, setPicking] = useState(false);
  const [modeDialog, setModeDialog] = useState<ModeDialogState | null>(null);
  const [modeSaving, setModeSaving] = useState(false);
  const [modeError, setModeError] = useState<string | null>(null);
  const [refDialog, setRefDialog] = useState<RefDialogState | null>(null);
  const [refSaving, setRefSaving] = useState(false);
  const [refError, setRefError] = useState<string | null>(null);

  const { data: resources = [] } = useQuery(
    projectResourcesOptions(wsId, projectId),
  );
  const createResource = useCreateProjectResource(wsId, projectId);
  const updateResource = useUpdateProjectResource(wsId, projectId);
  const deleteResource = useDeleteProjectResource(wsId, projectId);

  // Desktop-only entry points. We hide (not just disable) on web so users
  // there don't see an action they can never complete — the spec calls for
  // read-only on web because the daemon-id check can't be performed in the
  // browser.
  const desktopMode = isDesktopShell();
  const localDaemonId = daemonStatus.daemonId;

  // Only ever used to decide what to PRESELECT. Whether the machine can run
  // worktree mode is the server's call — it knows its own version, the client
  // would have to infer it from data the server wrote, and that inference is
  // what told a user on the newest release to upgrade it (#7113). The save is
  // gated server-side and surfaced here as an inline error instead.
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  // The one thing the client must still check up front: whether this server
  // performs that gate at all. One declared boolean, no inference — servers
  // that predate it drop execution_mode and answer 201.
  const serverValidatesWorktree = useConfigStore((state) => state.localWorktreeSupported);
  // Keyed on the resource's OWN daemon, not the machine the browser happens to
  // be on: a resource is pinned to one machine, and its mode can legitimately
  // be changed from the web app or from a different device. Using the local
  // daemon here would report "too old" for every resource whenever the viewer
  // is not on that machine.
  // Capability, not version, and judged by the daemon's newest runtime row —
  // see runtimeAdvertisesLocalWorktree for why an any-match would keep saying
  // yes after a downgrade.
  const advertisesWorktree = (daemonId: string | null) =>
    runtimeAdvertisesLocalWorktree(runtimes, daemonId);

  const attachedUrls = new Set(
    resources.filter(isGithubRef).map((r) => r.resource_ref.url),
  );
  const attachedLocalPaths = new Set(
    resources
      .filter(isLocalDirectoryRef)
      .filter((r) => r.resource_ref.daemon_id === localDaemonId)
      .map((r) => r.resource_ref.local_path),
  );
  // Per (project, daemon) we allow at most one local_directory — the
  // daemon-side resolver picks the first match by daemon_id, so two rows
  // on the same daemon would silently route the agent into one of them.
  // The server enforces this at the API boundary; the UI mirrors the
  // restriction by hiding the "Add" affordance once a row exists for the
  // current daemon, otherwise users would only discover the limit on a
  // 409 toast.
  const hasLocalDirectoryForCurrentDaemon =
    localDaemonId !== null && attachedLocalPaths.size > 0;

  const repoQuery = repoSearch.trim().toLowerCase();
  const filteredRepos =
    workspace?.repos?.filter((repo) => repo.url.toLowerCase().includes(repoQuery)) ?? [];

  const handleAttach = async (url: string, ref?: string) => {
    try {
      await createResource.mutateAsync({
        resource_type: "github_repo",
        // Omit the key entirely when empty rather than sending "": an absent
        // ref is what "use the default branch" looks like on the wire.
        resource_ref: ref ? { url, ref } : { url },
      });
      toast.success(t(($) => $.resources.toast_attached));
    } catch (err) {
      const msg = err instanceof Error ? err.message : t(($) => $.resources.toast_attach_failed);
      toast.error(msg);
    }
  };

  const handleAttachLocalDirectory = async () => {
    if (picking) return;
    setPicking(true);
    try {
      if (!localDaemonId || !daemonStatus.running) {
        toast.error(t(($) => $.resources.toast_local_daemon_not_running));
        return;
      }
      // Race guard: the button gates on this already, but if the picker
      // is opened while a concurrent resource-create lands the user
      // would otherwise see a 409. Surface a clearer message instead.
      if (attachedLocalPaths.size > 0) {
        toast.error(t(($) => $.resources.toast_local_daemon_already_attached));
        return;
      }
      const picked = await pickDirectory();
      if (!picked.ok) {
        if (picked.reason && picked.reason !== "cancelled") {
          toast.error(
            picked.error ?? t(($) => $.resources.toast_local_pick_failed),
          );
        }
        return;
      }
      const path = picked.path ?? "";
      const fallbackLabel = picked.basename ?? path;
      if (attachedLocalPaths.has(path)) {
        toast.error(t(($) => $.resources.toast_local_already_attached));
        return;
      }
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
      // Ask for the execution mode before creating. It is part of what the
      // user is choosing — whether tasks edit this folder or hand back a
      // branch — not a setting to discover afterwards.
      setModeError(null);
      setModeDialog({
        path,
        daemonId: localDaemonId,
        // Same preselection rule as the create-project flow: a git repo this
        // daemon can actually run worktree mode on starts on parallel, anything
        // else starts on direct. Only the PRESELECTION differs by folder — the
        // user still confirms, and existing resources keep whatever they have.
        mode:
          validation.is_git_repo === true &&
          serverValidatesWorktree &&
          advertisesWorktree(localDaemonId)
            ? "worktree"
            : "in_place",
        isGitRepo: validation.is_git_repo,
        label: fallbackLabel,
      });
      setAddOpen(false);
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : t(($) => $.resources.toast_local_pick_failed);
      toast.error(msg);
    } finally {
      setPicking(false);
    }
  };

  const handleConfirmMode = async (mode: LocalDirectoryExecutionMode) => {
    if (!modeDialog || modeSaving) return;
    setModeSaving(true);
    setModeError(null);
    try {
      if (modeDialog.resource) {
        const ref = modeDialog.resource.resource_ref;
        if (executionModeOf(ref) === mode) {
          setModeDialog(null);
          return;
        }
        await updateResource.mutateAsync({
          resourceId: modeDialog.resource.id,
          data: {
            // Spread first so every other ref field survives the edit — the
            // server replaces the whole ref, it does not deep-merge.
            resource_ref: { ...ref, execution_mode: mode },
          },
        });
        toast.success(t(($) => $.resources.toast_local_mode_updated));
      } else {
        if (!localDaemonId) return;
        await createResource.mutateAsync({
          resource_type: "local_directory",
          resource_ref: {
            local_path: modeDialog.path,
            daemon_id: localDaemonId,
            label: modeDialog.label ?? modeDialog.path,
            execution_mode: mode,
          },
        });
        toast.success(t(($) => $.resources.toast_local_attached));
      }
      setModeDialog(null);
    } catch (err) {
      // Keep the dialog open and show the reason inline: the most likely
      // failure is the server's daemon-version gate, and closing the dialog
      // would leave the user with a toast and no way to act on it.
      setModeError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.resources.toast_local_mode_update_failed),
      );
    } finally {
      setModeSaving(false);
    }
  };

  const handleSaveRef = async (nextRef: string) => {
    if (!refDialog || refSaving) return;
    const ref = refDialog.resource.resource_ref;
    if ((ref.ref ?? "") === nextRef) {
      setRefDialog(null);
      return;
    }
    setRefSaving(true);
    setRefError(null);
    try {
      await updateResource.mutateAsync({
        resourceId: refDialog.resource.id,
        data: {
          // Spread first so the url and any other ref field survive the edit —
          // the server replaces the whole ref, it does not deep-merge. Clearing
          // drops the key, which is how the default branch is restored.
          resource_ref: nextRef
            ? { ...ref, ref: nextRef }
            : { ...ref, ref: undefined },
        },
      });
      toast.success(
        nextRef
          ? t(($) => $.resources.toast_ref_updated)
          : t(($) => $.resources.toast_ref_cleared),
      );
      setRefDialog(null);
    } catch (err) {
      // Keep the dialog open so the rejected value is still there to fix.
      setRefError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.resources.toast_ref_update_failed),
      );
    } finally {
      setRefSaving(false);
    }
  };

  const handleRemove = async (resource: ProjectResource) => {
    try {
      await deleteResource.mutateAsync(resource.id);
      toast.success(t(($) => $.resources.toast_removed));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.resources.toast_remove_failed),
      );
    }
  };

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
        <div className="pl-2 space-y-1.5">
          {resources.length === 0 && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.resources.empty)}
            </p>
          )}
          {resources.length > 0 && (
            <div className="max-h-64 space-y-1.5 overflow-y-auto pr-1">
              {resources.map((resource) => (
                <ResourceRow
                  key={resource.id}
                  resource={resource}
                  localDaemonId={localDaemonId}
                  onRemove={() => handleRemove(resource)}
                  onEditGithubRef={(target) => {
                    setRefError(null);
                    setRefDialog({ resource: target });
                  }}
                  onEditLocalDirectoryMode={(target) => {
                    setModeError(null);
                    setModeDialog({
                      path: target.resource_ref.local_path,
                      daemonId: target.resource_ref.daemon_id,
                      mode: executionModeOf(target.resource_ref),
                      // The path is already saved, so there is nothing to
                      // re-validate from the browser; the desktop check only
                      // runs at pick time. Unknown means the option stays
                      // available and the daemon has the final say.
                      isGitRepo: undefined,
                      resource: target,
                    });
                  }}
                />
              ))}
            </div>
          )}
          <Popover
            open={addOpen}
            onOpenChange={(v) => {
              setAddOpen(v);
              if (!v) setRepoSearch("");
            }}
          >
            <PopoverTrigger
              render={
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-caption text-muted-foreground hover:text-foreground"
                >
                  <Plus className="size-3" />
                  {t(($) => $.resources.add_button)}
                </Button>
              }
            />
            <PopoverContent align="start" className="w-72 p-2 space-y-2">
              <div className="text-caption font-medium text-muted-foreground">
                {t(($) => $.resources.popover_title)}
              </div>
              {workspace?.repos && workspace.repos.length > 0 && (
                <>
                  <div className="relative">
                    <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                    <input
                      type="text"
                      value={repoSearch}
                      onChange={(e) => setRepoSearch(e.target.value)}
                      aria-label={t(($) => $.resources.repos_search_placeholder)}
                      placeholder={t(($) => $.resources.repos_search_placeholder)}
                      className="h-8 w-full rounded-md border bg-transparent pl-7 pr-2 text-caption outline-none placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring"
                    />
                  </div>
                  <div className="max-h-48 space-y-1 overflow-y-auto">
                    {filteredRepos.length === 0 && repoQuery && (
                      <p className="py-2 text-center text-caption text-muted-foreground">
                        {t(($) => $.resources.repos_search_empty)}
                      </p>
                    )}
                    {filteredRepos.map((repo) => {
                      const isAttached = attachedUrls.has(repo.url);
                      const isDisabled = isAttached || createResource.isPending;
                      return (
                        // Use aria-disabled instead of the native `disabled` attribute so
                        // hover events still reach the tooltip trigger on attached rows
                        // (browsers suppress pointer events on disabled form controls).
                        <button
                          key={repo.url}
                          type="button"
                          aria-disabled={isDisabled}
                          onClick={async () => {
                            if (isDisabled) return;
                            await handleAttach(repo.url);
                            setAddOpen(false);
                          }}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-caption text-left hover:bg-accent transition-colors aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:hover:bg-transparent"
                        >
                          <FolderGit className="size-3.5" />
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <span className="truncate flex-1">{githubShortLabel(repo.url)}</span>
                              }
                            />
                            <TooltipContent side="top">{repo.url}</TooltipContent>
                          </Tooltip>
                          {isAttached && (
                            <span className="text-micro text-muted-foreground">
                              {t(($) => $.resources.attached_badge)}
                            </span>
                          )}
                        </button>
                      );
                    })}
                  </div>
                </>
              )}
              <CustomRepoForm
                onSubmit={async (url, ref) => {
                  await handleAttach(url, ref);
                  setAddOpen(false);
                }}
              />
            </PopoverContent>
          </Popover>
          {desktopMode && (
            <div className="flex flex-col">
              <Button
                variant="ghost"
                size="sm"
                className="h-7 justify-start px-2 text-caption text-muted-foreground hover:text-foreground"
                disabled={
                  picking ||
                  createResource.isPending ||
                  !daemonStatus.running ||
                  hasLocalDirectoryForCurrentDaemon
                }
                onClick={() => {
                  void handleAttachLocalDirectory();
                }}
              >
                <FolderOpen className="size-3" />
                {t(($) => $.resources.add_local_directory_button)}
              </Button>
              {!daemonStatus.running && (
                <p className="px-2 pt-0.5 text-micro text-muted-foreground">
                  {t(($) => $.resources.local_daemon_offline_hint)}
                </p>
              )}
              {daemonStatus.running && hasLocalDirectoryForCurrentDaemon && (
                <p className="px-2 pt-0.5 text-micro text-muted-foreground">
                  {t(($) => $.resources.local_daemon_already_attached_hint)}
                </p>
              )}
            </div>
          )}
        </div>
      )}
      {refDialog && (
        <GithubRefDialog
          open
          onOpenChange={(next) => {
            if (!next) {
              setRefDialog(null);
              setRefError(null);
            }
          }}
          url={refDialog.resource.resource_ref.url}
          value={refDialog.resource.resource_ref.ref ?? ""}
          errorMessage={refError ?? undefined}
          saving={refSaving}
          onConfirm={(next) => void handleSaveRef(next)}
        />
      )}
      {modeDialog && (
        <LocalDirectoryModeDialog
          open
          onOpenChange={(next) => {
            if (!next) {
              setModeDialog(null);
              setModeError(null);
            }
          }}
          path={modeDialog.path}
          value={modeDialog.mode}
          unavailableReason={worktreeUnavailableReason(
            modeDialog.isGitRepo,
            serverValidatesWorktree,
          )}
          errorMessage={modeError ?? undefined}
          saving={modeSaving}
          confirmLabel={
            modeDialog.resource
              ? t(($) => $.resources.mode_save)
              : t(($) => $.resources.mode_add)
          }
          onConfirm={(mode) => void handleConfirmMode(mode)}
        />
      )}
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

interface ResourceRowProps {
  resource: ProjectResource;
  localDaemonId: string | null;
  onRemove: () => void;
  onEditGithubRef: (
    resource: ProjectResource & { resource_ref: GithubRepoResourceRef },
  ) => void;
  onEditLocalDirectoryMode: (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
  ) => void;
}

function ResourceRow({
  resource,
  localDaemonId,
  onRemove,
  onEditGithubRef,
  onEditLocalDirectoryMode,
}: ResourceRowProps) {
  const { t } = useT("projects");
  if (isGithubRef(resource)) {
    const ref = resource.resource_ref;
    const display = resource.label || githubShortLabel(ref.url);
    const tooltip = ref.ref ? `${ref.url}\nref: ${ref.ref}` : ref.url;
    return (
      <div className="text-caption group">
        <div className="flex items-center gap-2">
          <FolderGit className="size-3.5 text-muted-foreground shrink-0" />
          <Tooltip>
            <TooltipTrigger
              render={
                <a
                  href={ref.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="truncate min-w-0 flex-1 hover:underline"
                >
                  {display}
                </a>
              }
            />
            <TooltipContent side="top" className="whitespace-pre-line">{tooltip}</TooltipContent>
          </Tooltip>
          <button
            type="button"
            onClick={() => onEditGithubRef(resource)}
            className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
            title={t(($) => $.resources.ref_edit_tooltip)}
          >
            <GitBranch className="size-3 text-muted-foreground" />
          </button>
          <button
            type="button"
            onClick={onRemove}
            className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
            title={t(($) => $.resources.remove_tooltip)}
          >
            <Trash2 className="size-3 text-muted-foreground" />
          </button>
        </div>
        {/* Its own line rather than appended to the link text: a custom label
            takes that slot, and this panel is narrow enough that an inline
            badge truncates the repository name away — which is exactly when
            someone needs to read both.
            
            Rendered even when nothing is pinned, showing "Default branch".
            Without it, clearing a branch makes the line vanish, which reads
            the same as the setting never having existed — there is no way to
            confirm from the panel that the repo is deliberately on its
            default, or that this row has a branch setting at all. */}
        <Tooltip>
          <TooltipTrigger
            render={
              <div className="flex items-center gap-1 pl-[1.375rem] text-micro text-muted-foreground">
                <GitBranch className="size-3 shrink-0" />
                <span className="truncate">
                  {ref.ref || t(($) => $.resources.ref_default_label)}
                </span>
              </div>
            }
          />
          <TooltipContent side="top">
            {ref.ref
              ? t(($) => $.resources.ref_badge_tooltip, { ref: ref.ref })
              : t(($) => $.resources.ref_default_tooltip)}
          </TooltipContent>
        </Tooltip>
      </div>
    );
  }

  if (isLocalDirectoryRef(resource)) {
    return (
      <LocalDirectoryRow
        resource={resource}
        localDaemonId={localDaemonId}
        onRemove={onRemove}
        onEditMode={onEditLocalDirectoryMode}
      />
    );
  }

  return (
    <div className="flex items-center gap-2 text-caption text-muted-foreground">
      <span className="truncate flex-1">
        {resource.label || resource.resource_type}
      </span>
      <button
        type="button"
        onClick={onRemove}
        className="rounded-sm p-0.5 hover:bg-accent"
        title={t(($) => $.resources.remove_tooltip)}
      >
        <Trash2 className="size-3" />
      </button>
    </div>
  );
}

interface LocalDirectoryRowProps {
  resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef };
  localDaemonId: string | null;
  onRemove: () => void;
  onEditMode: (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
  ) => void;
}

function LocalDirectoryRow({
  resource,
  localDaemonId,
  onRemove,
  onEditMode,
}: LocalDirectoryRowProps) {
  const { t } = useT("projects");
  const ref = resource.resource_ref;
  const mode = executionModeOf(ref);
  const display = localDirectoryLabel(resource);
  const isForeignDaemon =
    localDaemonId !== null && ref.daemon_id !== localDaemonId;
  const isLocalUnknown = localDaemonId === null;
  // "disabled" in the spec sense — visual de-emphasis + no chat hint. Both
  // actions stay available so the user can drop or reconfigure a stale
  // registration from any device.
  const mismatch = isForeignDaemon || isLocalUnknown;

  return (
    <div
      className={`flex items-center gap-2 text-caption group ${
        mismatch ? "opacity-60" : ""
      }`}
    >
      <FolderOpen className="size-3.5 text-muted-foreground shrink-0" />
      {/* The name is the folder's own (or whatever a label update stored);
          there is deliberately no rename here. A folder is identified by its
          path, and a pencil that only retitled the row read as a broken edit
          action beside the branch and remove controls (MUL-7525). */}
      <Tooltip>
        <TooltipTrigger
          render={<span className="truncate flex-1">{display}</span>}
        />
        <TooltipContent side="top">
          <div className="space-y-0.5 text-micro">
            <div className="font-mono">{ref.local_path}</div>
            {mismatch && (
              <div className="text-muted-foreground">
                {isLocalUnknown
                  ? t(($) => $.resources.local_no_daemon_tooltip)
                  : t(($) => $.resources.local_other_machine_tooltip)}
              </div>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
      {/* Always visible, unlike the hover-only actions: without it there is no
          way to tell whether tasks on this folder edit it directly or hand back
          a branch, which is the first thing someone asks when a task queues (or
          does not). */}
      {mode === "worktree" && (
        <Tooltip>
          <TooltipTrigger
            render={
              <Badge variant="secondary" className="shrink-0 gap-1 font-normal">
                <GitBranch className="size-3" />
                {t(($) => $.resources.mode_badge_worktree)}
              </Badge>
            }
          />
          <TooltipContent side="top">
            {t(($) => $.resources.mode_badge_worktree_tooltip)}
          </TooltipContent>
        </Tooltip>
      )}
      {/* Not gated on `mismatch`: switching the mode only rewrites a field, so
          it works from the web app or another device, unlike the folder
          picker. */}
      <button
        type="button"
        onClick={() => onEditMode(resource)}
        className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
        title={t(($) => $.resources.mode_edit_tooltip)}
      >
        <GitBranch className="size-3 text-muted-foreground" />
      </button>
      <button
        type="button"
        onClick={onRemove}
        className="opacity-0 group-hover:opacity-100 transition-opacity rounded-sm p-0.5 hover:bg-accent"
        title={t(($) => $.resources.remove_tooltip)}
      >
        <Trash2 className="size-3 text-muted-foreground" />
      </button>
    </div>
  );
}

function CustomRepoForm({
  onSubmit,
}: {
  onSubmit: (url: string, ref?: string) => Promise<void> | void;
}) {
  const { t } = useT("projects");
  const [url, setUrl] = useState("");
  const [ref, setRef] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const refInvalid = githubRefHasError(ref);

  // Someone who wants a branch copies it out of the address bar, so a pasted
  // .../tree/<branch> URL is split into its two halves here rather than stored
  // whole as a clone URL that does not exist. The result lands in the visible
  // fields, so a wrong guess is obvious before anything is saved.
  //
  // Normalising the URL is unconditional. Gating it on the branch field being
  // empty meant a second pasted browse URL was stored whole. Whether to
  // overwrite the BRANCH is the separate question, and the pasted pair wins:
  // the branch field only appears once a URL is present, so a value sitting in
  // it came from the previous URL rather than from something typed ahead.
  const handleUrlChange = (next: string) => {
    const split = splitGithubUrlRef(next);
    setUrl(split.url);
    if (split.ref) setRef(split.ref);
  };

  const handle = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmedUrl = url.trim();
    const trimmedRef = ref.trim();
    if (!trimmedUrl || refInvalid) return;
    setSubmitting(true);
    try {
      await onSubmit(trimmedUrl, trimmedRef || undefined);
      setUrl("");
      setRef("");
    } finally {
      setSubmitting(false);
    }
  };
  return (
    <form onSubmit={handle} className="space-y-1.5 pt-1 border-t">
      <div className="flex items-center gap-1.5">
        <input
          type="text"
          value={url}
          onChange={(e) => handleUrlChange(e.target.value)}
          aria-label={t(($) => $.resources.popover_title)}
          placeholder={t(($) => $.resources.url_placeholder)}
          className="flex-1 min-w-0 bg-transparent text-caption px-2 py-1 outline-none placeholder:text-muted-foreground"
        />
        <Button
          type="submit"
          size="sm"
          variant="ghost"
          className="h-6 px-2 text-caption"
          disabled={!url.trim() || refInvalid || submitting}
        >
          {t(($) => $.resources.url_submit)}
        </Button>
      </div>
      {/* Only after a URL is entered: an empty attach form should still read as
          one field, and the default branch is the right answer often enough
          that this must not look like a second required step. */}
      {url.trim() && (
        <GithubRefField id="attach-repo-ref" value={ref} onChange={setRef} />
      )}
    </form>
  );
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
