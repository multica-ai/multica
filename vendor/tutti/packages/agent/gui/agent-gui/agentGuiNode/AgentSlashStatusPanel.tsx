import { observer } from "mobx-react-lite";
import { Fragment } from "react";
import { AgentProbeUsageFreshness } from "./AgentProbeUsageFreshness";

export interface AgentSlashStatusPanelStatus {
  agentSessionId?: string | null;
  baseUrl?: string | null;
  contextWindow?: {
    usedTokens?: number | null;
    totalTokens?: number | null;
  } | null;
  limits?: readonly AgentSlashStatusPanelLimit[];
  limitsLoading?: boolean;
  limitsUnavailable?: boolean;
  limitsResolvedEmpty?: boolean;
  limitsCapturedAtUnixMs?: number | null;
  limitsErrorMessage?: string | null;
  refreshFailed?: boolean;
  isRefreshing?: boolean;
}

export interface AgentSlashStatusPanelLimit {
  id: string;
  label: string;
  percentRemaining?: number | null;
  value: string;
  reset?: string | null;
}

export interface AgentSlashStatusPanelLabels {
  slashStatusTitle: string;
  slashStatusSession: string;
  slashStatusBaseUrl: string;
  slashStatusContext: string;
  slashStatusLimits: string;
  slashStatusClose: string;
  slashStatusContextValue: (input: {
    percentLeft: number;
    usedTokens: string;
    totalTokens: string;
  }) => string;
  slashStatusContextUnavailable: string;
  slashStatusLimitsUnavailable: string;
  slashStatusEmptyValue: string;
  slashStatusUsageJustUpdated: string;
  slashStatusUsageMinutesAgo: (count: number) => string;
  slashStatusUsageHoursAgo: (count: number) => string;
  slashStatusUsageUpdating: string;
  slashStatusUsageRefreshFailed: string;
  slashStatusUsageRefreshAria: string;
}

export function formatSlashStatusTokenCount(
  value: number | null | undefined
): string {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return "";
  }
  return Math.max(0, Math.trunc(value)).toLocaleString("en-US");
}

function slashStatusContextText(
  status: AgentSlashStatusPanelStatus | null | undefined,
  labels: Pick<
    AgentSlashStatusPanelLabels,
    "slashStatusContextValue" | "slashStatusContextUnavailable"
  >
): string {
  const usedTokens = status?.contextWindow?.usedTokens;
  const totalTokens = status?.contextWindow?.totalTokens;
  if (
    typeof usedTokens !== "number" ||
    !Number.isFinite(usedTokens) ||
    typeof totalTokens !== "number" ||
    !Number.isFinite(totalTokens) ||
    totalTokens <= 0
  ) {
    return labels.slashStatusContextUnavailable;
  }
  const used = Math.max(0, Math.trunc(usedTokens));
  const total = Math.max(0, Math.trunc(totalTokens));
  const percentLeft = Math.max(
    0,
    Math.min(100, Math.round(((total - used) / total) * 100))
  );
  return labels.slashStatusContextValue({
    percentLeft,
    usedTokens: formatSlashStatusTokenCount(used),
    totalTokens: formatSlashStatusTokenCount(total)
  });
}

export const AgentSlashStatusPanel = observer(function AgentSlashStatusPanel({
  status,
  labels,
  onClose,
  onRefresh
}: {
  status: AgentSlashStatusPanelStatus | null | undefined;
  labels: AgentSlashStatusPanelLabels;
  onClose: () => void;
  onRefresh?: () => void;
}): React.JSX.Element {
  const limits = status?.limits ?? [];
  const agentSessionId = status?.agentSessionId?.trim() ?? "";
  const showSessionDetails = agentSessionId.length > 0;
  return (
    <section
      className="agent-gui-node__slash-status-panel"
      data-testid="agent-gui-slash-status-panel"
      role="status"
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="truncate text-[11px] font-semibold leading-4">
          {labels.slashStatusTitle}
        </h3>
        <div className="flex min-w-0 items-center gap-1">
          {onRefresh ? (
            <AgentProbeUsageFreshness
              capturedAtUnixMs={status?.limitsCapturedAtUnixMs ?? null}
              isLoading={status?.isRefreshing === true}
              didFail={status?.refreshFailed === true}
              onRefresh={onRefresh}
              labels={{
                justUpdated: labels.slashStatusUsageJustUpdated,
                minutesAgo: labels.slashStatusUsageMinutesAgo,
                hoursAgo: labels.slashStatusUsageHoursAgo,
                updating: labels.slashStatusUsageUpdating,
                refreshFailed: labels.slashStatusUsageRefreshFailed,
                refreshAria: labels.slashStatusUsageRefreshAria
              }}
              testId="agent-gui-slash-status-refresh"
            />
          ) : null}
          <button
            className="nodrag shrink-0 rounded-[5px] px-1.5 py-0.5 text-[11px] leading-4 text-muted-foreground transition-colors hover:bg-background-hover hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring [-webkit-app-region:no-drag]"
            type="button"
            onClick={onClose}
          >
            {labels.slashStatusClose}
          </button>
        </div>
      </div>
      {status?.limitsErrorMessage ? (
        <div
          className="mb-2 rounded-[6px] border border-[var(--on-danger-hover)] bg-[var(--on-danger)] px-2 py-1.5 text-[11px] leading-4 text-[var(--state-danger)]"
          data-testid="agent-gui-slash-status-error"
          role="alert"
        >
          {status.limitsErrorMessage}
        </div>
      ) : null}
      <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-1 font-mono text-[11px] leading-4">
        {showSessionDetails ? (
          <>
            <dt className="text-muted-foreground">
              {labels.slashStatusSession}:
            </dt>
            <dd className="min-w-0 truncate">{agentSessionId}</dd>
          </>
        ) : null}
        <dt className="text-muted-foreground">{labels.slashStatusContext}:</dt>
        <dd className="min-w-0">{slashStatusContextText(status, labels)}</dd>
        {limits.map((limit) => (
          <Fragment key={limit.id}>
            <dt className="text-muted-foreground">{limit.label}:</dt>
            <dd className="min-w-0">
              <span className="agent-gui-node__slash-status-limit">
                {typeof limit.percentRemaining === "number" &&
                Number.isFinite(limit.percentRemaining) ? (
                  <span
                    aria-hidden="true"
                    className="agent-gui-node__slash-status-limit-meter"
                  >
                    <span
                      className="agent-gui-node__slash-status-limit-meter-fill"
                      style={{
                        width: `${Math.max(
                          0,
                          Math.min(100, limit.percentRemaining)
                        )}%`
                      }}
                    />
                  </span>
                ) : null}
                <span className="agent-gui-node__slash-status-limit-value">
                  {limit.value}
                  {limit.reset ? (
                    <span className="text-muted-foreground">
                      {" "}
                      ({limit.reset})
                    </span>
                  ) : null}
                </span>
              </span>
            </dd>
          </Fragment>
        ))}
        {limits.length === 0 &&
        (status?.limitsUnavailable ||
          status?.limitsResolvedEmpty ||
          status?.limitsLoading) ? (
          <>
            <dt className="text-muted-foreground">
              {labels.slashStatusLimits}:
            </dt>
            <dd className="min-w-0 text-muted-foreground">
              {status?.limitsLoading
                ? labels.slashStatusUsageUpdating
                : status?.limitsResolvedEmpty
                  ? labels.slashStatusEmptyValue
                  : labels.slashStatusLimitsUnavailable}
            </dd>
          </>
        ) : null}
      </dl>
    </section>
  );
});
