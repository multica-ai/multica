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
          renderSidebarActions
            ? (context) => renderSidebarActions({ ...context, purpose })
            : undefined
        }
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
