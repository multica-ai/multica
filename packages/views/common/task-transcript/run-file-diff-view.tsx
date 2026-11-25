"use client";

import { useMemo, useRef, useState, useEffect, type ReactNode } from "react";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import type { AgentRuntime } from "@multica/core/types";
import {
  fileChangesOptions,
  fileChangeBodyOptions,
  fileChangeBodyKey,
  fileChangeBodyState,
  fileChangesStatus,
  canReadFileChange,
  type RunFileChange,
  type RunFileChangeBody,
} from "@multica/core/agents/file-changes";
import {
  useRuntimeHealth,
  type RuntimeHealth,
} from "@multica/core/runtimes";
import {
  AlertTriangle,
  ArchiveX,
  ArrowLeft,
  ChevronRight,
  CloudOff,
  ExternalLink,
  Folder,
  RotateCw,
  ShieldOff,
  type IconComponent,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { CopyPathButton, DiffExpandControls, DiffLayoutSwitch } from "../split-diff/diff-controls";
import {
  GitHubSplitDiff,
  useContainerSplitMode,
  useExpandedCollapseIds,
  useRenderedDiffItems,
  type GitHubSplitMode,
} from "../split-diff/github-split-diff";
import {
  buildSplitDiff,
  collapseUnchangedRows,
  isHugeDiff,
} from "../split-diff/split-diff-model";
import {
  resolveRunFileChangeCandidate,
  type LocalFileOpener,
} from "../../platform/open-local-file";
import {
  useExistingLocalFilePaths,
  useLocalFileOpeners,
  useOpenLocalFileAction,
} from "../../platform/use-open-local-file";
import { shortenTracePath } from "./trace-event-presenter";
import { languageForPath } from "./diff-highlight";
import { useT } from "../../i18n";
import {
  copyRunFileChangePath,
  groupRunFileChanges,
  isRunWorkdirRoot,
  type RunFileChangeGroup,
} from "./run-file-diff-groups";

export function RunFileDiffView({
  taskId,
  workdir,
  runtime,
  onBack,
}: {
  taskId: string;
  workdir?: string;
  runtime?: Pick<AgentRuntime, "id" | "runtime_mode" | "daemon_id"> | null;
  onBack: () => void;
}) {
  return (
    <RunFiles
      key={taskId}
      taskId={taskId}
      workdir={workdir}
      runtime={runtime}
      onBack={onBack}
    />
  );
}

function RunFiles({
  taskId,
  workdir,
  runtime,
  onBack,
}: {
  taskId: string;
  workdir?: string;
  runtime?: Pick<AgentRuntime, "id" | "runtime_mode" | "daemon_id"> | null;
  onBack: () => void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const query = useInfiniteQuery(fileChangesOptions(wsId, taskId));
  const collection = query.data?.pages[0];
  const files = useMemo(
    () => [
      ...new Map(
        query.data?.pages.flatMap((page) =>
          page.collectionId === collection?.collectionId
            ? page.files.map((file) => [file.id, { file, page }] as const)
            : [],
        ) ?? [],
      ).values(),
    ],
    [query.data, collection?.collectionId],
  );
  const { t: tSettings } = useT("settings");
  const { effective: opener, platform } = useLocalFileOpeners();
  const openLocalFile = useOpenLocalFileAction();
  // A root label is the root's path relative to the run workdir, so the
  // workdir the dialog already shows is enough to re-anchor each file on this
  // machine — no extra API field needed.
  const openCandidates = useMemo(() => {
    const map = new Map<string, string | null>();
    for (const { file, page: filePage } of files) {
      map.set(
        file.id,
        resolveRunFileChangeCandidate(
          filePage.roots.find((root) => root.id === file.rootId)?.label ?? "",
          file.path,
          workdir,
        ),
      );
    }
    return map;
  }, [files, workdir]);
  const existingPaths = useExistingLocalFilePaths(
    [...openCandidates.values()].filter(
      (candidate): candidate is string => candidate !== null,
    ),
    workdir,
    runtime,
  );
  const openerLabel = opener
    ? opener.id === "file-manager"
      ? platform === "darwin"
        ? tSettings(($) => $.preferences.open_file.finder)
        : platform === "win32"
          ? tSettings(($) => $.preferences.open_file.explorer)
          : tSettings(($) => $.preferences.open_file.file_manager)
      : opener.name
    : "";
  const rootRef = useRef<HTMLDivElement>(null);
  const { mode: autoMode, width } = useContainerSplitMode(rootRef);
  const [userMode, setUserMode] = useState<GitHubSplitMode | null>(null);
  const mode = autoMode === "unified" ? "unified" : (userMode ?? autoMode);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(
    () => new Set(),
  );
  const groups = useMemo(
    () => groupRunFileChanges(files, collection?.roots ?? []),
    [files, collection?.roots],
  );
  const collectionId = collection?.collectionId ?? null;
  // Runtime health drives two relay affordances: the group-header offline
  // badge, and one automatic refetch of offline-failed bodies once the
  // runtime is back online.
  const queryClient = useQueryClient();
  const runtimeHealth = useRuntimeHealth(wsId, runtime?.id);
  const runtimeOffline =
    runtimeHealth === "recently_lost" ||
    runtimeHealth === "offline" ||
    runtimeHealth === "long_offline";
  // Files auto-reloading on the offline → online edge render the reconnecting
  // line instead of their failure card.
  const [autoReloading, setAutoReloading] = useState<Set<string>>(
    () => new Set(),
  );
  // One automatic retry per file per mount — a follow-up failure stays manual.
  const autoRetriedRef = useRef<Set<string>>(new Set());
  const expandedRef = useRef(expanded);
  useEffect(() => {
    expandedRef.current = expanded;
  }, [expanded]);
  const prevHealthRef = useRef<RuntimeHealth | "loading" | undefined>(
    undefined,
  );
  useEffect(() => {
    const prev = prevHealthRef.current;
    prevHealthRef.current = runtimeHealth;
    if (wsId === undefined || runtimeHealth !== "online" || prev === "online")
      return;
    const reloadIds = [...expandedRef.current].filter((fileId) => {
      if (autoRetriedRef.current.has(fileId)) return false;
      const state = queryClient.getQueryState(
        fileChangeBodyKey(wsId, taskId, collectionId, fileId),
      );
      return (
        state?.status === "error" &&
        fileChangeBodyState(state.error) === "offline"
      );
    });
    if (reloadIds.length === 0) return;
    for (const fileId of reloadIds) autoRetriedRef.current.add(fileId);
    setAutoReloading((current) => new Set([...current, ...reloadIds]));
    void Promise.all(
      reloadIds.map((fileId) =>
        queryClient.refetchQueries({
          queryKey: fileChangeBodyKey(wsId, taskId, collectionId, fileId),
          exact: true,
        }),
      ),
    ).finally(() => {
      setAutoReloading((current) => {
        const next = new Set(current);
        for (const fileId of reloadIds) next.delete(fileId);
        return next;
      });
    });
  }, [runtimeHealth, wsId, taskId, collectionId, queryClient]);
  const collectionStatus = collection
    ? fileChangesStatus(collection)
    : "unavailable";
  const ready =
    query.data?.pages.every((page) => fileChangesStatus(page) === "ready") ===
    true;
  const snapshotUsable =
    collectionStatus === "ready" || collectionStatus === "partial";
  const hasFiles = files.length > 0;
  const isLoading = query.isPending || collectionStatus === "collecting";
  const isPartial = snapshotUsable && !ready;
  const isEmpty =
    !isLoading &&
    !query.isError &&
    snapshotUsable &&
    collection?.filesTotal === 0;
  const showCentered =
    !isLoading && !hasFiles && (isEmpty || query.isError || !snapshotUsable);
  // Retry only where re-requesting can change the answer: request errors and
  // interrupted runs (the spool may still deliver). A legacy or otherwise
  // degraded snapshot is a final state — the button itself is state semantics.
  const retryable =
    query.isError ||
    (collection !== undefined &&
      collectionStatus === "unavailable" &&
      collection.reason !== "legacy_no_baseline");
  const retryButton = retryable ? (
    <Button
      variant="outline"
      size="sm"
      className="cursor-pointer whitespace-nowrap"
      disabled={query.isFetching}
      aria-busy={query.isFetching}
      onClick={() =>
        void (query.isFetchNextPageError
          ? query.fetchNextPage()
          : query.refetch())
      }
    >
      <RotateCw
        aria-hidden
        className={cn("h-3 w-3", query.isFetching && "animate-spin")}
      />
      {t(($) => $.transcript.net_retry)}
    </Button>
  ) : null;
  const countLabel =
    collection === undefined
      ? ""
      : `${t(($) => $.transcript.net_scope)} · ${t(($) => $.transcript.net_count, { count: collection.filesTotal })}${collection.totalsComplete ? "" : "+"}`;
  const emptyDescription = isPartial
    ? `${t(($) => $.transcript.net_partial)} ${t(($) => $.transcript.net_scope)}`
    : undefined;
  const degradedTitle = t(($) => $.transcript.net_unavailable);
  const degradedDescription =
    collection?.reason === "legacy_no_baseline"
      ? t(($) => $.transcript.net_legacy)
      : collection?.reason === "interrupted"
        ? t(($) => $.transcript.net_interrupted)
        : undefined;
  return (
    <div
      ref={rootRef}
      className="@container/file-diff flex min-h-0 min-w-0 flex-1 flex-col"
      data-testid="run-file-diff-view"
      data-file-diff-container-width={
        width > 0 ? String(Math.round(width)) : undefined
      }
    >
      <div className="flex shrink-0 items-center gap-2 border-b px-4 py-1.5">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="cursor-pointer"
          autoFocus
          onClick={onBack}
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          {t(($) => $.transcript.files_back)}
        </Button>
        <span className="min-w-0 truncate text-caption font-medium">
          {t(($) => $.transcript.files_title)}
        </span>
        <div className="ml-auto flex items-center gap-1.5">
          {hasFiles ? (
            <DiffExpandControls
              onExpandAll={() =>
                setExpanded(new Set(files.map((entry) => entry.file.id)))
              }
              onCollapseAll={() => setExpanded(new Set())}
            />
          ) : null}
          {autoMode !== "unified" ? (
            <DiffLayoutSwitch mode={mode} onChange={setUserMode} />
          ) : null}
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col overflow-auto">
        {isLoading ? (
          <div
            className="flex flex-col gap-2 p-3"
            role="status"
            aria-busy="true"
          >
            <Skeleton className="h-9 w-full rounded-md" />
            <Skeleton className="h-9 w-full rounded-md" />
            <Skeleton className="h-9 w-full rounded-md" />
            <p className="text-micro text-muted-foreground">
              {collectionStatus === "collecting"
                ? t(($) => $.transcript.net_collecting)
                : t(($) => $.transcript.net_loading)}
            </p>
          </div>
        ) : showCentered ? (
          query.isError ? (
            <FileDiffStatus
              density="list"
              tone="danger"
              icon={AlertTriangle}
              title={t(($) => $.transcript.net_error)}
              description={t(($) => $.transcript.net_error_hint)}
              action={retryButton}
            />
          ) : isEmpty ? (
            <FileDiffStatus
              density="list"
              tone={isPartial ? "warning" : "neutral"}
              icon={isPartial ? AlertTriangle : Folder}
              title={t(($) => $.transcript.files_empty)}
              description={emptyDescription}
            />
          ) : (
            <FileDiffStatus
              density="list"
              tone={
                collection?.reason === "interrupted" ? "warning" : "neutral"
              }
              icon={
                collection?.reason === "interrupted" ? AlertTriangle : Folder
              }
              title={degradedTitle}
              description={degradedDescription}
              action={retryButton}
            />
          )
        ) : (
          <div className="p-3">
            {query.isError ? (
              <div
                role="status"
                data-testid="run-file-diff-banner"
                className="mb-3 flex items-start gap-2 rounded-lg border border-destructive/20 bg-destructive/5 px-2.5 py-2 text-micro text-destructive"
              >
                <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p>{t(($) => $.transcript.net_error)}</p>
                  {retryButton ? <div className="mt-2">{retryButton}</div> : null}
                </div>
              </div>
            ) : isPartial ? (
              <div
                role="status"
                data-testid="run-file-diff-banner"
                className="mb-3 flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/10 px-2.5 py-2 text-micro text-warning"
              >
                <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
                <div>
                  {t(($) => $.transcript.net_partial)} {countLabel}
                </div>
              </div>
            ) : snapshotUsable && collection ? (
              <p className="mb-3 text-micro text-muted-foreground">
                {countLabel}
              </p>
            ) : null}
          <div className="flex flex-col gap-3">
            {groups.map((group, groupIndex) => {
              const groupCollapsed = collapsedGroups.has(group.rootId);
              const filesId = `run-file-diff-group-files-${groupIndex}`;
              const labelId = `run-file-diff-group-label-${groupIndex}`;
              return (
                <section
                  key={group.rootId}
                  aria-labelledby={labelId}
                  data-testid="run-file-diff-group"
                  data-root-id={group.rootId}
                >
                  <FileDiffGroupHeader
                    group={group}
                    labelId={labelId}
                    filesId={filesId}
                    runtimeOffline={runtimeOffline}
                    collapsed={groupCollapsed}
                    onToggle={() =>
                      setCollapsedGroups((current) => {
                        const next = new Set(current);
                        if (next.has(group.rootId)) next.delete(group.rootId);
                        else next.add(group.rootId);
                        return next;
                      })
                    }
                  />
                  <div
                    id={filesId}
                    hidden={groupCollapsed}
                    className={
                      groupCollapsed ? undefined : "flex flex-col gap-2"
                    }
                  >
                    {group.files.map(({ file, page: filePage }) => {
                      const candidate = openCandidates.get(file.id) ?? null;
                      const rawLabel =
                        filePage.roots.find((root) => root.id === file.rootId)
                          ?.label ?? "";
                      return (
                        <FileCard
                          key={`${collection?.collectionId}:${file.id}`}
                          file={file}
                          bodyAllowed={canReadFileChange(filePage, file)}
                          rootLabel={group.displayName}
                          copyPath={copyRunFileChangePath(
                            rawLabel,
                            file.path,
                            workdir,
                          )}
                          openTarget={
                            candidate && existingPaths.has(candidate)
                              ? candidate
                              : null
                          }
                          opener={opener}
                          openerLabel={openerLabel}
                          onOpen={openLocalFile}
                          wsId={wsId}
                          taskId={taskId}
                          collectionId={collectionId}
                          mode={mode}
                          expanded={expanded.has(file.id)}
                          autoReloading={autoReloading.has(file.id)}
                          onToggle={() =>
                            setExpanded((current) => {
                              const next = new Set(current);
                              if (next.has(file.id)) next.delete(file.id);
                              else next.add(file.id);
                              return next;
                            })
                          }
                        />
                      );
                    })}
                  </div>
                </section>
              );
            })}
          </div>
          {query.hasNextPage ? (
            <Button
              className="mt-3 cursor-pointer"
              variant="ghost"
              size="sm"
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
            >
              {t(($) => $.transcript.net_more)}
            </Button>
          ) : null}
        </div>
        )}
      </div>
    </div>
  );
}

// Inline loading line for a file's expand area: initial body fetch and the
// Auto-refetch after the runtime comes back online share one shape —
// spinner first, then the sentence saying what is happening.
function BodyLoadingLine({ text, testId }: { text: string; testId: string }) {
  return (
    <p
      role="status"
      aria-busy="true"
      data-testid={testId}
      className="flex items-center gap-2 px-3 py-4 text-caption text-muted-foreground"
    >
      <RotateCw
        aria-hidden
        className="size-3.5 shrink-0 animate-spin motion-reduce:animate-none"
      />
      {text}
    </p>
  );
}

function FileDiffStatus({
  density,
  tone = "neutral",
  icon: Icon,
  title,
  description,
  action,
}: {
  density: "list" | "card";
  tone?: "neutral" | "warning" | "danger";
  icon: IconComponent;
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  const list = density === "list";
  return (
    <div
      role="status"
      className={
        list
          ? "grid flex-1 place-items-center px-6 py-8"
          : "flex items-start gap-2 px-3 py-3.5"
      }
    >
      {list ? (
        <div className="w-full max-w-[360px] text-center">
          <div
            className={cn(
              "mx-auto mb-3 grid size-10 place-items-center rounded-full",
              tone === "warning" && "bg-warning/15 text-warning",
              tone === "danger" && "bg-destructive/10 text-destructive",
              tone === "neutral" && "bg-muted text-muted-foreground",
            )}
          >
            <Icon aria-hidden className="size-[18px]" />
          </div>
          <p className="text-caption font-semibold">{title}</p>
          {description ? (
            <p className="mt-1.5 text-micro text-muted-foreground">
              {description}
            </p>
          ) : null}
          {action ? <div className="mt-3">{action}</div> : null}
        </div>
      ) : (
        <>
          <div
            className={cn(
              "grid size-7 shrink-0 place-items-center rounded-full",
              tone === "warning" && "bg-warning/15 text-warning",
              tone === "danger" && "bg-destructive/10 text-destructive",
              tone === "neutral" && "bg-muted text-muted-foreground",
            )}
          >
            <Icon aria-hidden className="size-3.5" />
          </div>
          <div className="min-w-0 text-left">
            <p className="text-caption font-medium">{title}</p>
            {description ? (
              <p className="mt-1 text-micro text-muted-foreground">
                {description}
              </p>
            ) : null}
            {action ? <div className="mt-2">{action}</div> : null}
          </div>
        </>
      )}
    </div>
  );
}

function FileCard({
  file,
  bodyAllowed,
  rootLabel,
  copyPath,
  openTarget,
  opener,
  openerLabel,
  onOpen,
  wsId,
  taskId,
  collectionId,
  mode,
  expanded,
  autoReloading,
  onToggle,
}: {
  file: RunFileChange;
  bodyAllowed: boolean;
  rootLabel: string;
  copyPath: string;
  openTarget: string | null;
  opener?: LocalFileOpener;
  openerLabel: string;
  onOpen: (
    path: string,
    openerId: string,
  ) => Promise<{ ok: true } | { ok: false; reason: string }>;
  wsId: string;
  taskId: string;
  collectionId: string | null;
  mode: GitHubSplitMode;
  expanded: boolean;
  autoReloading: boolean;
  onToggle: () => void;
}) {
  const { t } = useT("agents");
  const [forceHuge, setForceHuge] = useState(false);
  const hasBody =
    bodyAllowed &&
    !file.binary &&
    (file.bodyStatus === "available" || file.bodyStatus === "redacted");
  const huge = isHugeDiff(0, file.beforeBytes + file.afterBytes);
  const body = useQuery(
    fileChangeBodyOptions(
      wsId,
      taskId,
      collectionId,
      file.id,
      expanded && hasBody && (!huge || forceHuge),
    ),
  );
  const data = body.data;
  // relay states: 503/504/429 collapse into one recoverable offline
  // card, 410 is a terminal purged record; everything else keeps the generic
  // retryable failure card.
  const bodyState = body.error ? fileChangeBodyState(body.error) : null;
  // A manual retry of a body-less query restarts it in React Query's pending
  // state; remembering the click keeps the failure card up (button spinning,
  // panel unchanged) instead of swapping to the generic loading line, so the
  // user can tell a manual retry from the automatic online reload.
  const [manualRetry, setManualRetry] = useState(false);
  useEffect(() => {
    if (!body.isFetching) setManualRetry(false);
  }, [body.isFetching]);
  const retryBodyButton = (
    <Button
      variant="ghost"
      size="sm"
      className="cursor-pointer"
      disabled={body.isFetching}
      aria-busy={body.isFetching}
      onClick={() => {
        setManualRetry(true);
        void body.refetch();
      }}
    >
      <RotateCw
        aria-hidden
        className={cn(
          "size-3",
          body.isFetching && "animate-spin motion-reduce:animate-none",
        )}
      />
      {t(($) => $.transcript.net_retry)}
    </Button>
  );
  const hugeBody = useMemo(
    () =>
      data
        ? isHugeDiff(
            Math.max(
              data.before?.split("\n").length ?? 0,
              data.after?.split("\n").length ?? 0,
            ),
            (data.before?.length ?? 0) + (data.after?.length ?? 0),
          )
        : false,
    [data],
  );
  const validBody =
    data &&
    data.id === file.id &&
    data.rootId === file.rootId &&
    (data.bodyStatus === "available" || data.bodyStatus === "redacted") &&
    !data.binary;
  const label =
    file.change === "added"
      ? t(($) => $.transcript.files_added)
      : file.change === "deleted"
        ? t(($) => $.transcript.net_deleted)
        : file.change === "mode_changed"
          ? t(($) => $.transcript.net_mode)
          : file.change === "modified"
            ? t(($) => $.transcript.net_modified)
            : t(($) => $.transcript.net_unavailable);
  return (
    <Collapsible
      open={expanded}
      onOpenChange={onToggle}
      render={<article className="rounded-md border bg-surface" />}
    >
      <div
        className={cn(
          "relative sticky top-8 z-[2] flex items-center gap-1.5 bg-surface px-3 py-2",
          expanded ? "rounded-t-md border-b" : "rounded-md",
        )}
      >
        {/* Dead space and +/- counts sit outside the filename trigger; this
            overlay makes the rest of the row toggle without nesting buttons. */}
        <CollapsibleTrigger
          aria-hidden
          tabIndex={-1}
          data-testid="run-file-diff-row-rest"
          className="absolute inset-0 z-0 cursor-pointer"
        />
        <CollapsibleTrigger className="relative z-[1] flex min-w-0 cursor-pointer items-center gap-1.5 text-left">
          <ChevronRight
            aria-hidden
            className={cn("h-3.5 w-3.5 shrink-0", expanded && "rotate-90")}
          />
          <span
            className={cn(
              "shrink-0 rounded-full px-1.5 py-px text-micro",
              file.change === "added"
                ? "bg-success/15 text-success"
                : file.change === "deleted"
                  ? "bg-destructive/15 text-destructive"
                  : file.change === "modified"
                    ? "bg-info/15 text-info"
                    : file.change === "mode_changed"
                      ? "bg-warning/15 text-warning"
                      : "bg-muted text-muted-foreground",
            )}
          >
            {label}
          </span>
          <span
            className="min-w-0 truncate font-mono text-caption"
            title={`${rootLabel} / ${file.path}`}
          >
            {shortenTracePath(file.path.replaceAll("\\", "/"))}
          </span>
        </CollapsibleTrigger>
        <div className="relative z-[1] flex shrink-0 items-center gap-1.5">
          <CopyPathButton path={copyPath} />
          {openTarget && opener ? (
            <OpenPathButton
              path={openTarget}
              opener={opener}
              openerLabel={openerLabel}
              onOpen={onOpen}
            />
          ) : null}
        </div>
        {file.insertions !== undefined && file.deletions !== undefined ? (
          <span className="pointer-events-none relative z-[1] ml-auto flex shrink-0 items-center gap-1.5 pl-2 font-mono text-caption tabular-nums">
            <span className="text-success">+{file.insertions}</span>
            <span className="text-destructive">−{file.deletions}</span>
          </span>
        ) : null}
      </div>
      <CollapsibleContent className="h-(--collapsible-panel-height) overflow-hidden rounded-b-md transition-[height] duration-200 ease-out data-starting-style:h-0 data-ending-style:h-0 motion-reduce:transition-none">
        {expanded ? (
          <>
            {file.beforeMode !== file.afterMode ? (
              <p className="px-3 py-2 font-mono text-micro">
                {file.beforeMode || "—"} → {file.afterMode || "—"}
              </p>
            ) : null}
            {!hasBody ? (
              <FileDiffStatus
                density="card"
                tone={
                  file.binary || file.bodyStatus === "binary"
                    ? "neutral"
                    : "warning"
                }
                icon={
                  file.binary || file.bodyStatus === "binary"
                    ? Folder
                    : AlertTriangle
                }
                title={
                  file.binary || file.bodyStatus === "binary"
                    ? t(($) => $.transcript.files_binary)
                    : file.bodyStatus === "omitted"
                      ? t(($) => $.transcript.net_omitted)
                      : t(($) => $.transcript.net_unavailable)
                }
              />
            ) : (huge || hugeBody) && !forceHuge ? (
              <FileDiffStatus
                density="card"
                tone="warning"
                icon={AlertTriangle}
                title={t(($) => $.transcript.net_large)}
                action={
                  <Button
                    variant="ghost"
                    size="sm"
                    className="cursor-pointer"
                    onClick={() => setForceHuge(true)}
                  >
                    {t(($) => $.transcript.files_view_anyway)}
                  </Button>
                }
              />
            ) : autoReloading ? (
              <BodyLoadingLine
                testId="run-file-diff-body-reconnecting"
                text={t(($) => $.transcript.net_reconnecting)}
              />
            ) : bodyState === "offline" && !validBody ? (
              <div data-testid="run-file-diff-body-offline">
                <FileDiffStatus
                  density="card"
                  tone="warning"
                  icon={CloudOff}
                  title={t(($) => $.transcript.net_runtime_offline)}
                  description={t(($) => $.transcript.net_runtime_offline_hint)}
                  action={retryBodyButton}
                />
              </div>
            ) : bodyState === "purged" && !validBody ? (
              <div data-testid="run-file-diff-body-purged">
                <FileDiffStatus
                  density="card"
                  tone="neutral"
                  icon={ArchiveX}
                  title={t(($) => $.transcript.net_record_purged)}
                  description={t(($) => $.transcript.net_record_purged_hint)}
                />
              </div>
            ) : bodyState === "mismatch" && !validBody ? (
              // The served content failed the capture-time digest check.
              // Terminal: no retry, nothing to recover — the panel must say
              // so instead of rendering content nothing vouches for.
              <div data-testid="run-file-diff-body-mismatch">
                <FileDiffStatus
                  density="card"
                  tone="danger"
                  icon={ShieldOff}
                  title={t(($) => $.transcript.net_digest_mismatch)}
                  description={t(($) => $.transcript.net_digest_mismatch_hint)}
                />
              </div>
            ) : body.isPending && manualRetry && body.isFetching ? (
              <div data-testid="run-file-diff-body-offline">
                <FileDiffStatus
                  density="card"
                  tone="warning"
                  icon={CloudOff}
                  title={t(($) => $.transcript.net_runtime_offline)}
                  description={t(($) => $.transcript.net_runtime_offline_hint)}
                  action={retryBodyButton}
                />
              </div>
            ) : body.isPending ? (
              // Pending is checked before the `!validBody` catch-all: both
              // hold while the first fetch is in flight, and only an actual
              // failure state may render the failure cards below.
              <BodyLoadingLine
                testId="run-file-diff-body-loading"
                text={t(($) => $.transcript.net_loading)}
              />
            ) : body.isError || !validBody ? (
              <FileDiffStatus
                density="card"
                tone="danger"
                icon={AlertTriangle}
                title={t(($) => $.transcript.net_error)}
                action={retryBodyButton}
              />
            ) : (
              <>
                {data.bodyStatus === "redacted" ? (
                  <p className="px-3 py-2 text-caption text-warning">
                    {t(($) => $.transcript.net_redacted)}
                  </p>
                ) : null}
                <MountedDiff file={data} mode={mode} />
              </>
            )}
          </>
        ) : null}
      </CollapsibleContent>
    </Collapsible>
  );
}

function MountedDiff({
  file,
  mode,
}: {
  file: RunFileChangeBody;
  mode: GitHubSplitMode;
}) {
  const { t } = useT("agents");
  const collapsed = useMemo(
    () =>
      collapseUnchangedRows(
        buildSplitDiff(file.before ?? "", file.after ?? ""),
      ),
    [file.before, file.after],
  );
  const [expanded, expand] = useExpandedCollapseIds();
  const items = useRenderedDiffItems(collapsed, expanded);
  return (
    <GitHubSplitDiff
      items={items}
      mode={mode}
      gutter="tint"
      language={languageForPath(file.path)}
      virtualize={items.length >= 80}
      expandUnchangedLabel={(count) =>
        t(($) => $.transcript.files_expand_unchanged, { count })
      }
      onExpandCollapse={expand}
    />
  );
}

function OpenPathButton({
  path,
  opener,
  openerLabel,
  onOpen,
}: {
  path: string;
  opener: LocalFileOpener;
  openerLabel: string;
  onOpen: (
    path: string,
    openerId: string,
  ) => Promise<{ ok: true } | { ok: false; reason: string }>;
}) {
  const { t } = useT("agents");
  const label = t(($) => $.transcript.files_open_with, { app: openerLabel });

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            data-testid="run-file-diff-open"
            className="cursor-pointer shrink-0 rounded-xs p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
            aria-label={label}
            onClick={() => {
              void onOpen(path, opener.id).then((result) => {
                if (result.ok) return;
                if (result.reason === "not_found") {
                  toast.error(t(($) => $.transcript.files_open_missing));
                  return;
                }
                if (result.reason === "opener_unavailable") {
                  toast.error(
                    t(($) => $.transcript.files_open_app_missing, {
                      app: openerLabel,
                    }),
                  );
                  return;
                }
                if (result.reason === "launch_failed") {
                  toast.error(
                    t(($) => $.transcript.files_open_launch_failed, {
                      app: openerLabel,
                    }),
                  );
                  return;
                }
                toast.error(t(($) => $.transcript.files_open_failed));
              });
            }}
          >
            <ExternalLink className="h-3.5 w-3.5" />
          </button>
        }
      />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function FileDiffGroupHeader({
  group,
  labelId,
  filesId,
  runtimeOffline,
  collapsed,
  onToggle,
}: {
  group: RunFileChangeGroup;
  labelId: string;
  filesId: string;
  runtimeOffline: boolean;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const { t } = useT("agents");
  const kindLabel = isRunWorkdirRoot(group.label)
    ? t(($) => $.transcript.files_root_kind_workdir)
    : group.kind === "git"
      ? t(($) => $.transcript.files_root_kind_project)
      : group.kind === "directory"
        ? t(($) => $.transcript.files_root_kind_directory)
        : null;
  const insertions = group.files.reduce(
    (sum, { file }) => sum + (file.insertions ?? 0),
    0,
  );
  const deletions = group.files.reduce(
    (sum, { file }) => sum + (file.deletions ?? 0),
    0,
  );
  const hasStats = group.files.some(
    ({ file }) =>
      file.insertions !== undefined && file.deletions !== undefined,
  );
  return (
    <div className="sticky top-0 z-[3] flex min-h-8 flex-wrap items-center gap-2 bg-background px-1.5 pb-1.5 pt-1">
      <button
        type="button"
        aria-expanded={!collapsed}
        aria-controls={filesId}
        className="flex min-w-0 cursor-pointer items-center gap-1.5 rounded-sm px-1 py-0.5 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        onClick={onToggle}
      >
        <ChevronRight
          aria-hidden
          className={cn("h-3.5 w-3.5 shrink-0", !collapsed && "rotate-90")}
        />
        <span
          id={labelId}
          className="min-w-0 truncate text-caption font-semibold"
        >
          {group.displayName}
        </span>
        {kindLabel ? (
          <span className="shrink-0 rounded-sm border px-1 text-micro leading-4 text-muted-foreground">
            {kindLabel}
          </span>
        ) : null}
      </button>
      <span className="text-micro text-muted-foreground">
        {t(($) => $.transcript.files_group_count, {
          count: group.files.length,
        })}
      </span>
      {/* collection-level hint that bodies cannot be fetched right
          now — same warning badge family as the partial marker. */}
      {runtimeOffline ? (
        <span
          data-testid="run-file-diff-group-offline"
          className="flex shrink-0 items-center gap-1 rounded-sm bg-warning/15 px-1.5 text-micro leading-[18px] text-warning"
        >
          <CloudOff aria-hidden className="size-[11px]" />
          {t(($) => $.transcript.net_group_runtime_offline)}
        </span>
      ) : null}
      {group.partial ? (
        <span className="rounded-sm bg-warning/15 px-1.5 text-micro leading-[18px] text-warning">
          {t(($) => $.transcript.net_partial)}
        </span>
      ) : null}
      {hasStats ? (
        <span className="ml-auto flex shrink-0 items-center gap-1.5 font-mono text-caption tabular-nums @max-[639px]/file-diff:hidden">
          <span className="text-success">+{insertions}</span>
          <span className="text-destructive">−{deletions}</span>
        </span>
      ) : null}
    </div>
  );
}
