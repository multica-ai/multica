import { useEffect, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { DragStrip } from "@multica/views/platform";
import {
  LOCAL_STACK_STEPS,
  type LocalStackState,
  type LocalStackStep,
} from "../../../shared/local-stack";

const STEP_LABELS: Record<LocalStackStep, string> = {
  config: "Loading configuration",
  probe: "Checking backend",
  engine: "Starting Docker engine",
  containers: "Starting containers",
  backend: "Waiting for backend",
};

const STEPS = LOCAL_STACK_STEPS.map((key) => ({
  key,
  label: STEP_LABELS[key],
}));

type StepStatus = "pending" | "active" | "done" | "failed";

function statusFor(
  step: LocalStackStep,
  state: LocalStackState,
): StepStatus {
  const order = STEPS.findIndex((s) => s.key === step);
  if (state.phase === "ready") return "done";
  if (state.phase === "idle") return "pending";

  const currentIndex = STEPS.findIndex((s) => s.key === state.step);
  if (state.phase === "failed") {
    if (order === currentIndex) return "failed";
    return order < currentIndex ? "done" : "pending";
  }
  if (order < currentIndex) return "done";
  return order === currentIndex ? "active" : "pending";
}

/** How long a bring-up may run before the overlay offers a way out. */
export const SKIP_VISIBLE_AFTER_MS = 15_000;

/**
 * Subscribes to supervisor state.
 *
 * The initial value comes from the synchronous preload read, not from an IPC
 * round-trip: the whole app is gated on this state, so seeding with `idle`
 * would paint the overlay as the first frame of every launch (including SaaS
 * builds, which have nothing to bring up) and would serialize the CoreProvider
 * mount behind an invoke. The async read still runs afterwards to close the
 * window between the preload snapshot and the subscription, and falls back to
 * `ready` if it rejects — a supervisor we cannot reach must never keep the gate
 * closed.
 */
export function useLocalStackState(): LocalStackState {
  const [state, setState] = useState<LocalStackState>(
    () => window.localStackAPI.initialState ?? { phase: "ready" },
  );

  useEffect(() => {
    let active = true;
    window.localStackAPI
      .getState()
      .then((s) => {
        if (active) setState(s);
      })
      .catch(() => {
        if (active) setState({ phase: "ready" });
      });
    const unsubscribe = window.localStackAPI.onState(setState);
    return () => {
      active = false;
      unsubscribe();
    };
  }, []);

  return state;
}

export function LocalStackOverlay({
  state,
  onRetry,
  onSkip,
}: {
  state: LocalStackState;
  onRetry: () => void;
  onSkip: () => void;
}) {
  const failed = state.phase === "failed";

  // Escape hatch while the bring-up is still running. Worst case without it is
  // colima status + colima start + compose up (180s each) plus the 90s backend
  // poll — around nine minutes of a window with no buttons — and it is also
  // what makes a supervisor that wedges mid-run survivable rather than terminal.
  // Delayed rather than immediate so the common short bring-up doesn't invite
  // the user to skip a stack that is seconds from ready.
  const [escapeHatchVisible, setEscapeHatchVisible] = useState(false);
  const running = state.phase === "running" || state.phase === "idle";
  useEffect(() => {
    if (!running) return undefined;
    const timer = setTimeout(
      () => setEscapeHatchVisible(true),
      SKIP_VISIBLE_AFTER_MS,
    );
    return () => clearTimeout(timer);
  }, [running]);

  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <div className="flex flex-1 items-center justify-center bg-background p-8 text-foreground">
        <div className="w-full max-w-md rounded-lg border bg-card p-6 shadow-sm">
          <h1 className="text-lg font-semibold">
            {failed ? "Local stack failed to start" : "Starting local stack"}
          </h1>

          <ul className="mt-5 space-y-2">
            {STEPS.map((step) => {
              const status = statusFor(step.key, state);
              return (
                <li
                  key={step.key}
                  data-testid={`step-${step.key}`}
                  data-status={status}
                  className="flex items-center gap-3 text-sm"
                >
                  <span
                    aria-hidden
                    className={
                      status === "done"
                        ? "size-2 rounded-full bg-primary"
                        : status === "active"
                          ? "size-2 animate-pulse rounded-full bg-primary"
                          : status === "failed"
                            ? "size-2 rounded-full bg-destructive"
                            : "size-2 rounded-full bg-muted-foreground/30"
                    }
                  />
                  <span
                    className={
                      status === "pending"
                        ? "text-muted-foreground"
                        : "text-foreground"
                    }
                  >
                    {step.label}
                  </span>
                </li>
              );
            })}
          </ul>

          {failed ? (
            <>
              <pre className="mt-4 max-h-32 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 text-xs text-muted-foreground">
                {state.message}
              </pre>
              <div className="mt-4 flex gap-2">
                <Button onClick={onRetry}>Retry</Button>
                <Button variant="outline" onClick={onSkip}>
                  Continue anyway
                </Button>
              </div>
            </>
          ) : (
            escapeHatchVisible && (
              <div className="mt-4 flex gap-2">
                <Button variant="outline" onClick={onSkip}>
                  Continue anyway
                </Button>
              </div>
            )
          )}
        </div>
      </div>
    </div>
  );
}
