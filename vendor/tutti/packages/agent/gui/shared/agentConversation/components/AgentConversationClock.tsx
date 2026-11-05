import { createContext, useContext, type JSX, type ReactNode } from "react";
import {
  useExternalStoreSnapshot,
  type ExternalStoreSnapshotSource
} from "@tutti-os/ui-react-hooks";
import { observer } from "mobx-react-lite";
export const AgentConversationClockProvider = observer(
  AgentConversationClockProviderRender
);

const AgentConversationClockVisibilityContext = createContext(true);
type ClockListener = () => void;

class AgentConversationClockStore {
  private readonly minuteListeners = new Set<ClockListener>();
  private readonly secondListeners = new Set<ClockListener>();
  private minuteTimeMs = Date.now();
  private minuteTimer: number | null = null;
  private secondTimeMs = Date.now();
  private secondTimer: number | null = null;

  readonly disabled: ExternalStoreSnapshotSource<number> = {
    getSnapshot: () => 0,
    subscribe: () => () => undefined
  };

  readonly minute: ExternalStoreSnapshotSource<number> = {
    getSnapshot: () => this.minuteTimeMs,
    subscribe: (listener) =>
      this.subscribe(
        listener,
        this.minuteListeners,
        60_000,
        "minuteTimeMs",
        "minuteTimer"
      )
  };

  readonly second: ExternalStoreSnapshotSource<number> = {
    getSnapshot: () => this.secondTimeMs,
    subscribe: (listener) =>
      this.subscribe(
        listener,
        this.secondListeners,
        1_000,
        "secondTimeMs",
        "secondTimer"
      )
  };

  private subscribe(
    listener: ClockListener,
    listeners: Set<ClockListener>,
    intervalMs: number,
    timeKey: "minuteTimeMs" | "secondTimeMs",
    timerKey: "minuteTimer" | "secondTimer"
  ): () => void {
    listeners.add(listener);
    if (listeners.size === 1) {
      this[timeKey] = Date.now();
      // timing: share one cadence timer across all mounted consumers.
      this[timerKey] = window.setInterval(() => {
        this[timeKey] = Date.now();
        listeners.forEach((candidate) => candidate());
      }, intervalMs);
    }
    return () => {
      listeners.delete(listener);
      if (listeners.size === 0) {
        const timer = this[timerKey];
        if (timer !== null) {
          window.clearInterval(timer);
          this[timerKey] = null;
        }
      }
    };
  }
}

const agentConversationClock = new AgentConversationClockStore();

function AgentConversationClockProviderRender({
  children,
  isVisible
}: {
  children: ReactNode;
  isVisible: boolean;
}): JSX.Element {
  return (
    <AgentConversationClockVisibilityContext.Provider value={isVisible}>
      {children}
    </AgentConversationClockVisibilityContext.Provider>
  );
}

export function useAgentConversationNowUnixMs(enabled: boolean): number | null {
  const isVisible = useContext(AgentConversationClockVisibilityContext);
  const shouldTick = enabled && isVisible;
  const nowUnixMs = useExternalStoreSnapshot(
    shouldTick ? agentConversationClock.second : agentConversationClock.disabled
  );
  return shouldTick ? nowUnixMs : null;
}

export function useAgentConversationMinuteNowUnixMs(): number {
  const isVisible = useContext(AgentConversationClockVisibilityContext);
  const nowUnixMs = useExternalStoreSnapshot(
    isVisible ? agentConversationClock.minute : agentConversationClock.disabled
  );
  return isVisible ? nowUnixMs : agentConversationClock.minute.getSnapshot();
}
