"use client";

import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import { Switch } from "@multica/ui/components/ui/switch";
import { Button } from "@multica/ui/components/ui/button";
import { Terminal as TerminalIcon } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { TerminalPanel } from "./terminal-panel";

export interface PresentationModeToggleProps {
  runtimeId: string;
  /** Disables interaction (e.g. not owner). The control still renders. */
  readOnly?: boolean;
}

/**
 * A switch that flips agent_runtime.presentation_mode between 'headless'
 * (default; today's behaviour) and 'interactive' (broker spawns a PTY-style
 * session attached to the Multica UI).
 *
 * Server enforces workspace membership on the PUT, so toggling for a
 * runtime in a workspace the user does not belong to fails with 403.
 */
export function PresentationModeToggle({
  runtimeId,
  readOnly = false,
}: PresentationModeToggleProps) {
  const enabled = useFeatureFlag("cerebro_interactive_terminal");
  const [mode, setMode] = useState<"headless" | "interactive" | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [terminalOpen, setTerminalOpen] = useState(false);

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    api
      .getRuntimePresentationMode(runtimeId)
      .then((r) => {
        if (!cancelled) setMode(r.presentation_mode);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [runtimeId, enabled]);

  if (!enabled) return null;

  const onChange = async (next: boolean) => {
    const target = next ? "interactive" : "headless";
    setPending(true);
    setError(null);
    try {
      const r = await api.setRuntimePresentationMode(runtimeId, target);
      setMode(r.presentation_mode);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="flex items-start gap-3 rounded-md border p-3">
      <TerminalIcon className="mt-0.5 size-4 text-muted-foreground" aria-hidden />
      <div className="flex-1">
        <div className="flex items-center justify-between gap-2">
          <label className="text-sm font-medium" htmlFor={`pm-${runtimeId}`}>
            Interactive terminal
          </label>
          <Switch
            id={`pm-${runtimeId}`}
            checked={mode === "interactive"}
            disabled={readOnly || pending || mode === null}
            onCheckedChange={onChange}
          />
        </div>
        <p className="mt-1 text-xs text-muted-foreground">
          When on, agent runs on this runtime stream their session to a Multica
          terminal you can attach to. Off keeps the current headless flow.
        </p>
        {error && (
          <p className="mt-1 text-xs text-destructive">{error}</p>
        )}
        {mode === "interactive" && (
          <div className="mt-3">
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => setTerminalOpen((v) => !v)}
            >
              {terminalOpen ? "Close test terminal" : "Open test terminal"}
            </Button>
            {terminalOpen && (
              <div className="mt-3">
                <TerminalPanel runtimeId={runtimeId} />
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
