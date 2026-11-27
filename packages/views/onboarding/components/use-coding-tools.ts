"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type {
  CodingToolError,
  CodingToolId,
  CodingToolPhase,
  CodingToolProbe,
  CodingToolRunEvent,
  CodingToolRunKind,
  CodingToolsBridge,
} from "@multica/core/onboarding";

export interface CodingToolRun {
  /** Empty until the host answers the start request. */
  runId: string;
  kind: CodingToolRunKind;
  phase: CodingToolPhase;
  status: "running" | "done" | "failed";
  progress?: number;
  loginUrl?: string;
  needsCode?: boolean;
  error?: CodingToolError;
  logTail?: string;
}

const PROBE_INTERVAL_MS = 5000;

/**
 * Installs and signs in to coding tools through the desktop bridge, and keeps
 * a probe of what this machine has. `poll` re-probes on an interval so a tool
 * the member installs in their own terminal shows up without a refresh.
 */
export function useCodingTools(
  bridge: CodingToolsBridge | undefined,
  {
    poll,
    onFinished,
  }: {
    poll: boolean;
    onFinished?: (event: CodingToolRunEvent) => void;
  },
) {
  const [probes, setProbes] = useState<Partial<Record<CodingToolId, CodingToolProbe>> | null>(
    null,
  );
  const [runs, setRuns] = useState<Partial<Record<CodingToolId, CodingToolRun>>>({});
  /** Tools installed or signed in during this visit, to badge and auto-select them. */
  const [fresh, setFresh] = useState<CodingToolId[]>([]);
  const onFinishedRef = useRef(onFinished);
  onFinishedRef.current = onFinished;

  const probe = useCallback(async () => {
    if (!bridge) return;
    try {
      const result = await bridge.probe();
      setProbes(Object.fromEntries(result.map((entry) => [entry.id, entry])));
    } catch {
      setProbes((current) => current ?? {});
    }
  }, [bridge]);

  useEffect(() => {
    void probe();
  }, [probe]);

  useEffect(() => {
    if (!bridge || !poll) return;
    const id = window.setInterval(() => void probe(), PROBE_INTERVAL_MS);
    return () => window.clearInterval(id);
  }, [bridge, poll, probe]);

  useEffect(() => {
    if (!bridge) return;
    return bridge.onEvent((event: CodingToolRunEvent) => {
      setRuns((current) => {
        const run = current[event.toolId];
        // A run's first events can land before `start` resolves with its id.
        if (!run || (run.runId && run.runId !== event.runId)) return current;
        const next: CodingToolRun = {
          ...run,
          runId: event.runId,
          phase: event.phase,
          status: event.status,
          progress: event.progress ?? (event.phase === run.phase ? run.progress : undefined),
          loginUrl: event.phase === "login" ? (event.loginUrl ?? run.loginUrl) : undefined,
          needsCode: event.phase === "login" ? (event.needsCode ?? run.needsCode) : undefined,
          error: event.error,
          logTail: event.logTail ?? run.logTail,
        };
        if (event.finished && event.error?.code === "cancelled") {
          const rest = { ...current };
          delete rest[event.toolId];
          return rest;
        }
        return { ...current, [event.toolId]: next };
      });
      if (event.finished) {
        if (event.status === "done") {
          setFresh((current) => (current.includes(event.toolId) ? current : [...current, event.toolId]));
        }
        void probe();
        onFinishedRef.current?.(event);
      }
    });
  }, [bridge, probe]);

  const start = useCallback(
    async (toolId: CodingToolId, kind: CodingToolRunKind) => {
      if (!bridge) return;
      setRuns((current) => ({
        ...current,
        [toolId]: {
          runId: "",
          kind,
          phase: kind === "install" ? "download" : "login",
          status: "running",
        },
      }));
      const result = await bridge.start(toolId, kind).catch((err: unknown) => ({
        error: { code: "unknown" as const, message: err instanceof Error ? err.message : String(err) },
      }));
      setRuns((current) => {
        const run = current[toolId];
        if (!run) return current;
        if ("error" in result) {
          return { ...current, [toolId]: { ...run, status: "failed", error: result.error } };
        }
        return run.runId ? current : { ...current, [toolId]: { ...run, runId: result.runId } };
      });
    },
    [bridge],
  );

  const cancel = useCallback(
    (toolId: CodingToolId) => {
      const run = runs[toolId];
      if (!bridge || !run?.runId) return;
      void bridge.cancel(run.runId);
    },
    [bridge, runs],
  );

  const submitCode = useCallback(
    async (toolId: CodingToolId, code: string) => {
      const run = runs[toolId];
      if (!bridge || !run?.runId) return false;
      return bridge.submitCode(run.runId, code);
    },
    [bridge, runs],
  );

  const dismiss = useCallback((toolId: CodingToolId) => {
    setRuns((current) => {
      const rest = { ...current };
      delete rest[toolId];
      return rest;
    });
  }, []);

  return { probes, runs, fresh, probe, start, cancel, submitCode, dismiss };
}
