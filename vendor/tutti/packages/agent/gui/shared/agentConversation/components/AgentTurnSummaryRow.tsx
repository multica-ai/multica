import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  toast
} from "@tutti-os/ui-system";
import {
  ChevronDown,
  ChevronRight,
  LoaderCircle,
  Redo2,
  Undo2
} from "lucide-react";
import { observer } from "mobx-react-lite";
import { useEffect, useMemo, useState, type JSX, type MouseEvent } from "react";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import { DirectLinedIcon } from "../../../app/renderer/components/icons/DirectLinedIcon";
import type { WorkspaceLinkAction } from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import { resolveWorkspaceFileLinkAction } from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import { CanvasNodeGhostIconButton } from "../../../contexts/workspace/presentation/renderer/components/shared/CanvasNodeGhostIconButton";
import { translate } from "../../../i18n/index";
import type {
  AgentTurnSummaryFileVM,
  AgentTurnSummaryPatchBatchVM,
  AgentTurnSummaryRowVM
} from "../contracts/agentTurnSummaryRowVM";
import { buildAgentTurnSummaryPatchDiff } from "../rules/agentTurnSummaryPatchDiff";
import {
  fileCanBuildPatch,
  patchBatchDirectoryCwd,
  resolvePatchDiffCwd,
  resolvePatchExecutionCwd
} from "../rules/agentTurnSummaryPatchRuntime";
import { CollapsibleReveal } from "./CollapsibleReveal";
import { AgentCodeBlock } from "./tool-renderers/code/AgentCodeBlock";
import { AgentMonacoDiffViewer } from "./tool-renderers/file-diff/AgentMonacoDiffViewer";
import {
  agentFileChangeStats,
  isAgentUnifiedDiff
} from "./tool-renderers/file-diff/agentUnifiedDiff";
import { AgentUnifiedPatchViewer } from "./tool-renderers/file-diff/AgentUnifiedPatchViewer";
const TurnSummaryFileCard = observer(TurnSummaryFileCardRender);

interface AgentTurnSummaryRowProps {
  row: AgentTurnSummaryRowVM;
  workspaceRoot?: string | null;
  basePath?: string | null;
  label: string;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
}

type PatchSupportState =
  | { key: string; status: "checking" }
  | { key: string; status: "supported" }
  | { key: string; status: "unsupported" };

export const AgentTurnSummaryRow = observer(function AgentTurnSummaryRow({
  row,
  workspaceRoot,
  basePath,
  onLinkAction
}: AgentTurnSummaryRowProps): JSX.Element {
  "use memo";
  const [expandedFiles, setExpandedFiles] = useState<Record<string, boolean>>(
    {}
  );
  const [showAllFiles, setShowAllFiles] = useState(false);
  const [patchAction, setPatchAction] = useState<"undo" | "reapply">("undo");
  const [patchPending, setPatchPending] = useState(false);
  const [patchSupportState, setPatchSupportState] =
    useState<PatchSupportState | null>(null);
  const agentHostApi = useOptionalAgentHostApi();
  const aggregateStats = useMemo(
    () => summarizeRowDiff(row.files),
    [row.files]
  );
  const patchBatches = useMemo(() => {
    const canBuildEveryFileFallback = row.files.every(fileCanBuildPatch);
    const fileFallbackBatches: AgentTurnSummaryPatchBatchVM[] =
      canBuildEveryFileFallback
        ? row.files.map((file) => ({
            changes: [
              {
                path: file.path,
                changeType: file.changeType,
                unifiedDiff: file.unifiedDiff,
                oldString: file.oldString ?? null,
                newString: file.newString ?? null,
                content: file.content ?? null
              }
            ],
            cwd: fallbackPatchFileCwd(file.path, workspaceRoot),
            toolCallId: `${row.id}:${file.messageId}:unified-diff`
          }))
        : [];
    const sourceBatches = row.patchBatches?.length
      ? row.patchBatches
      : fileFallbackBatches;
    if (!hasCompletePatchCoverage(row.files, sourceBatches)) {
      return [];
    }
    return buildExecutablePatchBatches(sourceBatches, workspaceRoot);
  }, [row.files, row.id, row.patchBatches, workspaceRoot]);
  const canRenderPatchButton = Boolean(
    agentHostApi?.workspace.applyGitPatch && row.files.length > 0
  );
  const canApplyPatch = Boolean(
    agentHostApi?.workspace.applyGitPatch && patchBatches.length > 0
  );
  const patchSupportCwds = useMemo(
    () => Array.from(new Set(patchBatches.map((batch) => batch.cwd))).sort(),
    [patchBatches]
  );
  const patchSupportKey = patchSupportCwds.join("\n");
  const resolveGitPatchSupport = agentHostApi?.workspace.resolveGitPatchSupport;
  useEffect(() => {
    if (!resolveGitPatchSupport || patchSupportCwds.length === 0) {
      setPatchSupportState(null);
      return;
    }
    let disposed = false;
    setPatchSupportState({ key: patchSupportKey, status: "checking" });
    void (async () => {
      try {
        for (const cwd of patchSupportCwds) {
          const result = await resolveGitPatchSupport({ cwd });
          if (!result.supported) {
            if (!disposed) {
              setPatchSupportState({
                key: patchSupportKey,
                status: "unsupported"
              });
            }
            return;
          }
        }
        if (!disposed) {
          setPatchSupportState({ key: patchSupportKey, status: "supported" });
        }
      } catch {
        if (!disposed) {
          setPatchSupportState({ key: patchSupportKey, status: "supported" });
        }
      }
    })();
    return () => {
      disposed = true;
    };
  }, [patchSupportCwds, patchSupportKey, resolveGitPatchSupport]);
  const visibleFiles = row.files.slice(0, 3);
  const hiddenFiles = row.files.slice(3);
  const hiddenFileCount = hiddenFiles.length;
  const patchActionLabel =
    patchAction === "undo"
      ? translate("agentHost.agentGui.turnSummaryUndo")
      : translate("agentHost.agentGui.turnSummaryReapply");
  const isPatchSupportChecking = Boolean(
    resolveGitPatchSupport &&
      patchSupportCwds.length > 0 &&
      (patchSupportState?.key !== patchSupportKey ||
        patchSupportState.status === "checking")
  );
  const isPatchUnsupported = Boolean(
    patchSupportState?.key === patchSupportKey &&
      patchSupportState.status === "unsupported"
  );
  const patchDisabledReason = isPatchUnsupported
    ? translate("agentHost.agentGui.turnSummaryGitRequired")
    : isPatchSupportChecking
      ? translate("agentHost.agentGui.turnSummaryCheckingGit")
      : !canApplyPatch
        ? translate("agentHost.agentGui.turnSummaryPatchUnavailable")
        : null;
  const isPatchActionDisabled = Boolean(
    patchPending ||
      isPatchSupportChecking ||
      isPatchUnsupported ||
      !canApplyPatch
  );

  const handlePatchAction = async (event: MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    if (!agentHostApi?.workspace.applyGitPatch || isPatchActionDisabled) {
      return;
    }
    const orderedBatches =
      patchAction === "undo" ? [...patchBatches].reverse() : patchBatches;
    if (orderedBatches.length === 0) {
      return;
    }
    setPatchPending(true);
    let changed = false;
    const failureMessage =
      patchAction === "undo"
        ? translate("agentHost.agentGui.turnSummaryUndoFailed")
        : translate("agentHost.agentGui.turnSummaryReapplyFailed");
    try {
      for (const batch of orderedBatches) {
        const result = await agentHostApi.workspace.applyGitPatch({
          cwd: batch.cwd,
          diff: batch.diff,
          revert: patchAction === "undo"
        });
        if (
          result.status === "success" ||
          result.appliedPaths.length > 0 ||
          result.conflictedPaths.length > 0
        ) {
          changed = true;
        }
        if (result.status !== "success") {
          showPatchFailureToast(
            agentHostApi,
            patchFailureMessage(result.errorCode, failureMessage)
          );
          return;
        }
      }
      setPatchAction((current) => (current === "undo" ? "reapply" : "undo"));
    } catch {
      showPatchFailureToast(agentHostApi, failureMessage);
    } finally {
      setPatchPending(false);
      if (changed) {
        window.dispatchEvent(new CustomEvent("tutti-agent-git-patch-applied"));
      }
    }
  };
  const handlePatchActionCallback = usePersistCallback(handlePatchAction);

  return (
    <section className="workspace-agents-status-panel__detail-turn-summary">
      <div className="agent-turn-summary-card w-full overflow-hidden rounded-[8px] text-[var(--text-primary)]">
        <div className="flex items-center gap-3 px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-3">
              <div className="min-w-0 text-[15px] font-semibold leading-5 tracking-[0] text-[var(--text-primary)]">
                {translate("agentHost.agentGui.turnSummaryFilesChanged", {
                  count: row.fileCount
                })}
              </div>
              <div className="inline-flex shrink-0 items-center gap-2.5 text-[11px] font-semibold">
                {aggregateStats.added > 0 ? (
                  <span className="workspace-agents-status-panel__detail-tool-diff-added">
                    +{aggregateStats.added}
                  </span>
                ) : null}
                {aggregateStats.removed > 0 ? (
                  <span className="workspace-agents-status-panel__detail-tool-diff-removed">
                    -{aggregateStats.removed}
                  </span>
                ) : null}
              </div>
            </div>
          </div>
          {canRenderPatchButton ? (
            <TooltipProvider delayDuration={200}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span
                    className={`inline-flex shrink-0 ${
                      isPatchActionDisabled ? "cursor-not-allowed" : ""
                    }`}
                  >
                    <CanvasNodeGhostIconButton
                      aria-label={patchActionLabel}
                      className="w-auto min-w-0 gap-1.5 px-2 text-[12px] font-medium leading-4 disabled:pointer-events-none"
                      disabled={isPatchActionDisabled}
                      onClick={handlePatchActionCallback}
                    >
                      {patchPending || isPatchSupportChecking ? (
                        <LoaderCircle
                          width={14}
                          height={14}
                          aria-hidden="true"
                          className="animate-spin text-[var(--text-secondary)]"
                        />
                      ) : patchAction === "undo" ? (
                        <Undo2
                          width={14}
                          height={14}
                          aria-hidden="true"
                          className="text-[var(--text-secondary)]"
                        />
                      ) : (
                        <Redo2
                          width={14}
                          height={14}
                          aria-hidden="true"
                          className="text-[var(--text-secondary)]"
                        />
                      )}
                      <span>{patchActionLabel}</span>
                    </CanvasNodeGhostIconButton>
                  </span>
                </TooltipTrigger>
                {patchDisabledReason ? (
                  <TooltipContent className="max-w-[260px] whitespace-normal text-left">
                    {patchDisabledReason}
                  </TooltipContent>
                ) : null}
              </Tooltip>
            </TooltipProvider>
          ) : null}
        </div>

        <div className="agent-turn-summary-card__list">
          {visibleFiles.map((file) => {
            const key = `${file.path}:${file.messageId}`;
            return (
              <TurnSummaryFileCardBinding1
                key={key}
                bindingKey={key}
                file={file}
                workspaceRoot={workspaceRoot}
                basePath={basePath}
                expandedFiles={expandedFiles}
                onLinkAction={onLinkAction}
                setExpandedFiles={setExpandedFiles}
              />
            );
          })}

          {hiddenFileCount > 0 ? (
            <CollapsibleReveal expanded={showAllFiles} preMountOnIdle>
              <div className="agent-turn-summary-card__overflow-list">
                {hiddenFiles.map((file) => {
                  const key = `${file.path}:${file.messageId}`;
                  return (
                    <TurnSummaryFileCardBinding2
                      key={key}
                      bindingKey={key}
                      file={file}
                      workspaceRoot={workspaceRoot}
                      basePath={basePath}
                      expandedFiles={expandedFiles}
                      onLinkAction={onLinkAction}
                      setExpandedFiles={setExpandedFiles}
                    />
                  );
                })}
              </div>
            </CollapsibleReveal>
          ) : null}

          {hiddenFileCount > 0 && !showAllFiles ? (
            <ButtonBinding3
              setShowAllFiles={setShowAllFiles}
              hiddenFileCount={hiddenFileCount}
            />
          ) : hiddenFileCount > 0 ? (
            <ButtonBinding4 setShowAllFiles={setShowAllFiles} />
          ) : null}
        </div>
      </div>
    </section>
  );
});

function patchFailureMessage(
  errorCode: string | undefined,
  fallback: string
): string {
  switch (errorCode) {
    case "invalid-patch":
      return translate("agentHost.agentGui.turnSummaryInvalidPatch");
    case "patch-does-not-apply":
      return translate("agentHost.agentGui.turnSummaryPatchDoesNotApply");
    case "not-git-repo":
      return translate("agentHost.agentGui.turnSummaryGitRequired");
    default:
      return fallback;
  }
}

function TurnSummaryFileCardRender({
  file,
  workspaceRoot,
  basePath,
  expanded,
  onLinkAction,
  onToggle
}: TurnSummaryFileCardProps): JSX.Element {
  "use memo";
  const action = resolveWorkspaceFileLinkAction({
    path: file.path,
    workspaceRoot: workspaceRoot,
    basePath,
    source: "agent-file-change"
  });
  const canOpen = Boolean(action && onLinkAction);
  const stats = summarizeFileDiff(file);
  const preview = filePreview(file);

  return (
    <div className="agent-turn-summary-card__file">
      <div className="agent-turn-summary-card__file-row flex min-w-0 items-center gap-2.5 overflow-hidden px-4 py-2.5">
        <div className="flex min-w-0 flex-1 items-center gap-2.5">
          <button
            type="button"
            className="group/file-toggle flex min-w-0 flex-1 items-center gap-1 overflow-hidden text-left"
            aria-expanded={expanded}
            onClick={onToggle}
          >
            <div className="min-w-0 flex-1 overflow-hidden">
              <span
                className={`agent-turn-summary-card__path flex min-w-0 max-w-full overflow-hidden whitespace-nowrap text-[13px] font-medium leading-5 text-[var(--text-secondary)] ${
                  file.changeType === "deleted" ? "line-through" : ""
                }`}
                title={file.path}
              >
                {file.directory ? (
                  <span className="agent-turn-summary-card__path-directory">
                    {file.directory}/
                  </span>
                ) : null}
                <span className="agent-turn-summary-card__path-file">
                  {file.fileName}
                </span>
              </span>
            </div>
            <span className="shrink-0 text-[var(--text-tertiary)] opacity-0 transition-opacity group-hover/file-toggle:opacity-100 group-focus-visible/file-toggle:opacity-100">
              <ChevronRight
                size={14}
                strokeWidth={2.2}
                aria-hidden="true"
                className={`transition-transform duration-150 ease-out ${expanded ? "rotate-90" : "rotate-0"}`}
              />
            </span>
          </button>
          <span className="ml-auto inline-flex shrink-0 items-center gap-2 text-[11px] font-semibold">
            {stats.added > 0 ? (
              <span className="workspace-agents-status-panel__detail-tool-diff-added">
                +{stats.added}
              </span>
            ) : null}
            {stats.removed > 0 ? (
              <span className="workspace-agents-status-panel__detail-tool-diff-removed">
                -{stats.removed}
              </span>
            ) : null}
          </span>
        </div>

        {canOpen ? (
          <CanvasNodeGhostIconButtonBinding5
            file={file}
            onLinkAction={onLinkAction}
            action={action}
          />
        ) : null}
      </div>

      {preview ? (
        <CollapsibleReveal expanded={expanded} preMountOnIdle>
          <div className="agent-turn-summary-card__preview rounded-none px-4 pb-3 pt-2">
            {preview}
          </div>
        </CollapsibleReveal>
      ) : null}
    </div>
  );
}

function filePreview(file: AgentTurnSummaryFileVM): JSX.Element | null {
  if (
    file.changeType === "created" &&
    file.content !== null &&
    file.content !== undefined
  ) {
    return (
      <AgentCodeBlock
        path={file.path}
        content={file.content}
        showHeader={false}
        collapsible
        flat
      />
    );
  }

  if (file.unifiedDiff && isAgentUnifiedDiff(file.unifiedDiff)) {
    return (
      <AgentUnifiedPatchViewer
        path={file.path}
        diffText={file.unifiedDiff}
        showHeader={false}
        flat
      />
    );
  }

  if (
    file.changeType === "created" &&
    file.newString !== null &&
    file.newString !== undefined
  ) {
    return (
      <AgentCodeBlock
        path={file.path}
        content={file.newString}
        showHeader={false}
        collapsible
        flat
      />
    );
  }

  if (
    (file.oldString !== null && file.oldString !== undefined) ||
    (file.newString !== null && file.newString !== undefined)
  ) {
    return (
      <AgentMonacoDiffViewer
        path={file.path}
        oldValue={file.oldString ?? ""}
        newValue={file.newString ?? ""}
        flat
        showHeader={false}
      />
    );
  }

  if (
    file.changeType === "deleted" &&
    ((file.content !== null && file.content !== undefined) ||
      (file.unifiedDiff !== null && file.unifiedDiff !== undefined))
  ) {
    return (
      <AgentMonacoDiffViewer
        path={file.path}
        oldValue={file.oldString ?? file.content ?? file.unifiedDiff ?? ""}
        newValue=""
        flat
        showHeader={false}
      />
    );
  }

  if (file.content !== null && file.content !== undefined) {
    return (
      <AgentCodeBlock
        path={file.path}
        content={file.content}
        showHeader={false}
        collapsible
        flat
      />
    );
  }

  if (
    file.unifiedDiff !== null &&
    file.unifiedDiff !== undefined &&
    file.changeType !== "deleted"
  ) {
    return (
      <AgentCodeBlock
        path={file.path}
        content={file.content ?? file.newString ?? file.unifiedDiff}
        showHeader={false}
        collapsible
        flat
      />
    );
  }

  return null;
}

function summarizeRowDiff(files: AgentTurnSummaryFileVM[]): {
  added: number;
  removed: number;
} {
  return files.reduce(
    (totals, file) => {
      const stats = summarizeFileDiff(file);
      return {
        added: totals.added + stats.added,
        removed: totals.removed + stats.removed
      };
    },
    { added: 0, removed: 0 }
  );
}

function showPatchFailureToast(
  agentHostApi: ReturnType<typeof useOptionalAgentHostApi>,
  message: string
): void {
  if (agentHostApi?.toast?.error) {
    agentHostApi.toast.error(message);
    return;
  }
  toast.error(message);
}

function buildExecutablePatchBatches(
  sourceBatches: readonly AgentTurnSummaryPatchBatchVM[],
  workspaceRoot?: string | null
): { cwd: string; diff: string }[] {
  const batches: { cwd: string; diff: string }[] = [];
  for (const batch of sourceBatches) {
    if (
      batch.changes.length === 0 ||
      batch.changes.some(
        (change) =>
          !buildAgentTurnSummaryPatchDiff({
            ...batch,
            changes: [change]
          }).trim()
      )
    ) {
      return [];
    }
    const sourceCwd = patchBatchDirectoryCwd(
      batch.cwd ?? workspaceRoot ?? null,
      batch.changes,
      workspaceRoot
    );
    const executionCwd = resolvePatchExecutionCwd(sourceCwd, workspaceRoot);
    const diffCwd = resolvePatchDiffCwd({
      executionCwd,
      sourceCwd,
      changes: batch.changes
    });
    const diff = buildAgentTurnSummaryPatchDiff({
      ...batch,
      cwd: diffCwd
    });
    if (!executionCwd || !diff.trim()) {
      return [];
    }
    batches.push({ cwd: executionCwd, diff });
  }
  return batches;
}

function hasCompletePatchCoverage(
  files: readonly AgentTurnSummaryFileVM[],
  batches: readonly AgentTurnSummaryPatchBatchVM[]
): boolean {
  if (files.length === 0 || batches.length === 0) {
    return false;
  }
  const visiblePaths = new Set(files.map((file) => file.path));
  const patchPaths = batches.flatMap((batch) =>
    batch.changes.map((change) => change.path)
  );
  return (
    patchPaths.every((path) => visiblePaths.has(path)) &&
    files.every((file) => patchPaths.includes(file.path))
  );
}

function fallbackPatchFileCwd(
  path: string,
  workspaceRoot?: string | null
): string | null {
  const root = workspaceRoot?.trim() ?? "";
  if (root && root !== "/") {
    return root;
  }
  const normalizedPath = path.trim().replaceAll("\\", "/").replace(/\/+$/, "");
  if (!normalizedPath.startsWith("/")) {
    return root || null;
  }
  const index = normalizedPath.lastIndexOf("/");
  if (index <= 0) {
    return "/";
  }
  return normalizedPath.slice(0, index);
}

function summarizeFileDiff(file: AgentTurnSummaryFileVM): {
  added: number;
  removed: number;
} {
  return agentFileChangeStats({
    changeType: file.changeType,
    unifiedDiff: file.unifiedDiff,
    content: file.content,
    oldString: file.oldString,
    newString: file.newString
  });
}

const TurnSummaryFileCardBinding1 = observer(
  function TurnSummaryFileCardBinding1({
    bindingKey: key,
    file,
    workspaceRoot,
    basePath,
    expandedFiles,
    onLinkAction,
    setExpandedFiles
  }: TurnSummaryFileCardBinding1Props) {
    const onToggleProp1 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof TurnSummaryFileCard>["onToggle"]>
    >(() =>
      setExpandedFiles((current) => ({
        ...current,
        [key]: !current[key]
      }))
    );
    return (
      <TurnSummaryFileCard
        key={key}
        file={file}
        workspaceRoot={workspaceRoot}
        basePath={basePath}
        expanded={Boolean(expandedFiles[key])}
        onLinkAction={onLinkAction}
        onToggle={onToggleProp1}
      />
    );
  }
);

const TurnSummaryFileCardBinding2 = observer(
  function TurnSummaryFileCardBinding2({
    bindingKey: key,
    file,
    workspaceRoot,
    basePath,
    expandedFiles,
    onLinkAction,
    setExpandedFiles
  }: TurnSummaryFileCardBinding1Props) {
    const onToggleProp2 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof TurnSummaryFileCard>["onToggle"]>
    >(() =>
      setExpandedFiles((current) => ({
        ...current,
        [key]: !current[key]
      }))
    );
    return (
      <TurnSummaryFileCard
        key={key}
        file={file}
        workspaceRoot={workspaceRoot}
        basePath={basePath}
        expanded={Boolean(expandedFiles[key])}
        onLinkAction={onLinkAction}
        onToggle={onToggleProp2}
      />
    );
  }
);

const ButtonBinding3 = observer(function ButtonBinding3({
  setShowAllFiles,
  hiddenFileCount,
  ...slotProps
}: {
  setShowAllFiles: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  hiddenFileCount: number;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    setShowAllFiles: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    hiddenFileCount: number;
  }
>) {
  const onClickProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => setShowAllFiles(true));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="agent-turn-summary-card__toggle-more flex w-full items-center gap-2 px-4 py-2.5 text-left text-[11px] font-semibold transition-colors"
        onClick={onClickProp3}
      >
        <span>
          {translate("agentHost.agentGui.turnSummaryShowMoreFiles", {
            count: hiddenFileCount
          })}
        </span>
        <ChevronDown size={16} strokeWidth={2.2} aria-hidden="true" />
      </button>
    </Slot.Root>
  );
});

const ButtonBinding4 = observer(function ButtonBinding4({
  setShowAllFiles,
  ...slotProps
}: {
  setShowAllFiles: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    setShowAllFiles: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
  }
>) {
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => setShowAllFiles(false));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="agent-turn-summary-card__toggle-more flex w-full items-center gap-2 px-4 py-2.5 text-left text-[11px] font-semibold transition-colors"
        onClick={onClickProp4}
      >
        <span>{translate("agentHost.agentGui.turnSummaryShowFewerFiles")}</span>
        <ChevronRight
          size={16}
          strokeWidth={2.2}
          aria-hidden="true"
          className="-rotate-90"
        />
      </button>
    </Slot.Root>
  );
});

const CanvasNodeGhostIconButtonBinding5 = observer(
  function CanvasNodeGhostIconButtonBinding5({
    file,
    onLinkAction,
    action,
    ...slotProps
  }: {
    file: NonNullable<TurnSummaryFileCardProps>["file"];
    onLinkAction: NonNullable<TurnSummaryFileCardProps>["onLinkAction"];
    action:
      | import("../../../actions/workspaceLinkActions").OpenWorkspaceFileLinkAction
      | null;
  } & Omit<
    React.ComponentProps<typeof Slot.Root>,
    keyof {
      file: NonNullable<TurnSummaryFileCardProps>["file"];
      onLinkAction: NonNullable<TurnSummaryFileCardProps>["onLinkAction"];
      action:
        | import("../../../actions/workspaceLinkActions").OpenWorkspaceFileLinkAction
        | null;
    }
  >) {
    const onClickProp5 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof CanvasNodeGhostIconButton>["onClick"]
      >
    >(() => {
      onLinkAction?.(action as WorkspaceLinkAction);
    });
    return (
      <Slot.Root {...slotProps}>
        <CanvasNodeGhostIconButton
          aria-label={translate(
            "agentHost.workspaceAgentSessionDetailOpenFile",
            {
              path: file.path
            }
          )}
          onClick={onClickProp5}
        >
          <DirectLinedIcon
            width={14}
            height={14}
            aria-hidden="true"
            className="text-[var(--text-secondary)]"
          />
        </CanvasNodeGhostIconButton>
      </Slot.Root>
    );
  }
);

type TurnSummaryFileCardProps = {
  file: AgentTurnSummaryFileVM;
  workspaceRoot?: string | null;
  basePath?: string | null;
  expanded: boolean;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onToggle: () => void;
};

type TurnSummaryFileCardBinding1Props = {
  bindingKey: string;
  file: AgentTurnSummaryFileVM;
  workspaceRoot: NonNullable<AgentTurnSummaryRowProps>["workspaceRoot"];
  basePath: NonNullable<AgentTurnSummaryRowProps>["basePath"];
  expandedFiles: Record<string, boolean>;
  onLinkAction: NonNullable<AgentTurnSummaryRowProps>["onLinkAction"];
  setExpandedFiles: import("react").Dispatch<
    import("react").SetStateAction<Record<string, boolean>>
  >;
};

type TurnSummaryFileCardBinding2Props = {
  bindingKey: string;
  file: AgentTurnSummaryFileVM;
  workspaceRoot: NonNullable<AgentTurnSummaryRowProps>["workspaceRoot"];
  basePath: NonNullable<AgentTurnSummaryRowProps>["basePath"];
  expandedFiles: Record<string, boolean>;
  onLinkAction: NonNullable<AgentTurnSummaryRowProps>["onLinkAction"];
  setExpandedFiles: import("react").Dispatch<
    import("react").SetStateAction<Record<string, boolean>>
  >;
};

type CanvasNodeGhostIconButtonBinding5Props = {
  file: NonNullable<{
    file: AgentTurnSummaryFileVM;
    workspaceRoot?: string | null;
    basePath?: string | null;
    expanded: boolean;
    onLinkAction?: (action: WorkspaceLinkAction) => void;
    onToggle: () => void;
  }>["file"];
  onLinkAction: NonNullable<{
    file: AgentTurnSummaryFileVM;
    workspaceRoot?: string | null;
    basePath?: string | null;
    expanded: boolean;
    onLinkAction?: (action: WorkspaceLinkAction) => void;
    onToggle: () => void;
  }>["onLinkAction"];
  action:
    | import("../../../actions/workspaceLinkActions").OpenWorkspaceFileLinkAction
    | null;
};
