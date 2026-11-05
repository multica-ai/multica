import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { Button } from "@tutti-os/ui-system/components";
import { observer } from "mobx-react-lite";
import { translate } from "../../../i18n";
import type { AgentGUIEditRetryRecoveryAction } from "../controller/agentGuiController.promptOperations";
import type { AgentGUIEditRetryPresentation } from "../model/agentGUIEditRetryModel";

export const AgentGUIEditRetryStatus = observer(
  function AgentGUIEditRetryStatus({
    presentation,
    onRecover
  }: {
    presentation: AgentGUIEditRetryPresentation;
    onRecover: (action: AgentGUIEditRetryRecoveryAction) => Promise<void>;
  }): React.JSX.Element | null {
    if (presentation.state === "ready") {
      return null;
    }
    const processing = presentation.state === "processing";
    const message = processing
      ? translate("agentHost.agentGui.editRetryProcessing")
      : translate("agentHost.agentGui.editRetryNeedsAction");
    const canReconcile = presentation.availableActions.includes("reconcile");
    const canRetryReplacement =
      presentation.availableActions.includes("retry_replacement");

    return (
      <div
        className="mx-auto flex w-[min(720px,100%)] items-center justify-between gap-3 rounded-lg border border-[var(--line-2,var(--tutti-line-2))] bg-[var(--transparency-block)] px-3 py-2 text-[12px] text-[var(--text-secondary)]"
        role="status"
        data-agent-edit-retry-state={presentation.state}
      >
        <span
          className={processing ? "tsh-inline-loading-ellipsis" : undefined}
        >
          {message}
        </span>
        {!processing && (canReconcile || canRetryReplacement) ? (
          <div className="flex shrink-0 items-center gap-1">
            {canReconcile ? <ButtonBinding1 onRecover={onRecover} /> : null}
            {canRetryReplacement ? (
              <ButtonBinding2 onRecover={onRecover} />
            ) : null}
          </div>
        ) : null}
      </div>
    );
  }
);

const ButtonBinding1 = observer(function ButtonBinding1({
  onRecover,
  ...slotProps
}: {
  onRecover: NonNullable<{
    presentation: AgentGUIEditRetryPresentation;
    onRecover: (action: AgentGUIEditRetryRecoveryAction) => Promise<void>;
  }>["onRecover"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    onRecover: NonNullable<{
      presentation: AgentGUIEditRetryPresentation;
      onRecover: (action: AgentGUIEditRetryRecoveryAction) => Promise<void>;
    }>["onRecover"];
  }
>) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void onRecover("reconcile").catch(() => {});
  });
  return (
    <Slot.Root {...slotProps}>
      <Button type="button" variant="ghost" size="sm" onClick={onClickProp1}>
        {translate("agentHost.agentGui.editRetryReconcile")}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  onRecover,
  ...slotProps
}: {
  onRecover: NonNullable<{
    presentation: AgentGUIEditRetryPresentation;
    onRecover: (action: AgentGUIEditRetryRecoveryAction) => Promise<void>;
  }>["onRecover"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    onRecover: NonNullable<{
      presentation: AgentGUIEditRetryPresentation;
      onRecover: (action: AgentGUIEditRetryRecoveryAction) => Promise<void>;
    }>["onRecover"];
  }
>) {
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void onRecover("retry_replacement").catch(() => {});
  });
  return (
    <Slot.Root {...slotProps}>
      <Button type="button" variant="ghost" size="sm" onClick={onClickProp2}>
        {translate("agentHost.agentGui.editRetryRetryReplacement")}
      </Button>
    </Slot.Root>
  );
});
