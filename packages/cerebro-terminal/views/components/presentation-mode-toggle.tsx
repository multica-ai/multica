"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@multica/core/api";
import { Switch } from "@multica/ui/components/ui/switch";
import { Button } from "@multica/ui/components/ui/button";
import { Terminal as TerminalIcon, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import { openTerminalPopout } from "../use-issue-terminal-link";

export interface PresentationModeToggleProps {
  runtimeId: string;
  /** Disables interaction (e.g. not owner). The control still renders. */
  readOnly?: boolean;
}

/** Build the popout URL + delegate to the shared opener so all call-sites
 *  share window name + dimensions (window.open re-uses the same browser
 *  window per runtime). */
function openTerminalFor(slug: string, runtimeId: string) {
  openTerminalPopout(`/${slug}/terminal/${runtimeId}`, runtimeId);
}

/**
 * A switch that flips agent_runtime.presentation_mode between 'headless'
 * (default; today's behaviour) and 'interactive' (daemon mirrors agent
 * output to a broker session attached to the Multica UI).
 *
 * Server enforces workspace membership on the PUT, so toggling for a
 * runtime in a workspace the user does not belong to fails with 403.
 * Cloud-runtime providers return 400 `cloud_runtime_not_supported`; the
 * toggle reverts to headless and surfaces a toast.
 */
export function PresentationModeToggle({
  runtimeId,
  readOnly = false,
}: PresentationModeToggleProps) {
  const enabled = useFeatureFlag("cerebro_interactive_terminal");
  const workspace = useCurrentWorkspace();
  const [mode, setMode] = useState<"headless" | "interactive" | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .getRuntimePresentationMode(runtimeId)
      .then((r) => {
        if (cancelled) return;
        setMode(r.presentation_mode);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [runtimeId]);

  const onChange = async (next: boolean) => {
    const target = next ? "interactive" : "headless";
    setPending(true);
    setError(null);
    try {
      const r = await api.setRuntimePresentationMode(runtimeId, target);
      setMode(r.presentation_mode);
    } catch (err) {
      // 400 cloud_runtime_not_supported: revert UI and toast a friendly message.
      const code = extractErrorCode(err);
      if (code === "cloud_runtime_not_supported") {
        toast.error("Interactive mode is not supported for cloud runtimes.");
        setMode("headless");
      } else {
        setError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setPending(false);
    }
  };

  if (!enabled) return null;

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
          <div className="mt-3 flex flex-wrap gap-2">
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => {
                if (!workspace) return;
                openTerminalFor(workspace.slug, runtimeId);
              }}
              disabled={!workspace}
            >
              <ExternalLink className="mr-1.5 size-3.5" aria-hidden />
              Open terminal window
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

function extractErrorCode(err: unknown): string | null {
  if (!(err instanceof ApiError)) return null;
  const body = err.body;
  if (body && typeof body === "object" && "error" in body) {
    const code = (body as { error?: unknown }).error;
    if (typeof code === "string") return code;
  }
  return null;
}
