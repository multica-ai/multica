"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Copy,
  FolderGit,
  FolderOpen,
  GitBranch,
  Laptop,
  Link2,
  Monitor,
  MoreHorizontal,
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
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
import { copyText } from "@multica/ui/lib/clipboard";
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
import {
  buildRuntimeMachines,
  type RuntimeMachine,
} from "../../runtimes/components/runtime-machines";
import { useT } from "../../i18n";
import { githubShortLabel } from "../../common/github-url";
import {
  ObjectSection,
  ObjectSectionAction,
  ObjectSectionCard,
  ObjectSectionEmpty,
} from "../../layout/object-page";

// Project Resources section of the project page.
//
// One list, one Add menu. Repositories apply to every run on every machine,
// while a local folder only applies to runs on the one machine it is pinned to
// (at most one per machine), so a folder row names its machine. Add a new
// resource_type by:
//   (1) extending the server validator
//   (2) extending ProjectResourceType in @multica/core/types
//   (3) giving it a row and an Add menu entry here; unknown types fall
//       through to a plain row
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

const readOnlyChipClassName =
  "inline-flex h-5 max-w-full shrink-0 items-center gap-1 rounded-sm bg-muted px-1.5 text-micro text-muted-foreground";
const chipClassName = cn(
  readOnlyChipClassName,
  "transition-colors hover:bg-surface-selected hover:text-foreground data-[popup-open]:bg-surface-selected data-[popup-open]:text-foreground",
);


export function ProjectResourcesSection({
  projectId,
  readOnly = false,
}: {
  projectId: string;
  /** Lists the resources without adding, editing or removing (a guest's view). */
  readOnly?: boolean;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
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
  const canAddFolder = desktopMode && !readOnly;

  const daemonStatus = useLocalDaemonStatus();
  const localDaemonId = daemonStatus.daemonId;
  // Runtimes only name the machines folders live on, so a project without
  // folders (and a guest who cannot add one) never asks for them.
  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(wsId),
    enabled: folders.length > 0 || canAddFolder,
  });
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
  const currentMachineTitle =
    (localDaemonId ? machines.find((m) => m.daemonId === localDaemonId)?.title : undefined) ??
    daemonStatus.deviceName ??
    undefined;

  const isCurrent = (folder: LocalDirectoryResource) =>
    desktopMode && localDaemonId !== null && folder.resource_ref.daemon_id === localDaemonId;
  // This computer's folder first: it is the one that applies to runs here.
  const orderedFolders = [...folders.filter(isCurrent), ...folders.filter((f) => !isCurrent(f))];
  const hasCurrentFolder = orderedFolders.some(isCurrent);

  return (
    <ObjectSection
      collapsible
      title={t(($) => $.resources.section_header)}
      count={resources.length > 0 ? resources.length : undefined}
      action={
        readOnly ? undefined : (
          <AddResourceMenu
            wsId={wsId}
            projectId={projectId}
            attachedUrls={new Set(repos.map((r) => r.resource_ref.url))}
            workspaceRepos={workspace?.repos ?? []}
            canAddFolder={canAddFolder}
            hasCurrentFolder={hasCurrentFolder}
            daemonStatus={daemonStatus}
            runtimes={runtimes}
            serverValidatesWorktree={serverValidatesWorktree}
          />
        )
      }
    >
      {resources.length === 0 ? (
        <ObjectSectionEmpty
          icon={FolderGit}
          description={
            readOnly
              ? t(($) => $.resources.empty_read_only)
              : t(($) => $.resources.empty)
          }
        />
      ) : (
        <ObjectSectionCard>
          {repos.map((resource) => (
            <GithubRepoRow
              key={resource.id}
              resource={resource}
              description={
                workspace?.repos?.find((repo) => repo.url === resource.resource_ref.url)
                  ?.description
              }
              readOnly={readOnly}
            />
          ))}
          {orderedFolders.map((folder) => (
            <LocalFolderRow
              key={folder.id}
              resource={folder}
              current={isCurrent(folder)}
              currentMachineTitle={currentMachineTitle}
              machine={machines.find((m) => m.daemonId === folder.resource_ref.daemon_id)}
              serverValidatesWorktree={serverValidatesWorktree}
              readOnly={readOnly}
            />
          ))}
          {others.map((resource) => (
            <UnknownResourceRow key={resource.id} resource={resource} readOnly={readOnly} />
          ))}
        </ObjectSectionCard>
      )}
    </ObjectSection>
  );
}

// ---------------------------------------------------------------------------
// Adding

type PendingFolder = {
  path: string;
  label: string;
  mode: LocalDirectoryExecutionMode;
  /** undefined = unknown (older desktop build); treated as "cannot verify". */
  isGitRepo: boolean | undefined;
};

/**
 * The section's one Add entry. A repository is searched or pasted in a
 * popover anchored to the same button; a folder goes through the system
 * picker and then a dialog that asks for the execution mode before anything
 * is created — it is part of what the user is choosing (whether tasks edit
 * this folder or hand back a branch), not a setting to discover afterwards.
 */
function AddResourceMenu({
  wsId,
  projectId,
  attachedUrls,
  workspaceRepos,
  canAddFolder,
  hasCurrentFolder,
  daemonStatus,
  runtimes,
  serverValidatesWorktree,
}: {
  wsId: string;
  projectId: string;
  attachedUrls: Set<string>;
  workspaceRepos: { url: string; description?: string | null }[];
  canAddFolder: boolean;
  hasCurrentFolder: boolean;
  daemonStatus: LocalDaemonStatus;
  runtimes: AgentRuntime[];
  serverValidatesWorktree: boolean;
}) {
  const { t } = useT("projects");
  const createResource = useCreateProjectResource(wsId, projectId);
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null);
  const [repoOpen, setRepoOpen] = useState(false);
  // The menu hands focus back to its trigger as it closes, which would pull
  // focus out of the repository popover opening in its place.
  const openingRepo = useRef(false);
  const [picking, setPicking] = useState(false);
  const [pending, setPending] = useState<PendingFolder | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const daemonId = daemonStatus.daemonId;
  const daemonReady = daemonStatus.running && daemonId !== null;

  // The server allows one folder per computer, so the entry explains why it
  // is unavailable rather than disappearing.
  const folderBlocker = !canAddFolder
    ? t(($) => $.resources.add_local_desktop_only)
    : !daemonReady
      ? t(($) => $.resources.local_daemon_offline_hint)
      : hasCurrentFolder
        ? t(($) => $.resources.add_local_exists)
        : null;

  const attachRepo = async (url: string, ref?: string) => {
    try {
      await createResource.mutateAsync({
        resource_type: "github_repo",
        // Omit the key entirely when empty rather than sending "": an absent
        // ref is what "use the default branch" looks like on the wire.
        resource_ref: ref ? { url, ref } : { url },
      });
      toast.success(t(($) => $.resources.toast_attached));
      setRepoOpen(false);
    } catch (err) {
      toast.error(errorMessage(err, t(($) => $.resources.toast_attach_failed)));
    }
  };

  const pickFolder = async () => {
    if (picking || !daemonReady) return;
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

  const addFolder = async () => {
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

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          ref={setAnchor}
          render={<ObjectSectionAction icon={Plus} disabled={picking} />}
        >
          {t(($) => $.resources.add)}
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="end"
          className="w-72"
          finalFocus={() => {
            const skip = openingRepo.current;
            openingRepo.current = false;
            return !skip;
          }}
        >
          <AddMenuItem
            icon={<FolderGit />}
            title={t(($) => $.resources.add_repo)}
            hint={t(($) => $.resources.add_repo_hint)}
            onClick={() => {
              openingRepo.current = true;
              setRepoOpen(true);
            }}
          />
          <AddMenuItem
            icon={<FolderOpen />}
            title={t(($) => $.resources.add_local)}
            hint={folderBlocker ?? t(($) => $.resources.add_local_hint)}
            disabled={folderBlocker !== null}
            onClick={() => void pickFolder()}
          />
        </DropdownMenuContent>
      </DropdownMenu>
      <Popover open={repoOpen} onOpenChange={setRepoOpen}>
        <PopoverContent anchor={anchor} align="end" className="w-72 p-2">
          <AddRepositoryPanel
            workspaceRepos={workspaceRepos}
            attachedUrls={attachedUrls}
            busy={createResource.isPending}
            onAttach={attachRepo}
          />
        </PopoverContent>
      </Popover>
      <Dialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open && !saving) setPending(null);
        }}
      >
        {pending && (
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>{t(($) => $.resources.local_dialog_title)}</DialogTitle>
              <DialogDescription>
                {t(($) => $.resources.local_dialog_description)}
              </DialogDescription>
            </DialogHeader>
            <div className="flex items-center gap-2 rounded-lg border py-2 pr-1.5 pl-2.5">
              <FolderOpen className="size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="truncate text-body font-medium">{pending.label}</div>
                <div className="break-all font-mono text-micro text-muted-foreground">
                  {pending.path}
                </div>
              </div>
              <Button
                variant="ghost"
                size="xs"
                disabled={picking || saving}
                onClick={() => void pickFolder()}
              >
                {t(($) => $.resources.local_dialog_change)}
              </Button>
            </div>
            <div className="space-y-1.5">
              <div className="text-caption font-medium text-muted-foreground">
                {t(($) => $.resources.local_dialog_mode_label)}
              </div>
              <LocalDirectoryModeOptions
                value={pending.mode}
                onChange={(mode) => setPending({ ...pending, mode })}
                unavailableReason={worktreeUnavailableReason(
                  pending.isGitRepo,
                  serverValidatesWorktree,
                )}
              />
            </div>
            {error && <InlineError message={error} />}
            <DialogFooter>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setPending(null)}
                disabled={saving}
              >
                {t(($) => $.resources.mode_cancel)}
              </Button>
              <Button
                size="sm"
                onClick={() => void addFolder()}
                disabled={saving}
                aria-busy={saving}
              >
                {t(($) => $.resources.mode_add)}
              </Button>
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>
    </>
  );
}

function AddMenuItem({
  icon,
  title,
  hint,
  disabled,
  onClick,
}: {
  icon: React.ReactNode;
  title: string;
  hint: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <DropdownMenuItem
      disabled={disabled}
      onClick={onClick}
      className="items-start [&_svg]:mt-0.5"
    >
      {icon}
      <span className="min-w-0">
        <span className="block">{title}</span>
        <span className="block text-caption text-muted-foreground">{hint}</span>
      </span>
    </DropdownMenuItem>
  );
}

// ---------------------------------------------------------------------------
// Rows

function ResourceRow({
  icon: Icon,
  dim = false,
  name,
  chip,
  aside,
  menu,
}: {
  icon: typeof FolderGit;
  dim?: boolean;
  name: React.ReactNode;
  chip?: React.ReactNode;
  aside?: React.ReactNode;
  menu?: React.ReactNode;
}) {
  return (
    <div
      data-resource-row=""
      className={cn(
        "group/resource flex min-h-11 items-center gap-3 py-1.5 pr-2 pl-3 text-body",
        dim && "opacity-60",
      )}
    >
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      <div className="flex min-w-0 items-center gap-2">
        {name}
        {chip}
      </div>
      {/* Zero basis: takes only what the name leaves, so the name never truncates first.
          Safe alignment clips overflow on the right instead of spilling over the chip. */}
      <div className="flex min-w-0 flex-1 items-center justify-end-safe gap-1 overflow-hidden text-caption text-muted-foreground">
        {aside}
      </div>
      {menu ?? <span aria-hidden="true" className="size-6 shrink-0" />}
    </div>
  );
}

/** A row's ⋯ menu: one secondary action, then Remove. */
function ResourceRowMenu({
  resource,
  name,
  copyLabel,
  copyValue,
}: {
  resource: ProjectResource;
  name: string;
  copyLabel: string;
  copyValue: string;
}) {
  const { t } = useT("projects");
  const { remove, removing } = useRemoveResource(resource);
  const copy = async () => {
    if (await copyText(copyValue)) toast.success(t(($) => $.resources.toast_copied));
  };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton
            label={t(($) => $.resources.row_menu, { name })}
            size="icon-xs"
            disabled={removing}
            aria-busy={removing}
            className={cn(
              "text-muted-foreground",
              !removing &&
                "opacity-0 group-hover/resource:opacity-100 focus-visible:opacity-100 data-[popup-open]:opacity-100 [@media(pointer:coarse)]:opacity-100",
            )}
          />
        }
      >
        {removing ? <Spinner className="size-3" aria-hidden="true" /> : <MoreHorizontal />}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto min-w-40">
        <DropdownMenuItem onClick={() => void copy()}>
          <Copy className="size-3.5" />
          {copyLabel}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={() => void remove()}>
          <Trash2 className="size-3.5" />
          {t(($) => $.resources.remove_tooltip)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function GithubRepoRow({
  resource,
  description,
  readOnly,
}: {
  resource: GithubResource;
  description?: string | null;
  readOnly: boolean;
}) {
  const { t } = useT("projects");
  const ref = resource.resource_ref;
  const display = resource.label || githubShortLabel(ref.url);
  // Workspace repository descriptions are source-of-truth metadata. They stay
  // live here rather than being copied into the resource label, while an
  // explicit resource label remains the user's preferred primary text.
  const liveDescription = resource.label ? undefined : description?.trim();

  return (
    <ResourceRow
      icon={FolderGit}
      name={
        <Tooltip>
          <TooltipTrigger
            render={
              <a
                href={ref.url}
                target="_blank"
                rel="noopener noreferrer"
                className="min-w-0 truncate font-medium hover:underline"
              >
                {display}
              </a>
            }
          />
          <TooltipContent side="top">{ref.url}</TooltipContent>
        </Tooltip>
      }
      chip={<GithubRefChip resource={resource} readOnly={readOnly} />}
      aside={liveDescription ? <span className="truncate">{liveDescription}</span> : undefined}
      menu={
        readOnly ? undefined : (
          <ResourceRowMenu
            resource={resource}
            name={display}
            copyLabel={t(($) => $.resources.copy_url)}
            copyValue={ref.url}
          />
        )
      }
    />
  );
}

function LocalFolderRow({
  resource,
  current,
  currentMachineTitle,
  machine,
  serverValidatesWorktree,
  readOnly,
}: {
  resource: LocalDirectoryResource;
  current: boolean;
  currentMachineTitle?: string;
  machine?: RuntimeMachine;
  serverValidatesWorktree: boolean;
  readOnly: boolean;
}) {
  const { t } = useT("projects");
  // The name is the folder's own (or whatever a label update stored); there is
  // deliberately no rename. A folder is identified by its path (MUL-7525).
  const display = localDirectoryLabel(resource);
  const path = resource.resource_ref.local_path;
  const MachineIcon = current ? Laptop : Monitor;
  const machineTitle = current
    ? currentMachineTitle
    : (machine?.title ?? t(($) => $.resources.local_unknown_computer));
  const machineNote = current
    ? t(($) => $.resources.local_this_computer)
    : machine !== undefined && machine.health !== "online"
      ? t(($) => $.resources.local_offline)
      : undefined;

  return (
    // Folders on other machines are de-emphasised but stay editable: dropping
    // or reconfiguring a stale registration only rewrites the resource, which
    // works from any device.
    <ResourceRow
      icon={FolderOpen}
      dim={!current}
      name={
        <Tooltip>
          <TooltipTrigger
            render={<span className="min-w-0 truncate font-medium">{display}</span>}
          />
          <TooltipContent side="top">
            <span className="font-mono text-micro">{path}</span>
          </TooltipContent>
        </Tooltip>
      }
      chip={
        <ExecutionModeChip
          resource={resource}
          serverValidatesWorktree={serverValidatesWorktree}
          readOnly={readOnly}
        />
      }
      aside={
        <>
          <MachineIcon className="size-3 shrink-0" />
          {machineTitle && <span className="truncate">{machineTitle}</span>}
          {machineTitle && machineNote && <span aria-hidden="true">·</span>}
          {machineNote && <span className="shrink-0">{machineNote}</span>}
        </>
      }
      menu={
        readOnly ? undefined : (
          <ResourceRowMenu
            resource={resource}
            name={display}
            copyLabel={t(($) => $.resources.copy_path)}
            copyValue={path}
          />
        )
      }
    />
  );
}

function UnknownResourceRow({ resource, readOnly }: { resource: ProjectResource; readOnly: boolean }) {
  const { remove, removing } = useRemoveResource(resource);
  const { t } = useT("projects");
  return (
    <ResourceRow
      icon={FolderGit}
      name={
        <span className="min-w-0 truncate text-muted-foreground">
          {resource.label || resource.resource_type}
        </span>
      }
      menu={
        readOnly ? undefined : (
          <IconButton
            label={t(($) => $.resources.remove_tooltip)}
            size="icon-xs"
            className="text-muted-foreground"
            disabled={removing}
            aria-busy={removing}
            onClick={() => void remove()}
          >
            {removing ? <Spinner className="size-3" aria-hidden="true" /> : <Trash2 />}
          </IconButton>
        )
      }
    />
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
  // whole as a clone URL that does not exist. Changing the URL also replaces
  // the branch field, so a plain clone URL cannot inherit the previous ref.
  const handleQueryChange = (next: string) => {
    if (!looksLikeRepoUrl(next)) {
      setQuery(next);
      setRef("");
      return;
    }
    const split = splitGithubUrlRef(next);
    setQuery(split.url);
    setRef(split.ref ?? "");
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


/**
 * The branch a repository's tasks start from, editable in place.
 *
 * Always rendered, showing "Default branch" when nothing is pinned: without
 * it, clearing a branch makes the line vanish, which reads the same as the
 * setting never having existed.
 */
function GithubRefChip({ resource, readOnly }: { resource: GithubResource; readOnly: boolean }) {
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

  if (readOnly) {
    return (
      <span className={cn(readOnlyChipClassName, current && "text-foreground")}>
        <GitBranch className="size-3 shrink-0" />
        <span className={cn("max-w-32 truncate", current && "font-mono")}>
          {current || t(($) => $.resources.ref_default_label)}
        </span>
      </span>
    );
  }

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
/**
 * How runs use a folder, always visible — "direct" included — because it is
 * the first thing someone asks when a task queues (or does not). Picking an
 * option saves it; a server rejection stays inline so the choice can be fixed.
 */
function ExecutionModeChip({
  resource,
  serverValidatesWorktree,
  readOnly,
}: {
  resource: LocalDirectoryResource;
  serverValidatesWorktree: boolean;
  readOnly: boolean;
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

  const badge = (
    <>
      {mode === "worktree" ? (
        <GitBranch className="size-3 shrink-0" />
      ) : (
        <Pencil className="size-3 shrink-0" />
      )}
      {mode === "worktree"
        ? t(($) => $.resources.mode_badge_worktree)
        : t(($) => $.resources.mode_badge_in_place)}
    </>
  );

  if (readOnly) {
    return <span className={cn(readOnlyChipClassName, "text-foreground")}>{badge}</span>;
  }

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <TooltipHint label={t(($) => $.resources.mode_edit_tooltip)}>
        <PopoverTrigger
          render={
            <button type="button" className={cn(chipClassName, "text-foreground")}>
              {badge}
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
