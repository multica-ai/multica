import { Fragment, useLayoutEffect, useMemo, useState, type JSX, type ReactNode } from "react";
import { observer } from "mobx-react-lite";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import type { TranscriptPresentationModel } from "../../../state/TranscriptPresentationModel";
import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";
import { useAgentObservationGap } from "../AgentObservationGapContext";
import { AgentForkThroughTurnButton, AgentForkThroughTurnFooter } from "./AgentForkThroughTurnButton";
import { AgentTranscriptItemContent, AgentTranscriptItemView } from "./AgentTranscriptItemView";
import { useAgentTurnDisclosureStore } from "./AgentTurnDisclosureContext";
import { AgentTurnWorkSection } from "./AgentTurnWorkSection";
import { findLastMessageRowIndex, isAssistantParticipantContentRow, transcriptRowKey } from "./agentTranscriptModel";
import { resolveAgentTranscriptEditRetryRowId } from "./useAgentTranscriptEditRetryProjection";
import type { AgentTranscriptViewProps } from "./AgentTranscriptView";
import type { RenderAgentTranscriptTurn } from "./AgentTranscriptViewport";

export type TranscriptContentOptions = Pick<AgentTranscriptViewProps,
  "labels" | "editRetry" | "onLinkAction" | "onAuthLogin" | "onForkThroughTurn" |
  "availableSkills" | "workspaceAppIcons" | "showRawTimelineJson" | "participantPresentation"
>;

export const AgentTranscriptGroup = observer(function AgentTranscriptGroup({
  model, groupKey, options, onDisclosureMotionChange
}: {
  model: TranscriptPresentationModel;
  groupKey: string;
  options: TranscriptContentOptions;
  onDisclosureMotionChange: Parameters<RenderAgentTranscriptTurn>[1];
}) {
  const group = model.group(groupKey);
  const baseSection = model.workSection(groupKey);
  const active = model.isActive(groupKey);
  const gap = useAgentObservationGap(model.sessionId, active ? group?.turnId : null);
  const disclosureStore = useAgentTurnDisclosureStore();
  const rows = useMemo(() => group?.rows.map(({ row }) => row) ?? [], [group]);
  const editRetry = options.editRetry?.agentSessionId.trim() === model.sessionId.trim() ? options.editRetry : undefined;
  const editableId = resolveAgentTranscriptEditRetryRowId(rows, editRetry?.eligibleTurnId);
  const forkId = options.onForkThroughTurn ? model.forkTurnId(groupKey) : null;
  const onFork = usePersistCallback(() => {
    const id = model.forkTurnId(groupKey);
    if (id) options.onForkThroughTurn?.(id);
  });
  const onToolExpandedChange = usePersistCallback((key: string, expanded: boolean) => model.setToolExpanded(key, expanded));
  const section = useMemo(() => {
    if (!baseSection || baseSection.timing.kind !== "live" || !gap) return baseSection;
    const frozen = Number.isFinite(gap.startedAtUnixMs) && gap.startedAtUnixMs >= baseSection.timing.startedAtUnixMs;
    return { ...baseSection, timing: {
      ...baseSection.timing,
      ...(frozen ? { frozenAtUnixMs: gap.startedAtUnixMs } : {}),
      ...(gap.presentationState ? { observationGapPresentationState: gap.presentationState } : {})
    } };
  }, [baseSection, gap]);
  const footerIndex = group ? findLastMessageRowIndex(group.rows) : null;
  const forkButton = useMemo(() => forkId ? (
    <AgentForkThroughTurnButton agentSessionId={model.sessionId} turnId={forkId} onFork={onFork} />
  ) : null, [model, forkId, onFork]);
  const forkFooter = useMemo(() => forkId && footerIndex === null ? (
    <AgentForkThroughTurnFooter agentSessionId={model.sessionId} turnId={forkId} onFork={onFork} />
  ) : null, [model, forkId, footerIndex, onFork]);
  const renderRow = useMemo(() => (row: AgentTranscriptRowVM, rowIndex: number, renderKey?: string) => (
    <AgentTranscriptRow
      key={`${model.sessionId}\u0000${renderKey ?? transcriptRowKey(row)}`}
      model={model}
      groupKey={groupKey}
      row={row}
      rowIndex={rowIndex}
      renderKey={renderKey}
      options={options}
      editRetry={row.id === editableId ? editRetry : undefined}
      active={active && row.turnId !== null && row.turnId === group?.turnId}
      processingPaused={active && row.turnId !== null && row.turnId === group?.turnId && gap !== null}
      onToolExpandedChange={onToolExpandedChange}
      footer={rowIndex === footerIndex ? forkButton : null}
      legacy={!section}
    />
  ), [model, groupKey, group, options, editableId, editRetry, active, gap, onToolExpandedChange, footerIndex, forkButton, section]);
  if (!group) return null;
  return section ? (
    <AgentTurnWorkSection
      model={section}
      sessionId={model.sessionId}
      turnKey={group.turnId ?? group.key}
      showDivider={model.showDivider(groupKey)}
      disclosureStore={disclosureStore}
      onDisclosureMotionChange={onDisclosureMotionChange}
      renderRow={renderRow}
      footer={forkFooter}
    />
  ) : (
    <>
      {group.rows.map(({ row, rowIndex }) => renderRow(row, rowIndex))}
      {forkFooter}
    </>
  );
});

const AgentTranscriptRow = observer(function AgentTranscriptRow({
  model, groupKey, row, rowIndex, renderKey, options, editRetry,
  active, processingPaused, onToolExpandedChange, footer, legacy
}: {
  model: TranscriptPresentationModel;
  groupKey: string;
  row: AgentTranscriptRowVM;
  rowIndex: number;
  renderKey?: string;
  options: TranscriptContentOptions;
  editRetry: AgentTranscriptViewProps["editRetry"];
  active: boolean;
  processingPaused: boolean;
  onToolExpandedChange: (key: string, expanded: boolean) => void;
  footer: ReactNode;
  legacy: boolean;
}) {
  const key = renderKey ?? transcriptRowKey(row);
  const context = model.context;
  const header = model.participantMode && model.headerShown(groupKey, key);
  const participantContent = model.participantMode && !header && isAssistantParticipantContentRow(row) ? "assistant" : undefined;
  const entering = row.kind !== "processing" && model.rowEntering(groupKey, key);
  const [animateEnter, setAnimateEnter] = useState(entering);
  useLayoutEffect(() => {
    model.markRowPresented(row);
  }, [model, row]);
  const finishEntrance = usePersistCallback<React.AnimationEventHandler<HTMLDivElement>>((event) => {
    if (event.target === event.currentTarget) setAnimateEnter(false);
  });
  return (
    <Fragment>
      {legacy && model.rowHasDivider(groupKey, rowIndex) ? (
        <div className="h-px w-full flex-none bg-[var(--line-2,var(--tutti-line-2))]" data-testid="agent-transcript-turn-divider" aria-hidden="true" />
      ) : null}
      <div
        className="agent-gui-transcript-row"
        data-agent-transcript-row={key}
        data-agent-transcript-turn-completed={model.isCompleted(groupKey) ? "true" : undefined}
        data-agent-transcript-row-kind={row.kind}
        data-agent-transcript-row-speaker={row.kind === "message" ? row.speaker : undefined}
        data-agent-transcript-row-thinking-first={row.kind === "message" && row.speaker === "assistant" && row.thinking.length > 0 ? "true" : undefined}
        data-agent-transcript-row-thinking-last={row.kind === "message" && row.speaker === "assistant" && row.thinking.length > 0 && row.messages.length === 0 ? "true" : undefined}
        data-agent-transcript-row-index={model.rowOffset(groupKey) + rowIndex}
        data-agent-transcript-row-participant-content={participantContent}
        data-agent-transcript-row-enter={animateEnter ? "true" : undefined}
        onAnimationEnd={finishEntrance}
      >
        <AgentTranscriptBoundItem
          row={row}
          model={model}
          workspaceRoot={context.workspaceRoot}
          basePath={context.cwd}
          provider={context.provider}
          editRetry={editRetry}
          labels={options.labels}
          onLinkAction={options.onLinkAction}
          onAuthLogin={options.onAuthLogin}
          availableSkills={options.availableSkills}
          workspaceAppIcons={options.workspaceAppIcons}
          showRawTimelineJson={options.showRawTimelineJson}
          participantPresentation={options.participantPresentation}
          showParticipantHeader={header}
          isActiveTurn={active}
          processingPaused={processingPaused}
          toolGroupExpanded={row.kind === "tool-group" ? model.toolExpanded(transcriptRowKey(row)) : undefined}
          toolGroupExpansionKey={row.kind === "tool-group" ? transcriptRowKey(row) : undefined}
          onToolGroupExpandedChange={onToolExpandedChange}
          footerAction={footer}
        />
      </div>
    </Fragment>
  );
});

const AgentTranscriptBoundItem = observer(function AgentTranscriptBoundItem({
  row, model, ...props
}: import("./AgentTranscriptItemView").AgentTranscriptItemViewProps & { model: TranscriptPresentationModel }) {
  return model.rowsModel ? (
    <AgentTranscriptItemView {...props} rowsModel={model.rowsModel} agentSessionId={model.sessionId} rowKey={transcriptRowKey(row)} descriptor={row} />
  ) : <AgentTranscriptItemContent {...props} row={row} />;
});
