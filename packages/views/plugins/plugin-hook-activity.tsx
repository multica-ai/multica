"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import { pluginInvocationsOptions } from "@multica/core/plugins";
import type { PluginInvocation } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

/**
 * Why a plugin's hook is failing, where the admin who installed it will look.
 *
 * An `event` hook fails silently by design — nothing blocks, nobody is watching
 * the call, and after a few failures the circuit breaker stops even trying. All
 * of that is correct behaviour and completely invisible without this: the plugin
 * still says "enabled" while doing nothing at all.
 *
 * Shows nothing when the recent calls all succeeded. A working plugin does not
 * need a panel about it.
 */

/** A breaker trips on this many failures inside its window; see plugin_hook.go. */
const BREAKER_THRESHOLD = 5;

export function summarizeInvocations(invocations: readonly PluginInvocation[]) {
  const recent = invocations.slice(0, 20);
  const failures = recent.filter((invocation) => invocation.status !== "ok");
  return {
    total: recent.length,
    failures,
    // Consecutive from the newest end: a hook that failed and then recovered is
    // not the same story as one that is failing right now.
    consecutiveFailures: countLeadingFailures(recent),
    lastError: failures[0]?.error,
  };
}

function countLeadingFailures(invocations: readonly PluginInvocation[]): number {
  let count = 0;
  for (const invocation of invocations) {
    if (invocation.status === "ok") break;
    count += 1;
  }
  return count;
}

export function PluginHookActivity({ wsId, installationId }: { wsId: string; installationId: string }) {
  const { t } = useT("settings");
  const { data } = useQuery(pluginInvocationsOptions(wsId, installationId));

  const summary = summarizeInvocations(data ?? []);
  if (summary.failures.length === 0) return null;

  const tripped = summary.consecutiveFailures >= BREAKER_THRESHOLD;

  return (
    <div
      className={cn(
        "flex items-start gap-2 rounded-md border px-3 py-2 text-caption",
        // Red only once delivery has actually stopped. A plugin whose endpoint
        // blipped twice is worth mentioning, not worth alarming about — and a
        // banner that is always red stops being read.
        tripped
          ? "border-destructive/40 bg-destructive/5 text-destructive"
          : "border-surface-border text-muted-foreground",
      )}
    >
      <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
      <div className="min-w-0 space-y-0.5">
        <div className="font-medium">
          {tripped
            ? t(($) => $.plugins.hook_circuit_open)
            : t(($) => $.plugins.hook_recent_failures, { count: summary.failures.length })}
        </div>
        {summary.lastError ? <div className="truncate">{summary.lastError}</div> : null}
      </div>
    </div>
  );
}
