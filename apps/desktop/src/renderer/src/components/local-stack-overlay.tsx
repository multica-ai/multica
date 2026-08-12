import { useEffect, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
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

/**
 * Subscribes to supervisor state. Reads the current value on mount first: the
 * bring-up starts as soon as the main process is ready, which is before React
 * mounts, so a subscribe-only approach would miss the early transitions.
 */
export function useLocalStackState(): LocalStackState {
  const [state, setState] = useState<LocalStackState>({ phase: "idle" });

  useEffect(() => {
    let active = true;
    void window.localStackAPI.getState().then((s) => {
      if (active) setState(s);
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

  return (
    <div className="flex h-screen items-center justify-center bg-background p-8 text-foreground">
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

        {failed && (
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
        )}
      </div>
    </div>
  );
}
