import { DropdownMenuItem } from "@tutti-os/ui-system";
import { RefreshCw } from "lucide-react";
import { observer } from "mobx-react-lite";
import { useEffect, useState } from "react";

import { toLocalShortDateTime } from "../../app/renderer/shell/utils/format";

export interface AgentProbeUsageFreshnessLabels {
  justUpdated: string;
  minutesAgo: (count: number) => string;
  hoursAgo: (count: number) => string;
  updating: string;
  refreshFailed: string;
  refreshAria: string;
}

/** How often the relative "updated N ago" label re-renders while the popover
 * is open. Coarse on purpose — minute-level granularity doesn't need a faster
 * tick, and the popover only mounts (so this interval only runs) while open. */
const FRESHNESS_TICK_MS = 20_000;

function freshnessAgeText(
  capturedAtUnixMs: number,
  nowMs: number,
  labels: AgentProbeUsageFreshnessLabels
): string {
  const ageMs = Math.max(0, nowMs - capturedAtUnixMs);
  const minutes = Math.floor(ageMs / 60_000);
  if (minutes < 1) {
    return labels.justUpdated;
  }
  if (minutes < 60) {
    return labels.minutesAgo(minutes);
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return labels.hoursAgo(hours);
  }
  return toLocalShortDateTime(capturedAtUnixMs);
}

/**
 * Header-right control for a usage/limits popover: a single clickable affordance
 * that both surfaces data freshness ("Updated 3m ago" / "Updating…" / "Refresh
 * failed") and, on click, forces a fresh probe fetch. Keeping freshness and the
 * refresh action in one control makes the cause/effect obvious — the timestamp
 * is what the button updates.
 */
export const AgentProbeUsageFreshness = observer(
  function AgentProbeUsageFreshness({
    ariaDescribedBy,
    capturedAtUnixMs,
    isLoading,
    didFail,
    onRefresh,
    labels,
    disabled = false,
    presentation = "button",
    testId
  }: {
    ariaDescribedBy?: string;
    capturedAtUnixMs: number | null;
    isLoading: boolean;
    didFail: boolean;
    onRefresh: () => void;
    labels: AgentProbeUsageFreshnessLabels;
    disabled?: boolean;
    presentation?: "button" | "menu-item";
    testId?: string;
  }): React.JSX.Element {
    const [nowMs, setNowMs] = useState(() => Date.now());
    // Bumped on every click. A refresh can be a no-op (served from the
    // main-process throttle cache within its TTL) and never flip `isLoading`, so
    // the click would otherwise produce no motion at all. Replaying a one-shot
    // spin keyed on this nonce gives a reassuring acknowledgement regardless.
    const [clickSpinNonce, setClickSpinNonce] = useState(0);
    const showRelativeTime =
      !isLoading && !didFail && typeof capturedAtUnixMs === "number";

    const handleClick = (): void => {
      setClickSpinNonce((nonce) => nonce + 1);
      onRefresh();
    };

    useEffect(() => {
      if (!showRelativeTime) {
        return;
      }
      setNowMs(Date.now());
      const timer = setInterval(() => setNowMs(Date.now()), FRESHNESS_TICK_MS);
      return () => clearInterval(timer);
    }, [showRelativeTime, capturedAtUnixMs]);

    const text = isLoading
      ? labels.updating
      : didFail
        ? labels.refreshFailed
        : typeof capturedAtUnixMs === "number"
          ? freshnessAgeText(capturedAtUnixMs, nowMs, labels)
          : "";

    const stateClassName = didFail
      ? "text-[var(--state-danger)] hover:text-[var(--state-danger)]"
      : "text-[var(--text-tertiary)] hover:text-[var(--text-primary)]";

    const content = (
      <>
        <RefreshCw
          // Remount on each click (and when loading toggles) so the CSS animation
          // restarts: a continuous spin while a fetch is in flight, otherwise a
          // single reassuring turn per click.
          key={isLoading ? "loading" : `spin-${clickSpinNonce}`}
          size={12}
          strokeWidth={2}
          aria-hidden="true"
          className={
            isLoading
              ? "animate-spin"
              : clickSpinNonce > 0
                ? "motion-safe:animate-[spin_0.6s_linear]"
                : undefined
          }
        />
        {text ? <span className="whitespace-nowrap">{text}</span> : null}
      </>
    );

    if (presentation === "menu-item") {
      return (
        <DropdownMenuItem
          aria-busy={isLoading}
          aria-describedby={ariaDescribedBy}
          aria-label={labels.refreshAria}
          className={`h-6 shrink-0 gap-1 px-1 text-[11px] ${stateClassName}`}
          data-state={isLoading ? "loading" : didFail ? "failed" : "idle"}
          data-testid={testId}
          disabled={isLoading || disabled}
          onSelect={(event) => {
            event.preventDefault();
            handleClick();
          }}
          title={labels.refreshAria}
        >
          {content}
        </DropdownMenuItem>
      );
    }

    return (
      <button
        type="button"
        aria-describedby={ariaDescribedBy}
        data-testid={testId}
        data-state={isLoading ? "loading" : didFail ? "failed" : "idle"}
        className={`nodrag inline-flex shrink-0 cursor-pointer items-center gap-1 rounded-[5px] px-1 py-0.5 text-[11px] leading-4 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] disabled:cursor-default disabled:opacity-70 [-webkit-app-region:no-drag] ${stateClassName}`}
        onClick={handleClick}
        disabled={isLoading || disabled}
        aria-label={labels.refreshAria}
        aria-busy={isLoading}
        title={labels.refreshAria}
      >
        {content}
      </button>
    );
  }
);
