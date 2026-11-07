"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowUpCircle, Check, Loader2, XCircle } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import {
  agentCLIDisplayName,
  isManualInstallOnly,
} from "@multica/core/runtimes";
import {
  runtimeCLIStatusOptions,
  runtimeKeys,
} from "@multica/core/runtimes/queries";
import type { AgentCLIStatus, AgentCLIUpgradeJob } from "@multica/core/types";
import { useT } from "../../i18n";

const POLL_MS = 2000;

function isJobActive(status: string | undefined): boolean {
  return status === "queued" || status === "draining" || status === "upgrading";
}

function useRuntimeT() {
  return useT("runtimes").t;
}

type RuntimeTranslator = ReturnType<typeof useRuntimeT>;

function stageLabel(status: string | undefined, t: RuntimeTranslator): string {
  switch (status) {
    case "queued":
      return t(($) => $.agent_cli.stage.queued);
    case "draining":
      return t(($) => $.agent_cli.stage.draining);
    case "upgrading":
      return t(($) => $.agent_cli.stage.upgrading);
    case "succeeded":
      return t(($) => $.agent_cli.stage.succeeded);
    case "failed":
      return t(($) => $.agent_cli.stage.failed);
    default:
      return "";
  }
}

function statusLabel(row: AgentCLIStatus, t: RuntimeTranslator): string {
  if (isManualInstallOnly(row)) {
    return t(($) => $.agent_cli.manual_install);
  }
  switch (row.status) {
    case "up_to_date":
      return t(($) => $.agent_cli.status.up_to_date);
    case "outdated":
      return t(($) => $.agent_cli.status.outdated);
    case "missing":
      return t(($) => $.agent_cli.status.missing);
    default:
      return t(($) => $.agent_cli.status.unknown);
  }
}

function AgentCliCard({
  runtimeId,
  row,
  isOnline,
  canUpgrade,
}: {
  runtimeId: string;
  row: AgentCLIStatus;
  isOnline: boolean;
  canUpgrade: boolean;
}) {
  const { t } = useT("runtimes");
  const queryClient = useQueryClient();
  const [job, setJob] = useState<AgentCLIUpgradeJob | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const cleanup = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  useEffect(() => cleanup, [cleanup]);

  const refreshStatus = useCallback(() => {
    void queryClient.invalidateQueries({
      queryKey: runtimeKeys.cliStatus(runtimeId),
    });
  }, [queryClient, runtimeId]);

  const startPolling = useCallback(
    (jobId: string) => {
      cleanup();
      if (!jobId) return;
      pollRef.current = setInterval(() => {
        void api
          .getAgentCLIUpgrade(runtimeId, jobId)
          .then((next) => {
            setJob(next);
            if (next.status === "succeeded") {
              setBusy(false);
              setError("");
              cleanup();
              refreshStatus();
              window.setTimeout(() => setJob(null), 4000);
              return;
            }
            if (!isJobActive(next.status)) {
              setBusy(false);
              setError(next.error || t(($) => $.agent_cli.unknown_error));
              cleanup();
            }
          })
          .catch(() => {
            // Ignore a single missed poll; the next tick retries.
          });
      }, POLL_MS);
    },
    [cleanup, refreshStatus, runtimeId, t],
  );

  const handleUpgrade = async () => {
    if (!canUpgrade || !isOnline || !row.upgradable || isManualInstallOnly(row)) {
      return;
    }
    cleanup();
    setBusy(true);
    setError("");
    setJob({
      job_id: "",
      runtime_id: runtimeId,
      cli: String(row.cli),
      status: "queued",
    });
    try {
      const started = await api.initiateAgentCLIUpgrade(runtimeId, row.cli);
      setJob(started);
      if (started.status === "succeeded") {
        setBusy(false);
        refreshStatus();
        window.setTimeout(() => setJob(null), 4000);
        return;
      }
      if (!isJobActive(started.status)) {
        setBusy(false);
        setError(started.error || t(($) => $.agent_cli.unknown_error));
        return;
      }
      startPolling(started.job_id);
    } catch (err) {
      setBusy(false);
      setJob((prev) =>
        prev
          ? { ...prev, status: "failed" }
          : {
              job_id: "",
              runtime_id: runtimeId,
              cli: String(row.cli),
              status: "failed",
            },
      );
      setError(
        err instanceof Error ? err.message : t(($) => $.agent_cli.initiate_failed),
      );
    }
  };

  const manualOnly = isManualInstallOnly(row);
  const active = busy || isJobActive(job?.status);
  const showUpdate =
    row.upgradable && !manualOnly && canUpgrade && isOnline && !job;
  const showLatest = row.status === "up_to_date" && !manualOnly && !job;
  const stage = job ? stageLabel(job.status, t) : "";
  const cliName = String(row.cli);

  return (
    <div
      className="rounded-lg border bg-card p-3"
      data-testid={`agent-cli-card-${cliName}`}
      data-cli={cliName}
      data-status={row.status}
      data-upgradable={row.upgradable ? "true" : "false"}
      data-manual-install={manualOnly ? "true" : "false"}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 space-y-1">
          <div className="text-caption font-semibold">
            {agentCLIDisplayName(cliName)}
          </div>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-caption text-muted-foreground">
            <span className="font-mono text-foreground">
              {row.current?.trim()
                ? row.current
                : t(($) => $.agent_cli.not_installed)}
            </span>
            {row.latest?.trim() ? (
              <>
                <span aria-hidden="true">→</span>
                <span className="font-mono text-info">{row.latest}</span>
              </>
            ) : null}
          </div>
          <div className="text-caption text-muted-foreground">
            {statusLabel(row, t)}
          </div>
        </div>

        <div className="flex shrink-0 flex-col items-end gap-1">
          {showLatest && (
            <span className="inline-flex items-center gap-1 text-caption text-success">
              <Check className="size-3" />
              {t(($) => $.agent_cli.latest)}
            </span>
          )}
          {showUpdate && (
            <Button
              type="button"
              variant="outline"
              size="xs"
              onClick={() => void handleUpgrade()}
              disabled={active}
              data-testid={`agent-cli-update-${cliName}`}
            >
              <ArrowUpCircle className="size-3" />
              {t(($) => $.agent_cli.update)}
            </Button>
          )}
          {active && (
            <span className="inline-flex items-center gap-1 text-caption text-info">
              <Loader2 className="size-3 animate-spin" />
              {stage || t(($) => $.agent_cli.stage.queued)}
            </span>
          )}
          {job?.status === "succeeded" && !active && (
            <span className="inline-flex items-center gap-1 text-caption text-success">
              <Check className="size-3" />
              {t(($) => $.agent_cli.stage.succeeded)}
            </span>
          )}
          {job?.status === "failed" && !active && (
            <span className="inline-flex items-center gap-1 text-caption text-destructive">
              <XCircle className="size-3" />
              {t(($) => $.agent_cli.stage.failed)}
            </span>
          )}
        </div>
      </div>

      {job?.status === "failed" && error && !manualOnly && (
        <div className="mt-2 rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2">
          <p className="text-caption text-destructive">{error}</p>
          {row.upgradable && canUpgrade && isOnline && (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              className="mt-1"
              data-testid={`agent-cli-retry-${cliName}`}
              onClick={() => void handleUpgrade()}
            >
              {t(($) => $.agent_cli.retry)}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

/**
 * Status for the managed agent CLIs on one runtime. An upgrade control is
 * shown only when the server marks the row upgradable. Otherwise the card
 * shows status, plus install-or-upgrade copy when it is not already current.
 */
export function AgentCliSection({
  runtimeId,
  isOnline,
  canUpgrade = false,
}: {
  runtimeId: string;
  isOnline: boolean;
  /** When false, cards stay read-only. */
  canUpgrade?: boolean;
}) {
  const { t } = useT("runtimes");
  const { data, isLoading, isError } = useQuery(
    runtimeCLIStatusOptions(runtimeId),
  );
  const clis = data && Array.isArray(data.clis) ? data.clis : [];

  if (!runtimeId) return null;

  return (
    <div className="space-y-2" data-testid="agent-cli-section">
      {isLoading && clis.length === 0 && (
        <div className="text-caption text-muted-foreground">
          {t(($) => $.agent_cli.loading)}
        </div>
      )}
      {isError && (
        <div className="text-caption text-destructive">
          {t(($) => $.agent_cli.load_failed)}
        </div>
      )}
      {clis.length > 0 && (
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
          {clis.map((row) => (
            <AgentCliCard
              key={row.cli || row.current}
              runtimeId={runtimeId}
              row={row}
              isOnline={isOnline}
              canUpgrade={canUpgrade}
            />
          ))}
        </div>
      )}
    </div>
  );
}
