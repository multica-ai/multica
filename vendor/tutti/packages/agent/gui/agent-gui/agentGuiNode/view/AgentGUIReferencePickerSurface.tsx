import { useMemo } from "react";
import type { WorkspaceFileManagerI18nRuntime } from "@tutti-os/workspace-file-manager";
import type {
  WorkspaceFileReferenceAdapter,
  WorkspaceFileReferenceCopy
} from "@tutti-os/workspace-file-reference/contracts";
import {
  ReferenceSourcePicker,
  WorkspaceFileReferencePicker,
  type ReferenceSourcePickerProps
} from "@tutti-os/workspace-file-reference/ui";
import { observer } from "mobx-react-lite";
import type { ReactNode } from "react";

export interface AgentGUIReferencePickerSurfaceProps {
  aggregator: ReferenceSourcePickerProps["aggregator"] | null;
  copy: WorkspaceFileReferenceCopy;
  fileAdapter: WorkspaceFileReferenceAdapter | null;
  fileManagerCopy: WorkspaceFileManagerI18nRuntime | null;
  initialPath: string | null | undefined;
  initialTarget: ReferenceSourcePickerProps["initialTarget"];
  isNodeSelectable: ReferenceSourcePickerProps["isNodeSelectable"];
  open: boolean;
  purpose: "directory" | "reference";
  renderDirectoryHeaderActions?: ReferenceSourcePickerProps["renderHeaderActions"];
  renderSidebarActions?: (
    context: Parameters<
      NonNullable<ReferenceSourcePickerProps["renderSidebarActions"]>
    >[0] & { purpose: "directory" | "reference" }
  ) => ReactNode;
  resolveContentErrorAction: ReferenceSourcePickerProps["resolveContentErrorAction"];
  resolveEntryIconUrl: ReferenceSourcePickerProps["resolveEntryIconUrl"];
  workspaceId: string;
  onClose: ReferenceSourcePickerProps["onClose"];
  onConfirm: ReferenceSourcePickerProps["onConfirm"];
  onConfirmBundles: ReferenceSourcePickerProps["onConfirmBundles"];
}

export const AgentGUIReferencePickerSurface = observer(
  function AgentGUIReferencePickerSurface({
    aggregator,
    copy,
    fileAdapter,
    fileManagerCopy,
    initialPath,
    initialTarget,
    isNodeSelectable,
    open,
    purpose,
    renderDirectoryHeaderActions,
    renderSidebarActions,
    resolveContentErrorAction,
    resolveEntryIconUrl,
    workspaceId,
    onClose,
    onConfirm,
    onConfirmBundles
  }: AgentGUIReferencePickerSurfaceProps): React.JSX.Element {
    return aggregator ? (
      <ReferenceSourcePickerBinding1
        aggregator={aggregator}
        copy={copy}
        initialTarget={initialTarget}
        isNodeSelectable={isNodeSelectable}
        fileManagerCopy={fileManagerCopy}
        open={open}
        purpose={purpose}
        renderDirectoryHeaderActions={renderDirectoryHeaderActions}
        renderSidebarActions={renderSidebarActions}
        resolveContentErrorAction={resolveContentErrorAction}
        resolveEntryIconUrl={resolveEntryIconUrl}
        workspaceId={workspaceId}
        onClose={onClose}
        onConfirm={onConfirm}
        onConfirmBundles={onConfirmBundles}
      />
    ) : (
      <WorkspaceFileReferencePicker
        copy={copy}
        fileAdapter={fileAdapter ?? undefined}
        initialPath={initialPath}
        open={open}
        scoped
        workspaceId={workspaceId}
        onClose={onClose}
        onConfirm={onConfirm}
      />
    );
  }
);

const ReferenceSourcePickerBinding1 = observer(
  function ReferenceSourcePickerBinding1({
    aggregator,
    copy,
    initialTarget,
    isNodeSelectable,
    fileManagerCopy,
    open,
    purpose,
    renderDirectoryHeaderActions,
    renderSidebarActions,
    resolveContentErrorAction,
    resolveEntryIconUrl,
    workspaceId,
    onClose,
    onConfirm,
    onConfirmBundles
  }: ReferenceSourcePickerBinding1Props) {
    const renderSidebarActionsProp1 = useMemo<
      NonNullable<
        React.ComponentProps<
          typeof ReferenceSourcePicker
        >["renderSidebarActions"]
      >
    >(
      () => (context) => renderSidebarActions!({ ...context, purpose }),
      [purpose, renderSidebarActions]
    );
    return (
      <ReferenceSourcePicker
        aggregator={aggregator}
        copy={copy}
        initialTarget={initialTarget}
        isNodeSelectable={isNodeSelectable}
        fileManagerCopy={fileManagerCopy ?? undefined}
        open={open}
        purpose={purpose}
        renderHeaderActions={
          purpose === "directory" ? renderDirectoryHeaderActions : undefined
        }
        renderSidebarActions={
          renderSidebarActions ? renderSidebarActionsProp1 : undefined
        }
        resolveContentErrorAction={resolveContentErrorAction}
        resolveEntryIconUrl={resolveEntryIconUrl}
        workspaceId={workspaceId}
        onClose={onClose}
        onConfirm={onConfirm}
        onConfirmBundles={onConfirmBundles}
      />
    );
  }
);

type ReferenceSourcePickerBinding1Props = {
  aggregator: import("../../../../../workspace/file-reference/src/core/referenceSourceAggregator").ReferenceSourceAggregator;
  copy: WorkspaceFileReferenceCopy;
  initialTarget:
    | import("../../../../../workspace/file-reference/src/contracts/referenceSource").ReferenceLocateTarget
    | null
    | undefined;
  isNodeSelectable:
    | ((
        node: import("../../../../../workspace/file-reference/src/contracts/referenceSource").ReferenceNode
      ) => boolean)
    | undefined;
  fileManagerCopy: WorkspaceFileManagerI18nRuntime | null;
  open: boolean;
  purpose: "directory" | "reference";
  renderDirectoryHeaderActions:
    | ((context: {
        refresh: () => void;
        selectTarget: (
          target: import("../../../../../workspace/file-reference/src/contracts/referenceSource").ReferenceLocateTarget
        ) => Promise<boolean>;
        selectTargets: (
          targets: readonly import("../../../../../workspace/file-reference/src/contracts/referenceSource").ReferenceLocateTarget[]
        ) => Promise<number>;
      }) => ReactNode)
    | undefined;
  renderSidebarActions:
    | ((
        context: Parameters<
          NonNullable<ReferenceSourcePickerProps["renderSidebarActions"]>
        >[0] & { purpose: "directory" | "reference" }
      ) => ReactNode)
    | undefined;
  resolveContentErrorAction:
    | ((
        error: Error
      ) =>
        | import("../../../../../workspace/file-reference/src/ui/index").ReferenceSourceContentErrorAction
        | null)
    | undefined;
  resolveEntryIconUrl:
    | ((
        entry: import("../../../../../workspace/file-manager/src/index").WorkspaceFileEntry
      ) => Promise<string | null | undefined>)
    | undefined;
  workspaceId: string;
  onClose: () => void;
  onConfirm: (
    refs: import("../../../../../workspace/file-reference/src/contracts/index").WorkspaceFileReference[]
  ) => void;
  onConfirmBundles:
    | ((
        result: import("../../../../../workspace/file-reference/src/ui/index").ReferenceGroupedSelection
      ) => void)
    | undefined;
};
