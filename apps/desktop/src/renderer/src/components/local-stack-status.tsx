import { useEffect, useState } from "react";
import { FileText, RefreshCw } from "lucide-react";

import type {
  LocalStackComponentName,
  LocalStackComponentState,
  LocalStackComponentStatus,
  LocalStackOverallState,
  LocalStackStatus,
} from "../../../shared/local-stack-types";
import { LOCAL_STACK_COMPONENT_ORDER } from "../../../shared/local-stack-types";
import { Button } from "@multica/ui/components/ui/button";

const COMPONENT_LABELS: Record<LocalStackComponentName, string> = {
  database: "Database",
  migrations: "Migrations",
  api: "API",
  bootstrap: "Bootstrap",
  daemon: "Daemon",
  runtimeRegistration: "Runtime registration",
};

const HEADERS: Record<
  LocalStackOverallState,
  { title: string; subtitle: string }
> = {
  starting: {
    title: "Starting Multica…",
    subtitle:
      "We're spinning up local components. This usually takes a few seconds.",
  },
  failing: {
    title: "Multica isn't ready yet",
    subtitle:
      "One of the local components failed to start. See details below.",
  },
  ready: {
    title: "Ready",
    subtitle: "All local components are running.",
  },
};

const STATE_DOT_CLASS: Record<LocalStackComponentState, string> = {
  ready: "bg-success",
  starting: "bg-warning",
  retrying: "bg-warning",
  failing: "bg-destructive",
  pending: "bg-muted-foreground/40",
};

const STATE_LABEL: Record<LocalStackComponentState, string> = {
  ready: "Ready",
  starting: "Starting",
  retrying: "Retrying",
  failing: "Failed",
  pending: "Pending",
};

/**
 * Subscribe to the main-process supervisor. Returns the latest status
 * snapshot; null until the first IPC round-trip completes.
 */
export function useLocalStackStatus(): LocalStackStatus | null {
  const [status, setStatus] = useState<LocalStackStatus | null>(null);
  useEffect(() => {
    let cancelled = false;
    void window.localStackAPI.getStatus().then((s) => {
      if (!cancelled) setStatus(s);
    });
    const unsubscribe = window.localStackAPI.onStatusChange((s) => {
      setStatus(s);
    });
    return () => {
      cancelled = true;
      unsubscribe();
    };
  }, []);
  return status;
}

/**
 * Renders the boot-status screen. Caller decides when to show it (gated by
 * `overall !== "ready"` in App.tsx).
 */
export function LocalStackStatusScreen({ status }: { status: LocalStackStatus }) {
  const header = HEADERS[status.overall];
  const retryDisabled = status.overall === "starting";
  const componentByName = new Map(
    status.components.map((c): [LocalStackComponentName, LocalStackComponentStatus] => [c.name, c]),
  );

  return (
    <div className="flex h-screen flex-col items-center justify-center gap-6 bg-background p-8">
      <div className="flex max-w-md flex-col items-center gap-2 text-center">
        <h1 className="text-2xl font-semibold text-foreground">
          {header.title}
        </h1>
        <p className="text-sm text-muted-foreground">{header.subtitle}</p>
      </div>

      <ul className="w-full max-w-md divide-y divide-border rounded-lg border border-border bg-background">
        {LOCAL_STACK_COMPONENT_ORDER.map((name) => {
          const component = componentByName.get(name);
          const state: LocalStackComponentState = component?.state ?? "pending";
          const detail = component?.detail ?? null;
          return (
            <li
              key={name}
              className="flex flex-col gap-1 px-4 py-3"
            >
              <div className="flex items-center justify-between gap-3">
                <span className="text-sm font-medium text-foreground">
                  {COMPONENT_LABELS[name]}
                </span>
                <span className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span
                    aria-hidden="true"
                    className={`inline-block size-2 rounded-full ${STATE_DOT_CLASS[state]}`}
                  />
                  {STATE_LABEL[state]}
                </span>
              </div>
              {detail ? (
                <p className="text-xs text-muted-foreground">{detail}</p>
              ) : null}
            </li>
          );
        })}
      </ul>

      <div className="flex items-center gap-2">
        <Button
          variant="default"
          disabled={retryDisabled}
          onClick={() => {
            void window.localStackAPI.retry();
          }}
        >
          <RefreshCw />
          Retry
        </Button>
        <Button
          variant="outline"
          onClick={() => {
            void window.localStackAPI.openLogs();
          }}
        >
          <FileText />
          Open logs
        </Button>
      </div>
    </div>
  );
}
