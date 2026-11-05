import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import type { AgentActivitySessionForkLineage } from "@tutti-os/agent-activity-core";
import { ScrollArea } from "@tutti-os/ui-system/components";
import { GitFork } from "lucide-react";
import {
  memo,
  useMemo,
  useState,
  type CSSProperties,
  type ReactNode,
  type Ref,
  type RefObject
} from "react";
import type { WorkspaceLinkAction } from "../../../actions/workspaceLinkActions";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../../shared/AgentMessageMarkdown";
import type { AgentConversationFollowEndMode } from "../../../shared/agentConversation/agentConversationFollowEndController";
import type { AgentTranscriptVirtualScrollController } from "../../../shared/agentConversation/components/AgentTranscriptView";
import type { AgentTranscriptEditRetryControl } from "../../../shared/agentConversation/components/useAgentTranscriptEditRetryProjection";
import styles from "../AgentGUINode.styles";
import type { AgentGUIEditRetryRecoveryAction } from "../controller/agentGuiController.promptOperations";
import type { AgentGUIEditRetryPresentation } from "../model/agentGUIEditRetryModel";
import type { AgentGUIProviderSkillOption } from "../model/agentGuiNodeTypes";
import { AgentGUIConversationTimelinePane } from "./AgentGUIConversationTimelinePane";
import { AgentGUIEditRetryStatus } from "./AgentGUIEditRetryStatus";
import {
  AgentGUITextSelectionActions,
  readAgentGUITextSelection,
  type AgentGUITextSelectionSnapshot
} from "./AgentGUITextSelectionActions";
import { observer } from "mobx-react-lite";

const TIMELINE_CONTENT_STYLE: CSSProperties = {
  width: "100%",
  minWidth: "100%",
  display: "grid",
  gridTemplateColumns: "minmax(0, 1fr)",
  gap: "24px"
};

interface AgentGUIDetailTimelineProps {
  availableSkills: readonly AgentGUIProviderSkillOption[];
  agentSessionId: string | null;
  editRetry?: {
    control?: AgentTranscriptEditRetryControl;
    presentation: AgentGUIEditRetryPresentation;
    recover: (action: AgentGUIEditRetryRecoveryAction) => Promise<void>;
  };
  conversationFlowEmpty: React.JSX.Element;
  conversationFlowLabels: {
    thinkingLabel: string;
    toolCallsLabel: (count: number) => string;
    processing: string;
    turnSummary: string;
    userMessageLocator: string;
  };
  hasActiveConversation: boolean;
  followEndMode: AgentConversationFollowEndMode;
  forkedFrom?: AgentActivitySessionForkLineage | null;

  homeContent: ReactNode;
  isLoadingOlderMessages: boolean;
  isVisible: boolean;
  isTimelineScrolledToTop: boolean;
  labels: {
    loadingConversation: string;
    continuedFromTask: string;
    selectionAddToConversation: string;
    selectionAskInSide: string;
  };
  onAuthLogin?: (provider?: string | null) => void;
  onForkThroughTurn?: (turnId: string) => void;
  onOpenForkSourceSession?: (agentSessionId: string) => void;
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onAddSelectionToConversation: (text: string) => void;
  onAskSelectionInSide?: (text: string) => void;
  textSelectionActionsEnabled?: boolean;
  showTimelineSkeleton: boolean;
  showUnavailableChatEmpty: boolean;
  timelineContentRef: RefObject<HTMLDivElement | null>;
  timelineRef: RefObject<HTMLDivElement | null>;
  virtualScrollControllerRef: Ref<AgentTranscriptVirtualScrollController>;
  workspaceAppIcons: readonly AgentMessageMarkdownWorkspaceAppIcon[];
}

export const AgentGUIDetailTimeline = observer(function AgentGUIDetailTimeline({
  availableSkills,
  agentSessionId,
  editRetry,
  conversationFlowEmpty,
  conversationFlowLabels,
  hasActiveConversation,
  followEndMode,
  forkedFrom,

  homeContent,
  isLoadingOlderMessages,
  isVisible,
  isTimelineScrolledToTop,
  labels,
  onAuthLogin,
  onForkThroughTurn,
  onOpenForkSourceSession,
  onLinkAction,
  onAddSelectionToConversation,
  onAskSelectionInSide,
  textSelectionActionsEnabled = true,
  showTimelineSkeleton,
  showUnavailableChatEmpty,
  timelineContentRef,
  timelineRef,
  virtualScrollControllerRef,
  workspaceAppIcons
}: AgentGUIDetailTimelineProps): React.JSX.Element {
  "use memo";
  const [textSelection, setTextSelection] =
    useState<AgentGUITextSelectionSnapshot | null>(null);
  const forkLineageAttachments = useMemo(
    () =>
      forkedFrom?.targetTurnId
        ? [
            {
              id: `fork-lineage:${forkedFrom.operationId}`,
              anchorTurnId: forkedFrom.targetTurnId,
              missingAnchorBehavior: "hide" as const,
              content: (
                <div
                  className="my-3 flex w-full items-center gap-3 text-[14px] font-medium text-[var(--tutti-purple)]"
                  data-testid="agent-gui-fork-lineage"
                >
                  <span
                    className="h-px min-w-0 flex-1 bg-[var(--line-2,var(--tutti-line-2))]"
                    aria-hidden="true"
                  />
                  <ButtonBinding1
                    onOpenForkSourceSession={onOpenForkSourceSession}
                    forkedFrom={forkedFrom}
                    labels={labels}
                  />
                  <span
                    className="h-px min-w-0 flex-1 bg-[var(--line-2,var(--tutti-line-2))]"
                    aria-hidden="true"
                  />
                </div>
              )
            }
          ]
        : [],
    [
      forkedFrom?.operationId,
      forkedFrom?.sourceAgentSessionId,
      forkedFrom?.targetTurnId,
      labels.continuedFromTask,
      onOpenForkSourceSession
    ]
  );
  const viewportPropsProp1Value1 = usePersistCallback<
    NonNullable<
      NonNullable<
        React.ComponentProps<typeof ScrollArea>["viewportProps"]
      >["onMouseUp"]
    >
  >((event) =>
    setTextSelection(readAgentGUITextSelection(event.currentTarget))
  );
  const viewportPropsProp1Value2 = usePersistCallback<
    NonNullable<
      NonNullable<
        React.ComponentProps<typeof ScrollArea>["viewportProps"]
      >["onPointerDown"]
    >
  >(() => setTextSelection(null));
  const viewportPropsProp1Value3 = usePersistCallback<
    NonNullable<
      NonNullable<
        React.ComponentProps<typeof ScrollArea>["viewportProps"]
      >["onScroll"]
    >
  >(() => setTextSelection(null));
  const viewportPropsProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof ScrollArea>["viewportProps"]>
  >(
    () => ({
      onMouseUp: viewportPropsProp1Value1,
      onPointerDown: viewportPropsProp1Value2,
      onScroll: viewportPropsProp1Value3
    }),
    [
      viewportPropsProp1Value1,
      viewportPropsProp1Value2,
      viewportPropsProp1Value3
    ]
  );
  return (
    <>
      <ScrollArea
        scrollbarMode="native"
        className="flex h-full min-h-0 flex-1 flex-col [&_[data-orientation=vertical][data-slot=scroll-area-scrollbar]]:opacity-100"
        viewportRef={timelineRef}
        viewportContentRef={timelineContentRef}
        viewportTestId="agent-gui-timeline"
        viewportClassName={`${styles.timeline} ${
          hasActiveConversation
            ? styles.timelineWithComposer
            : styles.timelineCentered
        } ${
          !isTimelineScrolledToTop ? styles.timelineScrolledFromTop : ""
        } ${showUnavailableChatEmpty ? styles.timelineUnavailableChatEmpty : ""}`.trim()}
        viewportContentStyle={TIMELINE_CONTENT_STYLE}
        viewportProps={viewportPropsProp1}
      >
        {hasActiveConversation ? (
          <>
            <AgentGUIConversationTimelinePane
              agentSessionId={agentSessionId}
              turnAttachments={forkLineageAttachments}
              editRetry={editRetry?.control}
              followEndMode={followEndMode}
              isLoading={showTimelineSkeleton}
              isLoadingOlderMessages={isLoadingOlderMessages}
              isVisible={isVisible}
              loadingLabel={labels.loadingConversation}
              empty={conversationFlowEmpty}
              onLinkAction={onLinkAction}
              onAuthLogin={onAuthLogin}
              onForkThroughTurn={onForkThroughTurn}
              availableSkills={availableSkills}
              workspaceAppIcons={workspaceAppIcons}
              labels={conversationFlowLabels}
              virtualScrollControllerRef={virtualScrollControllerRef}
            />
            {editRetry ? (
              <AgentGUIEditRetryStatus
                presentation={editRetry.presentation}
                onRecover={editRetry.recover}
              />
            ) : null}
          </>
        ) : (
          homeContent
        )}
      </ScrollArea>
      {textSelectionActionsEnabled &&
      hasActiveConversation &&
      textSelection &&
      timelineRef.current ? (
        <AgentGUITextSelectionActionsBinding2
          labels={labels}
          textSelection={textSelection}
          timelineRef={timelineRef}
          onAddSelectionToConversation={onAddSelectionToConversation}
          onAskSelectionInSide={onAskSelectionInSide}
          setTextSelection={setTextSelection}
        />
      ) : null}
    </>
  );
});

const ButtonBinding1 = observer(function ButtonBinding1({
  onOpenForkSourceSession,
  forkedFrom,
  labels,
  ...slotProps
}: ButtonBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding1Props>) {
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => onOpenForkSourceSession?.(forkedFrom.sourceAgentSessionId));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        className="flex shrink-0 cursor-pointer items-center gap-2 rounded-md px-1 py-0.5 transition-opacity hover:opacity-80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--tutti-purple)]"
        onClick={onClickProp2}
      >
        <GitFork width={16} height={16} aria-hidden="true" />
        {labels.continuedFromTask}
      </button>
    </Slot.Root>
  );
});

const AgentGUITextSelectionActionsBinding2 = observer(
  function AgentGUITextSelectionActionsBinding2({
    labels,
    textSelection,
    timelineRef,
    onAddSelectionToConversation,
    onAskSelectionInSide,
    setTextSelection
  }: AgentGUITextSelectionActionsBinding2Props) {
    const labelsProp3Value1 = labels.selectionAddToConversation;
    const labelsProp3Value2 = labels.selectionAskInSide;
    const labelsProp3 = useMemo<
      NonNullable<
        React.ComponentProps<typeof AgentGUITextSelectionActions>["labels"]
      >
    >(
      () => ({
        addToConversation: labelsProp3Value1,
        askInSide: labelsProp3Value2
      }),
      [labelsProp3Value1, labelsProp3Value2]
    );
    const onDismissProp4 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof AgentGUITextSelectionActions>["onDismiss"]
      >
    >(() => setTextSelection(null));
    return (
      <AgentGUITextSelectionActions
        labels={labelsProp3}
        snapshot={textSelection}
        portalTarget={timelineRef.current!.ownerDocument.body}
        onAddToConversation={onAddSelectionToConversation}
        onAskInSide={onAskSelectionInSide}
        onDismiss={onDismissProp4}
      />
    );
  }
);

type ButtonBinding1Props = {
  onOpenForkSourceSession: NonNullable<AgentGUIDetailTimelineProps>["onOpenForkSourceSession"];
  forkedFrom: AgentActivitySessionForkLineage;
  labels: NonNullable<AgentGUIDetailTimelineProps>["labels"];
};

type AgentGUITextSelectionActionsBinding2Props = {
  labels: NonNullable<AgentGUIDetailTimelineProps>["labels"];
  textSelection: AgentGUITextSelectionSnapshot;
  timelineRef: NonNullable<AgentGUIDetailTimelineProps>["timelineRef"];
  onAddSelectionToConversation: NonNullable<AgentGUIDetailTimelineProps>["onAddSelectionToConversation"];
  onAskSelectionInSide: NonNullable<AgentGUIDetailTimelineProps>["onAskSelectionInSide"];
  setTextSelection: import("react").Dispatch<
    import("react").SetStateAction<AgentGUITextSelectionSnapshot | null>
  >;
};
