import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useState, type JSX } from "react";
import {
  AlertCircle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  ListChecks,
  type LucideIcon
} from "lucide-react";
import { translate } from "../../../i18n/index";
import { CollapsibleReveal } from "./CollapsibleReveal";
import type { AgentTuttiModeCheckpointWakeVM } from "../contracts/agentMessageRowVM";
import { observer } from "mobx-react-lite";
export const AgentTuttiModeCheckpointWakeCard = observer(
  AgentTuttiModeCheckpointWakeCardRender
);

// Friendly, collapsed summary for a daemon checkpoint-wake prompt. The agent
// still receives the full prompt verbatim; here the user sees a compact card
// keyed off the checkpoint kind and Issue, with an expand affordance that
// reveals the complete injected text.
function AgentTuttiModeCheckpointWakeCardRender({
  checkpointWake,
  fullText
}: {
  checkpointWake: AgentTuttiModeCheckpointWakeVM;
  fullText: string;
}): JSX.Element {
  "use memo";
  const [expanded, setExpanded] = useState(false);
  const title = translate(checkpointWakeTitleKey(checkpointWake.kind));
  const Icon = checkpointWakeIcon(checkpointWake.kind);
  const issueLabel = checkpointWake.issueId
    ? translate("agentHost.agentGui.tuttiModeCheckpointWakeIssue", {
        issue: checkpointWake.issueId
      })
    : null;
  const hasBody = fullText.trim().length > 0;
  const toggleLabel = translate(
    expanded
      ? "agentHost.agentGui.tuttiModeCheckpointWakeCollapse"
      : "agentHost.agentGui.tuttiModeCheckpointWakeExpand"
  );
  return (
    <section
      data-testid="agent-tutti-checkpoint-wake-card"
      data-checkpoint-kind={checkpointWake.kind}
      className="box-border w-full min-w-0 rounded-[8px] border border-[var(--line-2)] bg-[var(--background-panel)] p-3"
    >
      <div className="flex min-w-0 items-center gap-2">
        <Icon
          size={15}
          strokeWidth={2}
          aria-hidden="true"
          className="shrink-0 text-[var(--text-secondary)]"
        />
        <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-[var(--text-primary)]">
          {title}
        </span>
        {hasBody ? (
          <ButtonBinding1
            expanded={expanded}
            setExpanded={setExpanded}
            toggleLabel={toggleLabel}
          />
        ) : null}
      </div>
      {issueLabel ? (
        <div className="mt-1 min-w-0 truncate font-[var(--tsh-font-mono)] text-[11px] text-[var(--text-tertiary)]">
          {issueLabel}
        </div>
      ) : null}
      {hasBody ? (
        <CollapsibleReveal expanded={expanded} preMountOnIdle>
          <pre className="mt-2 max-h-[320px] overflow-auto whitespace-pre-wrap break-words rounded-[6px] bg-[var(--transparency-block)] px-3 py-2 text-[12px] leading-5 text-[var(--text-secondary)]">
            {fullText}
          </pre>
        </CollapsibleReveal>
      ) : null}
    </section>
  );
}

function checkpointWakeTitleKey(kind: string): string {
  switch (kind) {
    case "task_settled":
      return "agentHost.agentGui.tuttiModeCheckpointWakeTaskSettled";
    case "task_failed":
      return "agentHost.agentGui.tuttiModeCheckpointWakeTaskFailed";
    case "task_canceled":
      return "agentHost.agentGui.tuttiModeCheckpointWakeTaskCanceled";
    case "all_tasks_terminal":
      return "agentHost.agentGui.tuttiModeCheckpointWakeGoalReview";
    case "initial_schedule":
      return "agentHost.agentGui.tuttiModeCheckpointWakeInitialSchedule";
    default:
      return "agentHost.agentGui.tuttiModeCheckpointWakeDefault";
  }
}

function checkpointWakeIcon(kind: string): LucideIcon {
  switch (kind) {
    case "task_failed":
    case "task_canceled":
      return AlertCircle;
    case "task_settled":
    case "all_tasks_terminal":
      return CheckCircle2;
    default:
      return ListChecks;
  }
}

const ButtonBinding1 = observer(function ButtonBinding1({
  expanded,
  setExpanded,
  toggleLabel,
  ...slotProps
}: {
  expanded: boolean;
  setExpanded: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
  toggleLabel: string;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    expanded: boolean;
    setExpanded: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    toggleLabel: string;
  }
>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<"button">["onClick"]>
  >(() => setExpanded((value) => !value));
  return (
    <Slot.Root {...slotProps}>
      <button
        type="button"
        data-testid="agent-tutti-checkpoint-wake-toggle"
        className="inline-flex h-6 shrink-0 items-center gap-1 rounded-[6px] px-1.5 text-[12px] text-[var(--text-tertiary)] transition-colors hover:bg-[var(--transparency-hover)] hover:text-[var(--text-secondary)]"
        aria-expanded={expanded}
        onClick={onClickProp1}
      >
        {toggleLabel}
        {expanded ? (
          <ChevronUp size={14} strokeWidth={2} aria-hidden="true" />
        ) : (
          <ChevronDown size={14} strokeWidth={2} aria-hidden="true" />
        )}
      </button>
    </Slot.Root>
  );
});
