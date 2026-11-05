import { observer } from "mobx-react-lite";
import { memo, useCallback, type JSX, type ReactNode } from "react";
import type { AgentGUIProviderSkillOption } from "../../../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type { WorkspaceLinkAction } from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import type { ConversationRowsModel } from "../../../state/ConversationRowsModel";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../AgentMessageMarkdown";
import { resolveAgentConversationLinkAction } from "../actions/agentConversationLinkActions";
import type { AgentConversationParticipantPresentation } from "../contracts/agentConversationParticipantPresentation";
import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";
import { AgentGeneratedImageRow } from "./AgentGeneratedImageRow";
import { AgentMessageBlock } from "./AgentMessageBlock";
import { AgentProcessingRow } from "./AgentProcessingRow";
import { AgentToolGroupRow } from "./AgentToolGroupRow";
import { AgentTurnSummaryRow } from "./AgentTurnSummaryRow";
import type { AgentUserMessageEditRetryControl } from "./AgentUserMessageEditRetry";

export interface AgentTranscriptItemViewProps {
  workspaceRoot: string | null;
  basePath: string;
  row: AgentTranscriptRowVM;
  editRetry?: AgentUserMessageEditRetryControl;
  labels: {
    toolCallsLabel: (count: number) => string;
    thinkingLabel: string;
    processing: string;
    turnSummary: string;
    rawTimelineJson?: string;
  };
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onAuthLogin?: (provider?: string | null) => void;
  provider?: string | null;
  availableSkills?: readonly AgentGUIProviderSkillOption[];
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  showRawTimelineJson?: boolean;
  participantPresentation?: AgentConversationParticipantPresentation;
  showParticipantHeader?: boolean;
  isActiveTurn?: boolean;
  processingPaused?: boolean;
  toolGroupExpanded?: boolean;
  toolGroupExpansionKey?: string;
  onToolGroupExpandedChange?: (key: string, expanded: boolean) => void;
  footerAction?: ReactNode;
}

export const AgentTranscriptItemContent = memo(
  function AgentTranscriptItemContent({
    workspaceRoot,
    basePath,
    row,
    editRetry,
    labels,
    onLinkAction,
    onAuthLogin,
    provider,
    availableSkills,
    workspaceAppIcons,
    showRawTimelineJson = false,
    participantPresentation,
    showParticipantHeader,
    isActiveTurn = false,
    processingPaused = false,
    toolGroupExpanded,
    toolGroupExpansionKey,
    onToolGroupExpandedChange,
    footerAction
  }: AgentTranscriptItemViewProps): JSX.Element {
    "use memo";

    const handleLinkClick = useCallback(
      (href: string) => {
        const action = resolveAgentConversationLinkAction({
          workspaceRoot,
          basePath,
          href,
          source: "agent-markdown"
        });
        if (action) {
          onLinkAction?.(action);
        }
      },
      [basePath, onLinkAction, workspaceRoot]
    );
    switch (row.kind) {
      case "generated-image":
        return <AgentGeneratedImageRow row={row} />;
      case "message":
        return (
          <AgentMessageBlock
            workspaceRoot={workspaceRoot}
            basePath={basePath}
            row={row}
            editRetry={editRetry}
            onLinkAction={onLinkAction}
            onAuthLogin={onAuthLogin}
            provider={provider}
            availableSkills={availableSkills}
            workspaceAppIcons={workspaceAppIcons}
            thinkingLabel={labels.thinkingLabel}
            toolCallsLabel={labels.toolCallsLabel}
            showRawTimelineJson={showRawTimelineJson}
            rawTimelineJsonLabel={labels.rawTimelineJson}
            participantPresentation={participantPresentation}
            showParticipantHeader={showParticipantHeader}
            isActiveTurn={isActiveTurn}
            footerAction={footerAction}
          />
        );
      case "tool-group":
        return (
          <AgentToolGroupRow
            row={row}
            label={labels.toolCallsLabel}
            thinkingLabel={labels.thinkingLabel}
            onLinkClick={handleLinkClick}
            showRawTimelineJson={showRawTimelineJson}
            rawTimelineJsonLabel={labels.rawTimelineJson}
            expanded={row.grouped ? toolGroupExpanded : undefined}
            onExpandedChange={
              row.grouped ? onToolGroupExpandedChange : undefined
            }
            expansionKey={toolGroupExpansionKey}
          />
        );
      case "turn-summary":
        return (
          <AgentTurnSummaryRow
            row={row}
            workspaceRoot={workspaceRoot}
            basePath={basePath}
            label={labels.turnSummary}
            onLinkAction={onLinkAction}
          />
        );
      case "processing":
        return (
          <AgentProcessingRow
            row={row}
            label={labels.processing}
            paused={processingPaused}
          />
        );
    }
  }
);

export const AgentTranscriptItemView = observer(
  function AgentTranscriptItemView({
    agentSessionId,
    rowKey,
    descriptor,
    rowsModel,
    ...props
  }: Omit<AgentTranscriptItemViewProps, "row"> & {
    agentSessionId: string;
    rowKey: string;
    descriptor: AgentTranscriptRowVM;
    rowsModel: ConversationRowsModel;
  }) {
    const row = rowsModel.resolve(descriptor);
    return row ? <AgentTranscriptItemContent {...props} row={row} /> : null;
  }
);
