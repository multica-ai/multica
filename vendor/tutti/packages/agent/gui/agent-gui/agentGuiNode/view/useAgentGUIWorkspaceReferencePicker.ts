import { createRichTextMentionHref } from "@tutti-os/ui-rich-text/core";
import type {
  ReferenceLocateTarget,
  ReferenceNode,
  WorkspaceFileReference
} from "@tutti-os/workspace-file-reference/contracts";
import type { ReferenceGroupedSelection } from "@tutti-os/workspace-file-reference/ui";
import { useCallback, useMemo, useRef, useState } from "react";
import type { WorkspaceReferencePickResult } from "../AgentComposer";
import type {
  AgentContextMentionItem,
  AgentMentionWorkspaceReferenceItem
} from "../agentRichText/agentFileMentionExtension";
import type {
  AgentGUIComposerViewModel,
  AgentGUIDetailViewModel,
  AgentGUIInteractionViewModel,
  AgentGUIOperationsViewModel,
  AgentGUIRailViewModel,
  AgentGUIReadinessViewModel,
  AgentGUIShellViewModel
} from "../model/agentGuiNodeTypes";
import type { AgentGUINodeViewProps } from "./AgentGUINodeView.types";

interface Input {
  onWorkspaceFileReferencesAdded: AgentGUINodeViewProps["onWorkspaceFileReferencesAdded"];
  projectDirectorySourceAggregator: AgentGUINodeViewProps["projectDirectorySourceAggregator"];
  referenceSourceAggregator: AgentGUINodeViewProps["referenceSourceAggregator"];
  resolveMentionReferenceTarget: AgentGUINodeViewProps["resolveMentionReferenceTarget"];
  resolveWorkspaceReferenceInitialTarget: AgentGUINodeViewProps["resolveWorkspaceReferenceInitialTarget"];
  shell: AgentGUIShellViewModel;
  rail: AgentGUIRailViewModel;
  detail: AgentGUIDetailViewModel;
  composer: AgentGUIComposerViewModel;
  interaction: AgentGUIInteractionViewModel;
  readiness: AgentGUIReadinessViewModel;
  operations: AgentGUIOperationsViewModel;
  workspaceFileReferenceAdapter: AgentGUINodeViewProps["workspaceFileReferenceAdapter"];
  workspaceFileReferenceCopy: AgentGUINodeViewProps["workspaceFileReferenceCopy"];
}

export function useAgentGUIWorkspaceReferencePicker(input: Input) {
  const {
    onWorkspaceFileReferencesAdded,
    projectDirectorySourceAggregator,
    referenceSourceAggregator,
    resolveMentionReferenceTarget,
    resolveWorkspaceReferenceInitialTarget,
    shell,
    rail,
    detail,
    composer,
    interaction,
    readiness,
    operations,
    workspaceFileReferenceAdapter,
    workspaceFileReferenceCopy
  } = input;
  const [workspaceReferencePickerOpen, setWorkspaceReferencePickerOpen] =
    useState(false);
  const [workspaceReferencePickerPurpose, setWorkspaceReferencePickerPurpose] =
    useState<"directory" | "reference">("reference");
  // 打开引用 picker 时的定位目标(点任务/应用行的产物图标时设置;「+」按钮则为 null)。
  const [workspaceReferencePickerTarget, setWorkspaceReferencePickerTarget] =
    useState<ReferenceLocateTarget | null>(null);
  const workspaceReferencePickerResolverRef = useRef<
    ((result: WorkspaceReferencePickResult) => void) | null
  >(null);
  const projectDirectoryPickerResolverRef = useRef<
    ((result: { path: string } | null) => void) | null
  >(null);
  const emptyReferencePickResult: WorkspaceReferencePickResult = useMemo(
    () => ({ files: [], mentionItems: [] }),
    []
  );
  const hostLocalFileSourceId = "host-local-file";
  const isWorkspaceReferencePickerNodeSelectable = useCallback(
    (node: ReferenceNode) => {
      if (workspaceReferencePickerPurpose === "directory") {
        return node.kind === "folder";
      }
      return true;
    },
    [workspaceReferencePickerPurpose]
  );
  const requestWorkspaceReferences = useCallback(
    async (
      entity?: AgentContextMentionItem | null
    ): Promise<WorkspaceReferencePickResult> => {
      if (!workspaceFileReferenceAdapter && !referenceSourceAggregator) {
        return emptyReferencePickResult;
      }
      // 仅多源 picker(referenceSourceAggregator)支持定位;本地 picker 不支持。
      const target =
        entity && referenceSourceAggregator
          ? (resolveMentionReferenceTarget?.(entity) ?? null)
          : referenceSourceAggregator
            ? (resolveWorkspaceReferenceInitialTarget?.({
                activeConversation: rail.activeConversation,
                composerSelectedProjectPath:
                  composer.state.composerSettings.selectedProjectPath ?? null,
                userProjects: rail.userProjects
              }) ?? null)
            : null;
      setWorkspaceReferencePickerTarget(target);
      setWorkspaceReferencePickerPurpose("reference");
      setWorkspaceReferencePickerOpen(true);
      return await new Promise<WorkspaceReferencePickResult>((resolve) => {
        workspaceReferencePickerResolverRef.current?.(emptyReferencePickResult);
        projectDirectoryPickerResolverRef.current?.(null);
        projectDirectoryPickerResolverRef.current = null;
        workspaceReferencePickerResolverRef.current = resolve;
      });
    },
    [
      emptyReferencePickResult,
      referenceSourceAggregator,
      resolveMentionReferenceTarget,
      resolveWorkspaceReferenceInitialTarget,
      rail.activeConversation,
      composer.state.composerSettings.selectedProjectPath,
      rail.userProjects,
      workspaceFileReferenceAdapter,
      workspaceFileReferenceCopy
    ]
  );
  const requestProjectDirectory = useCallback(async () => {
    if (!projectDirectorySourceAggregator) {
      return null;
    }
    setWorkspaceReferencePickerTarget(null);
    setWorkspaceReferencePickerPurpose("directory");
    setWorkspaceReferencePickerOpen(true);
    return await new Promise<{ path: string } | null>((resolve) => {
      workspaceReferencePickerResolverRef.current?.(emptyReferencePickResult);
      workspaceReferencePickerResolverRef.current = null;
      projectDirectoryPickerResolverRef.current?.(null);
      projectDirectoryPickerResolverRef.current = resolve;
    });
  }, [emptyReferencePickResult, projectDirectorySourceAggregator]);
  const closeWorkspaceReferencePicker = useCallback(() => {
    workspaceReferencePickerResolverRef.current?.(emptyReferencePickResult);
    workspaceReferencePickerResolverRef.current = null;
    projectDirectoryPickerResolverRef.current?.(null);
    projectDirectoryPickerResolverRef.current = null;
    setWorkspaceReferencePickerOpen(false);
    setWorkspaceReferencePickerTarget(null);
    setWorkspaceReferencePickerPurpose("reference");
  }, [emptyReferencePickResult]);
  const settleReferencePicker = useCallback(
    (
      result: WorkspaceReferencePickResult,
      addedFiles: WorkspaceFileReference[]
    ) => {
      workspaceReferencePickerResolverRef.current?.(result);
      workspaceReferencePickerResolverRef.current = null;
      setWorkspaceReferencePickerOpen(false);
      setWorkspaceReferencePickerTarget(null);
      setWorkspaceReferencePickerPurpose("reference");
      if (addedFiles.length > 0) {
        void onWorkspaceFileReferencesAdded?.(addedFiles);
      }
    },
    [onWorkspaceFileReferencesAdded]
  );
  const confirmWorkspaceReferencePicker = useCallback(
    (refs: WorkspaceFileReference[]) => {
      if (workspaceReferencePickerPurpose === "directory") {
        const directory = refs.find((ref) => ref.kind === "folder") ?? null;
        projectDirectoryPickerResolverRef.current?.(
          directory ? { path: directory.path } : null
        );
        projectDirectoryPickerResolverRef.current = null;
        setWorkspaceReferencePickerOpen(false);
        setWorkspaceReferencePickerTarget(null);
        setWorkspaceReferencePickerPurpose("reference");
        return;
      }
      settleReferencePicker({ files: refs, mentionItems: [] }, refs);
    },
    [settleReferencePicker, workspaceReferencePickerPurpose]
  );
  // 「文件夹=一个 reference 节点」确认:navigable 源文件夹折叠成 workspace-reference
  // mention item(只携带可解析句柄 source+id+groupId,不展开文件);松散文件仍按 file
  // mention 插入。agent 收到 `mention://workspace-reference/...` 后经 skill+CLI 按需解析。
  const confirmWorkspaceReferenceBundles = useCallback(
    (result: ReferenceGroupedSelection) => {
      const workspaceRefs = result.files.filter(
        (ref) => ref.sourceId !== hostLocalFileSourceId
      );
      const mentionItems: AgentMentionWorkspaceReferenceItem[] = result.bundles
        .filter((bundle) => bundle.handle != null)
        .map((bundle) => {
          const handle = bundle.handle!;
          const bundleIconUrl = bundle.iconUrl ?? undefined;
          return {
            kind: "workspace-reference",
            href: createRichTextMentionHref({
              providerId: "workspace-reference",
              entityId: handle.id,
              label: bundle.displayName,
              scope: {
                workspaceId: shell.workspaceId,
                source: handle.source,
                ...(handle.groupId?.trim()
                  ? { groupId: handle.groupId.trim() }
                  : {}),
                ...(bundle.fileCount > 0
                  ? { count: String(bundle.fileCount) }
                  : {})
              }
            }),
            workspaceId: shell.workspaceId,
            targetId: handle.id,
            source: handle.source,
            ...(handle.groupId ? { groupId: handle.groupId } : {}),
            name: bundle.displayName,
            iconUrl: bundleIconUrl,
            fileCount: bundle.fileCount
          };
        });
      // bundle 不再展开文件,仅松散文件计入「最近引用」跟踪。
      settleReferencePicker(
        { files: result.files, mentionItems },
        workspaceRefs
      );
    },
    [hostLocalFileSourceId, settleReferencePicker]
  );

  return {
    closeWorkspaceReferencePicker,
    confirmWorkspaceReferenceBundles,
    confirmWorkspaceReferencePicker,
    isWorkspaceReferencePickerNodeSelectable,
    requestProjectDirectory,
    requestWorkspaceReferences,
    workspaceReferencePickerAggregator:
      workspaceReferencePickerPurpose === "directory"
        ? (projectDirectorySourceAggregator ?? null)
        : (referenceSourceAggregator ?? null),
    workspaceReferencePickerOpen,
    workspaceReferencePickerPurpose,
    workspaceReferencePickerTarget
  };
}
