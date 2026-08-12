"use client";

import { useState } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  Loader2,
  RotateCw,
} from "lucide-react";
import type { AgentRuntime } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { BuiltInRuntimeConnectForm } from "./built-in-runtime-connect-form";
import {
  builtInRuntimeSetupPhase,
  findBuiltInRuntime,
} from "./built-in-runtime-setup";
import type { ManagedRuntimeSetupStatus } from "./managed-runtime-setup";

/**
 * "No coding CLI on this machine" stops being a dead end here.
 *
 * The install is always an explicit choice — Desktop no longer downloads a
 * runtime on startup — so this component owns the whole offer → install →
 * connect sequence and reports each state, including why an install failed.
 */
export function BuiltInRuntimeOffer({
  runtimes,
  wsId,
  setup,
  onInstall,
  onConnected,
  onSkipKey,
  manualInstall,
}: {
  runtimes: readonly AgentRuntime[];
  wsId: string;
  setup?: ManagedRuntimeSetupStatus | null;
  /** Platform-injected: only Desktop can install a local runtime. */
  onInstall: () => Promise<{ success: boolean; error?: string }>;
  onConnected: () => void;
  onSkipKey?: () => void;
  /** The "I already have Claude Code / Codex" escape hatch. */
  manualInstall?: React.ReactNode;
}) {
  const { t } = useT("runtimes");
  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
  const phase = builtInRuntimeSetupPhase({ runtimes, setup });
  const runtime = findBuiltInRuntime(runtimes);

  const install = async () => {
    if (starting) return;
    setStarting(true);
    setStartError(null);
    try {
      const result = await onInstall();
      if (!result.success) {
        setStartError(result.error ?? t(($) => $.built_in.install_failed_generic));
      }
    } finally {
      setStarting(false);
    }
  };

  if (phase === "connect" && runtime) {
    return (
      <section className="rounded-lg border p-5">
        <h3 className="text-body font-medium">
          {t(($) => $.built_in.connect.title)}
        </h3>
        <p className="mt-1 mb-4 text-caption leading-[1.55] text-muted-foreground">
          {t(($) => $.built_in.connect.subtitle)}
        </p>
        <BuiltInRuntimeConnectForm
          runtime={runtime}
          wsId={wsId}
          onConnected={onConnected}
          onSkip={onSkipKey}
        />
      </section>
    );
  }

  if (phase === "ready") {
    return (
      <section className="flex items-center gap-3 rounded-lg border border-success/30 bg-success/5 p-5">
        <CheckCircle2 className="h-4 w-4 shrink-0 text-success" />
        <div className="min-w-0">
          <p className="text-body font-medium">
            {t(($) => $.built_in.ready_title)}
          </p>
          <p className="mt-0.5 text-caption text-muted-foreground">
            {t(($) => $.built_in.ready_subtitle)}
          </p>
        </div>
      </section>
    );
  }

  if (phase === "installing") {
    return (
      <section className="flex items-center gap-3 rounded-lg border p-5">
        <Loader2 className="h-4 w-4 shrink-0 animate-spin text-info" />
        <div className="min-w-0">
          <p className="text-body font-medium">
            {t(($) => $.built_in.installing_title)}
          </p>
          <p className="mt-0.5 text-caption text-muted-foreground">
            {setup?.phase === "ready"
              ? t(($) => $.built_in.installing_registering)
              : t(($) => $.built_in.installing_downloading)}
          </p>
        </div>
      </section>
    );
  }

  const failureReason = setup?.error ?? startError;

  return (
    <section className="flex flex-col gap-3">
      <div className="rounded-lg border bg-card p-5">
        <h3 className="text-body font-medium">
          {phase === "failed"
            ? t(($) => $.built_in.failed_title)
            : t(($) => $.built_in.offer_title)}
        </h3>
        <p className="mt-1 text-caption leading-[1.55] text-muted-foreground">
          {phase === "failed"
            ? t(($) => $.built_in.failed_subtitle)
            : t(($) => $.built_in.offer_subtitle)}
        </p>

        {phase === "failed" && failureReason && (
          <p
            role="alert"
            className="mt-3 flex items-start gap-2 rounded-md bg-destructive/5 p-3 text-caption text-destructive"
          >
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span className="min-w-0 break-words">{failureReason}</span>
          </p>
        )}

        <Button className="mt-4 w-full" disabled={starting} onClick={() => void install()}>
          {starting ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : phase === "failed" ? (
            <RotateCw className="h-4 w-4" />
          ) : (
            <Download className="h-4 w-4" />
          )}
          {phase === "failed"
            ? t(($) => $.built_in.retry_action)
            : t(($) => $.built_in.offer_action)}
        </Button>
      </div>

      {manualInstall}
    </section>
  );
}
