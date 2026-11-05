import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { Button } from "@tutti-os/ui-system/components";
import { ChevronDownIcon } from "@tutti-os/ui-system/icons";
import { observer } from "mobx-react-lite";
import { useCallback, useRef, type JSX, type ReactNode } from "react";
import { useTranslation } from "../../../i18n/index";
import type { AgentTurnDisclosureStore } from "./AgentTurnDisclosureContext";
import {
  formatAgentTurnDuration,
  type AgentTurnDuration,
  type AgentTurnTiming,
  type AgentTurnWorkSectionModel,
  type AgentTurnWorkSectionRow
} from "./agentTurnWorkSectionModel";
import { CollapsibleReveal } from "./CollapsibleReveal";
import { useElapsedSeconds } from "./useElapsedSeconds";
const AgentTurnDurationLabel = observer(AgentTurnDurationLabelRender);

interface AgentTurnWorkSectionProps {
  model: AgentTurnWorkSectionModel;
  sessionId: string;
  turnKey: string;
  showDivider?: boolean;
  disclosureStore: AgentTurnDisclosureStore;
  onDisclosureMotionChange?: (
    turnKey: string,
    active: boolean,
    anchorElement?: HTMLElement | null
  ) => void;
  footer?: ReactNode;
  renderRow: (
    row: AgentTurnWorkSectionRow["row"],
    rowIndex: number,
    renderKey?: string
  ) => JSX.Element;
}

export const AgentTurnWorkSection = observer(function AgentTurnWorkSection({
  model,
  sessionId,
  turnKey,
  showDivider = false,
  disclosureStore,
  onDisclosureMotionChange,
  renderRow,
  footer
}: AgentTurnWorkSectionProps): JSX.Element {
  const { t } = useTranslation();
  const disclosureKey = `${sessionId}:${turnKey}`;
  const expanded = model.collapseEligible
    ? (disclosureStore.expandedOverrides[disclosureKey] ?? false)
    : true;

  const toggleLabel = expanded
    ? t("agentHost.agentGui.collapseTurnWork")
    : t("agentHost.agentGui.expandTurnWork");
  const pendingRevealTransitionsRef = useRef(0);
  const disclosureMotionActiveRef = useRef(false);
  const collapsibleSectionCount = model.sections.reduce(
    (count, section) =>
      count + (section.kind === "work" && model.collapseEligible ? 1 : 0),
    0
  );
  const finishDisclosureMotion = useCallback(() => {
    if (!disclosureMotionActiveRef.current) {
      return;
    }
    disclosureMotionActiveRef.current = false;
    pendingRevealTransitionsRef.current = 0;
    onDisclosureMotionChange?.(turnKey, false);
  }, [onDisclosureMotionChange, turnKey]);
  const startDisclosureMotion = useCallback(
    (anchorElement: HTMLElement | null) => {
      pendingRevealTransitionsRef.current = collapsibleSectionCount;
      if (collapsibleSectionCount === 0) {
        return;
      }
      disclosureMotionActiveRef.current = true;
      onDisclosureMotionChange?.(turnKey, true, anchorElement);
    },
    [collapsibleSectionCount, onDisclosureMotionChange, turnKey]
  );
  const startDisclosureMotionCallback = usePersistCallback(
    startDisclosureMotion
  );
  const handleRevealTransitionEnd = useCallback(() => {
    if (!disclosureMotionActiveRef.current) {
      return;
    }
    pendingRevealTransitionsRef.current -= 1;
    if (pendingRevealTransitionsRef.current <= 0) {
      finishDisclosureMotion();
    }
  }, [finishDisclosureMotion]);
  const handleRevealTransitionEndCallback = usePersistCallback(
    handleRevealTransitionEnd
  );

  return (
    <div className="grid min-w-0" data-agent-turn-work-section={turnKey}>
      {showDivider ? (
        <div
          className="mb-4 h-px w-full flex-none bg-[var(--line-2,var(--tutti-line-2))]"
          data-testid="agent-transcript-turn-divider"
          aria-hidden="true"
        />
      ) : null}
      {model.leadingRows.length > 0 ? (
        <div className="mb-4 grid gap-4">
          {renderRows(model.leadingRows, renderRow)}
        </div>
      ) : null}
      <div
        className="flex min-h-6 items-center gap-0.5 text-[12px] text-[var(--text-tertiary)]"
        data-agent-turn-work-header={turnKey}
      >
        {model.collapseEligible ? (
          <ButtonBinding1
            toggleLabel={toggleLabel}
            expanded={expanded}
            startDisclosureMotion={startDisclosureMotionCallback}
            disclosureStore={disclosureStore}
            disclosureKey={disclosureKey}
            model={model}
          />
        ) : (
          <AgentTurnDurationLabel timing={model.timing} />
        )}
      </div>
      {model.sections.map((section, sectionIndex) => {
        const content = renderRows(section.rows, renderRow);
        if (section.kind !== "work" || !model.collapseEligible) {
          const firstRow = section.rows[0];
          return (
            <div
              key={`${section.kind}:${firstRow?.renderKey ?? firstRow?.row.id ?? sectionIndex}`}
              className="grid gap-4 pt-4"
            >
              {content}
            </div>
          );
        }
        const firstRow = section.rows[0];
        return (
          <CollapsibleReveal
            key={`work:${firstRow?.renderKey ?? firstRow?.row.id ?? sectionIndex}`}
            expanded={expanded}
            innerClassName="grid gap-4 pt-4"
            onHeightTransitionEnd={handleRevealTransitionEndCallback}
          >
            {content}
          </CollapsibleReveal>
        );
      })}
      {footer}
    </div>
  );
});

function renderRows(
  rows: readonly AgentTurnWorkSectionRow[],
  renderRow: AgentTurnWorkSectionProps["renderRow"]
): ReactNode {
  return rows.map(({ row, rowIndex, renderKey }) =>
    renderRow(row, rowIndex, renderKey)
  );
}

function AgentTurnDurationLabelRender({
  timing
}: {
  timing: AgentTurnTiming;
}): JSX.Element {
  const { t } = useTranslation();
  const liveElapsedSeconds = useElapsedSeconds(
    timing.kind === "live" &&
      timing.observationGapPresentationState === undefined &&
      !Number.isFinite(timing.frozenAtUnixMs)
      ? timing.startedAtUnixMs
      : null
  );
  if (
    timing.kind === "live" &&
    timing.observationGapPresentationState !== undefined
  ) {
    return (
      <span>
        {timing.observationGapPresentationState === "peer-offline"
          ? t("agentHost.agentGui.turnPeerDeviceOfflinePendingSync")
          : t("agentHost.agentGui.turnPeerDeviceProgressSynchronizing")}
      </span>
    );
  }
  const elapsedSeconds =
    timing.kind === "live"
      ? Number.isFinite(timing.frozenAtUnixMs)
        ? Math.max(
            0,
            Math.floor(
              ((timing.frozenAtUnixMs as number) - timing.startedAtUnixMs) /
                1_000
            )
          )
        : (liveElapsedSeconds ?? 0)
      : timing.elapsedSeconds;
  return <span>{translateDuration(t, timing.kind, elapsedSeconds)}</span>;
}

function translateDuration(
  t: ReturnType<typeof useTranslation>["t"],
  kind: AgentTurnTiming["kind"],
  elapsedSeconds: number
): string {
  const duration = formatAgentTurnDuration(elapsedSeconds);
  return kind === "live"
    ? translateLiveDuration(t, duration)
    : translateSettledDuration(t, duration);
}

function translateLiveDuration(
  t: ReturnType<typeof useTranslation>["t"],
  duration: AgentTurnDuration
): string {
  if (duration.kind === "seconds") {
    return t("agentHost.agentGui.turnProcessedSeconds", {
      seconds: duration.seconds
    });
  }
  if (duration.kind === "minutes") {
    return t("agentHost.agentGui.turnProcessedMinutes", {
      minutes: duration.minutes
    });
  }
  return t("agentHost.agentGui.turnProcessedMinutesSeconds", {
    minutes: duration.minutes,
    seconds: duration.seconds
  });
}

function translateSettledDuration(
  t: ReturnType<typeof useTranslation>["t"],
  duration: AgentTurnDuration
): string {
  if (duration.kind === "seconds") {
    return t("agentHost.agentGui.turnTotalSeconds", {
      seconds: duration.seconds
    });
  }
  if (duration.kind === "minutes") {
    return t("agentHost.agentGui.turnTotalMinutes", {
      minutes: duration.minutes
    });
  }
  return t("agentHost.agentGui.turnTotalMinutesSeconds", {
    minutes: duration.minutes,
    seconds: duration.seconds
  });
}

const ButtonBinding1 = observer(function ButtonBinding1({
  toggleLabel,
  expanded,
  startDisclosureMotion,
  disclosureStore,
  disclosureKey,
  model,
  ...slotProps
}: ButtonBinding1Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding1Props>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >((event) => {
    startDisclosureMotion(
      event.currentTarget.closest<HTMLElement>("[data-agent-turn-work-header]")
    );
    disclosureStore.setExpandedOverride(disclosureKey, !expanded);
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="chrome"
        size="xs"
        className="-ml-2 gap-0.5 px-2 text-[12px] font-normal"
        aria-label={toggleLabel}
        aria-expanded={expanded}
        title={toggleLabel}
        onClick={onClickProp1}
      >
        <AgentTurnDurationLabel timing={model.timing} />
        <ChevronDownIcon
          aria-hidden="true"
          data-icon="inline-end"
          className={`transition-transform duration-150 ${
            expanded ? "rotate-0" : "-rotate-90"
          }`}
        />
      </Button>
    </Slot.Root>
  );
});

type ButtonBinding1Props = {
  toggleLabel: string;
  expanded: boolean;
  startDisclosureMotion: (anchorElement: HTMLElement | null) => void;
  disclosureStore: NonNullable<AgentTurnWorkSectionProps>["disclosureStore"];
  disclosureKey: string;
  model: NonNullable<AgentTurnWorkSectionProps>["model"];
};
