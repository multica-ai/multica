import { Button } from "@tutti-os/ui-system/components";
import type { AgentGUIEditRetryRecoveryAction } from "../controller/agentGuiController.promptOperations";
import type { AgentGUIEditRetryPresentation } from "../model/agentGUIEditRetryModel";
import { translate } from "../../../i18n";

export function AgentGUIEditRetryStatus({
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
      <span className={processing ? "tsh-inline-loading-ellipsis" : undefined}>
        {message}
      </span>
      {!processing && (canReconcile || canRetryReplacement) ? (
        <div className="flex shrink-0 items-center gap-1">
          {canReconcile ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                void onRecover("reconcile").catch(() => {});
              }}
            >
              {translate("agentHost.agentGui.editRetryReconcile")}
            </Button>
          ) : null}
          {canRetryReplacement ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                void onRecover("retry_replacement").catch(() => {});
              }}
            >
              {translate("agentHost.agentGui.editRetryRetryReplacement")}
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
