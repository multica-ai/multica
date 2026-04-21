"use client";

import { useCallback, useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import { useWSEvent } from "@multica/core/realtime";
import {
  runtimeKeys,
  runtimeListOptions,
} from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import { ProviderLogo } from "../../runtimes/components/provider-logo";

/**
 * Runtime step. Platform-agnostic: shows a live "waiting / connected"
 * status, the list of registered runtimes (selectable), and a single
 * CTA that adapts to state.
 *
 *  - **Empty list**: CTA is "Skip for now" (advances with null runtime).
 *    Web users normally see this first — they haven't installed the CLI
 *    yet. The `instructions` slot is where the platform shell (web)
 *    renders CLI install guidance.
 *  - **Has runtimes**: CTA is "Continue" (advances with the selected
 *    runtime). Required selection — we want step-agent to have a
 *    runtime_id to build a CreateAgent request against.
 *
 *  List is polled every 2s while empty and invalidated on
 *  `daemon:register` so the transition "empty → populated" feels
 *  instant — desktop's auto-started daemon flips it in ~3s, web flips
 *  when the user finishes the `multica setup` command in their terminal.
 */
export function StepRuntimeConnect({
  wsId,
  onNext,
  instructions,
}: {
  wsId: string;
  onNext: (runtime: AgentRuntime | null) => void;
  /**
   * Platform-specific content shown above the live status (CLI install
   * guide on web, omitted on desktop where the daemon auto-starts).
   * Passed in by the platform shell via `OnboardingFlow`'s
   * `runtimeInstructions` prop.
   */
  instructions?: React.ReactNode;
}) {
  const qc = useQueryClient();

  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(wsId, "me"),
    refetchInterval: (q) => (q.state.data?.length ? false : 2000),
  });

  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);
  useWSEvent("daemon:register", handleDaemonEvent);

  const [selectedId, setSelectedId] = useState<string | null>(null);

  // Default selection: prefer online, fall back to the first. Only
  // auto-picks when the user hasn't chosen anything — we don't overwrite
  // their manual choice as the list re-fetches.
  useEffect(() => {
    if (selectedId) return;
    const preferred =
      runtimes.find((r) => r.status === "online") ?? runtimes[0];
    if (preferred) setSelectedId(preferred.id);
  }, [runtimes, selectedId]);

  const selected = runtimes.find((r) => r.id === selectedId) ?? null;
  const hasRuntimes = runtimes.length > 0;

  return (
    <div className="flex w-full max-w-xl flex-col items-center gap-6">
      <div className="flex flex-col gap-3 text-center">
        <h1 className="text-3xl font-semibold tracking-tight">
          Connect a runtime
        </h1>
        <p className="text-base text-muted-foreground">
          A runtime is where your agents actually run. Connect one to your
          workspace so your first agent has somewhere to live.
        </p>
      </div>

      {!hasRuntimes && instructions}

      {/* Live connection status. Polls every 2s while empty and invalidates
          on `daemon:register` so the transition "waiting" → "connected"
          feels instant. Desktop sees this flip in ~3s; web sees it flip
          when the user has completed the CLI install on their terminal. */}
      <div className="flex w-full items-center gap-2 text-sm">
        {hasRuntimes ? (
          <>
            <div className="h-2 w-2 rounded-full bg-success" />
            <span className="font-medium">
              {runtimes.length} runtime{runtimes.length > 1 ? "s" : ""}{" "}
              connected
            </span>
          </>
        ) : (
          <>
            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
            <span className="text-muted-foreground">
              Waiting for connection...
            </span>
          </>
        )}
      </div>

      {hasRuntimes && (
        <div className="flex w-full flex-col gap-2">
          {runtimes.map((rt) => {
            const isSelected = rt.id === selectedId;
            const online = rt.status === "online";
            return (
              <Card
                key={rt.id}
                role="button"
                tabIndex={0}
                onClick={() => setSelectedId(rt.id)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    setSelectedId(rt.id);
                  }
                }}
                className={cn(
                  "flex cursor-pointer flex-row items-center gap-3 p-4 transition-colors",
                  isSelected
                    ? "border-primary ring-1 ring-primary"
                    : "hover:border-foreground/20",
                )}
              >
                <ProviderLogo provider={rt.provider} className="h-5 w-5" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{rt.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {rt.provider}
                  </div>
                </div>
                <span
                  className={cn(
                    "h-2 w-2 shrink-0 rounded-full",
                    online ? "bg-success" : "bg-muted-foreground/40",
                  )}
                  aria-label={online ? "Online" : "Offline"}
                />
              </Card>
            );
          })}
        </div>
      )}

      <Button
        size="lg"
        className="w-full"
        disabled={hasRuntimes && !selected}
        onClick={() => onNext(selected)}
        variant={hasRuntimes ? "default" : "outline"}
      >
        {hasRuntimes ? "Continue" : "Skip for now"}
      </Button>
    </div>
  );
}
