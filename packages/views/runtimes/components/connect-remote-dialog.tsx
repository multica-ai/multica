"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Check,
  Circle,
  CircleCheck,
  Copy,
  LoaderCircle,
  RefreshCw,
  Terminal,
} from "lucide-react";
import { useConfigStore } from "@multica/core/config";
import {
  paths,
  useCurrentWorkspace,
  useWorkspaceSlug,
} from "@multica/core/paths";
import { useWSEvent } from "@multica/core/realtime";
import {
  runtimeKeys,
  runtimeListOptions,
  runtimeSetupCreateOptions,
  runtimeSetupStatusOptions,
} from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { copyText } from "@multica/ui/lib/clipboard";
import { CODE_LIGATURE_CLASS } from "@multica/ui/lib/code-style";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { buildRuntimeMachines } from "./runtime-machines";
import { CompactRuntimeRow } from "../../onboarding/components/compact-runtime-row";

const INSTALL_SCRIPT =
  "https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh";

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

export function runtimeSetupCommand(
  token: string,
  serverUrl?: string,
  appUrl?: string,
): string {
  const args = [`--token ${shellQuote(token)}`];
  const normalizedServer = serverUrl?.trim().replace(/\/+$/, "");
  const normalizedApp = appUrl?.trim().replace(/\/+$/, "");
  if (normalizedServer && normalizedApp) {
    args.push(`--server-url ${shellQuote(normalizedServer)}`);
    args.push(`--app-url ${shellQuote(normalizedApp)}`);
  }
  return `curl -fsSL ${INSTALL_SCRIPT} | bash -s -- ${args.join(" ")}`;
}

interface ConnectRemoteDialogProps {
  onClose: () => void;
  /** Explicit during web onboarding; runtime settings use URL workspace state. */
  workspaceId?: string;
  /** Onboarding advances with the newly detected runtime. */
  onConnected?: (runtime: AgentRuntime) => void;
}

function useConnectWorkspaceId(explicitWorkspaceId?: string): string {
  const currentWorkspace = useCurrentWorkspace();
  const workspaceId = explicitWorkspaceId ?? currentWorkspace?.id;
  if (!workspaceId) {
    throw new Error(
      "ConnectRemoteDialog requires a workspaceId outside a workspace route",
    );
  }
  return workspaceId;
}

export function ConnectRemoteDialog({
  onClose,
  workspaceId,
  onConnected,
}: ConnectRemoteDialogProps) {
  const wsId = useConnectWorkspaceId(workspaceId);
  const slug = useWorkspaceSlug();
  const navigation = useNavigation();
  const qc = useQueryClient();
  const { t } = useT("runtimes");
  const daemonServerUrl = useConfigStore((state) => state.daemonServerUrl);
  const daemonAppUrl = useConfigStore((state) => state.daemonAppUrl);
  const [copied, setCopied] = useState(false);

  const created = useQuery(runtimeSetupCreateOptions(wsId));
  const sessionId = created.data?.id ?? "";
  const status = useQuery(runtimeSetupStatusOptions(wsId, sessionId));
  const session = status.data ?? created.data;
  const runtimes = useQuery(runtimeListOptions(wsId));

  const refreshProgress = useCallback(
    (payload: unknown) => {
      const body = payload as { setup_session_id?: unknown } | null;
      if (
        sessionId &&
        body?.setup_session_id &&
        body.setup_session_id !== sessionId
      ) {
        return;
      }
      if (sessionId) {
        void qc.invalidateQueries({
          queryKey: runtimeKeys.setupStatus(wsId, sessionId),
        });
      }
      void qc.invalidateQueries({ queryKey: runtimeKeys.list(wsId) });
    },
    [qc, sessionId, wsId],
  );
  useWSEvent("setup:progress", refreshProgress);
  useWSEvent("daemon:register", refreshProgress);

  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 2_000);
    return () => window.clearTimeout(timer);
  }, [copied]);

  // The raw token is intentionally returned only once, by the create call.
  // Status polling must never replace the source used to render the command.
  const command = created.data?.token
    ? runtimeSetupCommand(created.data.token, daemonServerUrl, daemonAppUrl)
    : "";
  const sessionRuntimes = useMemo(() => {
    if (!session?.daemon_id) return [];
    return (runtimes.data ?? []).filter(
      (runtime) => runtime.daemon_id === session.daemon_id,
    );
  }, [runtimes.data, session?.daemon_id]);
  const machines = useMemo(
    () => buildRuntimeMachines(sessionRuntimes, { now: Date.now() }),
    [sessionRuntimes],
  );

  // Onboarding (onConnected) attaches the Multica Helper agent to a runtime, so
  // it needs a concrete pick. Offer the machine connected in THIS session; when
  // the user already had a computer connected (a returning session whose daemon
  // differs), fall back to any online runtime so onboarding never dead-ends
  // with a permanently-disabled Continue (MUL-5112 regression).
  const onboarding = Boolean(onConnected);
  const selectableRuntimes = useMemo(() => {
    if (!onboarding) return sessionRuntimes;
    if (sessionRuntimes.length > 0) return sessionRuntimes;
    return (runtimes.data ?? []).filter((runtime) => runtime.status === "online");
  }, [onboarding, sessionRuntimes, runtimes.data]);

  const [selectedRuntimeId, setSelectedRuntimeId] = useState<string | null>(null);
  useEffect(() => {
    if (
      selectedRuntimeId &&
      selectableRuntimes.some((runtime) => runtime.id === selectedRuntimeId)
    ) {
      return;
    }
    setSelectedRuntimeId(selectableRuntimes[0]?.id ?? null);
  }, [selectableRuntimes, selectedRuntimeId]);
  const selectedRuntime =
    selectableRuntimes.find((runtime) => runtime.id === selectedRuntimeId) ?? null;

  // The runtimes-page "View runtime" button still targets the just-connected
  // machine; onboarding advances with the runtime the user picked.
  const firstRuntime = sessionRuntimes[0] ?? null;
  const primaryRuntime = onboarding ? selectedRuntime : firstRuntime;

  // A progress poll can succeed even if the corresponding websocket event
  // was missed. Refresh the runtime list when that fallback discovers one.
  useEffect(() => {
    if ((session?.runtime_count ?? 0) > 0) {
      void qc.invalidateQueries({ queryKey: runtimeKeys.list(wsId) });
    }
  }, [qc, session?.runtime_count, wsId]);

  const handleCopy = () => {
    if (!command) return;
    void copyText(command).then((ok) => setCopied(ok));
  };

  const handlePrimary = () => {
    if (onConnected) {
      if (selectedRuntime) onConnected(selectedRuntime);
      return;
    }
    if (!firstRuntime) return;
    onClose();
    if (slug) navigation.push(paths.workspace(slug).runtimeDetail(firstRuntime.id));
  };

  const handleCreateAgent = () => {
    onClose();
    if (slug) navigation.push(paths.workspace(slug).agents());
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[88vh] flex-col gap-0 p-0 sm:max-w-xl">
        <DialogHeader className="px-6 pt-6 pb-2">
          <DialogTitle className="text-base text-balance">
            {t(($) => $.connect.title)}
          </DialogTitle>
          <DialogDescription className="text-xs text-balance">
            {t(($) => $.connect.one_command_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-6 py-4">
          {created.isPending ? (
            <div className="flex min-h-28 items-center justify-center rounded-lg border bg-muted/30">
              <LoaderCircle className="size-5 animate-spin text-muted-foreground" aria-hidden />
              <span className="sr-only">{t(($) => $.connect.preparing)}</span>
            </div>
          ) : created.isError ? (
            <div className="space-y-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4">
              <p className="text-sm font-medium text-foreground">
                {t(($) => $.connect.prepare_failed)}
              </p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.connect.prepare_failed_hint)}
              </p>
              <Button variant="outline" size="sm" onClick={() => void created.refetch()}>
                <RefreshCw className="size-3.5" aria-hidden />
                {t(($) => $.connect.try_again)}
              </Button>
            </div>
          ) : (
            <>
              <div>
                <div className="mb-1.5 flex items-center justify-between gap-3">
                  <p className="text-xs font-medium text-foreground">
                    {t(($) => $.connect.run_command)}
                  </p>
                  <span className="text-[11px] text-muted-foreground">
                    {t(($) => $.connect.token_expiry)}
                  </span>
                </div>
                <div className="flex items-start gap-2 rounded-lg bg-muted px-3 py-3 font-mono text-sm">
                  <Terminal className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                  <code
                    className={cn(
                      "min-w-0 flex-1 break-all whitespace-pre-wrap tabular-nums",
                      CODE_LIGATURE_CLASS,
                    )}
                  >
                    {command}
                  </code>
                  <button
                    type="button"
                    onClick={handleCopy}
                    aria-label={t(($) => $.connect.copy_aria)}
                    className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    {copied ? (
                      <Check className="size-3.5 text-success" aria-hidden />
                    ) : (
                      <Copy className="size-3.5" aria-hidden />
                    )}
                  </button>
                </div>
                <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
                  {t(($) => $.connect.security_hint)}
                </p>
              </div>

              <SetupChecklist
                redeemed={Boolean(session?.redeemed_at)}
                daemonConnected={Boolean(session?.daemon_connected_at)}
                runtimeCount={session?.runtime_count ?? 0}
              />

              {session?.daemon_connected_at && session.runtime_count === 0 ? (
                <div className="flex gap-2.5 rounded-lg border border-warning/30 bg-warning/5 p-3">
                  <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" aria-hidden />
                  <div>
                    <p className="text-xs font-medium text-foreground">
                      {t(($) => $.connect.no_runtime_title)}
                    </p>
                    <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
                      {t(($) => $.connect.no_runtime_hint)}
                    </p>
                  </div>
                </div>
              ) : null}

              {onboarding && selectableRuntimes.length > 0 ? (
                <RuntimePicker
                  runtimes={selectableRuntimes}
                  selectedId={selectedRuntimeId}
                  onSelect={setSelectedRuntimeId}
                />
              ) : session?.runtime_count ? (
                <ConnectedMachines machines={machines} runtimeCount={session.runtime_count} />
              ) : null}
            </>
          )}
        </div>

        <DialogFooter className="m-0 rounded-b-xl border-t bg-muted/30 px-6 py-3 sm:justify-between">
          <Button variant="ghost" size="sm" onClick={onClose}>
            {t(($) => $.connect.cancel)}
          </Button>
          <div className="flex items-center gap-2">
            {!onConnected && firstRuntime ? (
              <Button variant="outline" size="sm" onClick={handleCreateAgent}>
                {t(($) => $.connect.create_agent)}
              </Button>
            ) : null}
            <Button size="sm" disabled={!primaryRuntime} onClick={handlePrimary}>
              {onConnected
                ? t(($) => $.connect.continue)
                : t(($) => $.connect.view_runtime)}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SetupChecklist({
  redeemed,
  daemonConnected,
  runtimeCount,
}: {
  redeemed: boolean;
  daemonConnected: boolean;
  runtimeCount: number;
}) {
  const { t } = useT("runtimes");
  const items = [
    { done: redeemed, label: t(($) => $.connect.check_command) },
    { done: daemonConnected, label: t(($) => $.connect.check_daemon) },
    {
      done: runtimeCount > 0,
      label: t(($) => $.connect.check_runtimes, { count: runtimeCount }),
    },
  ];
  return (
    <div className="rounded-lg border p-3" aria-live="polite">
      <p className="mb-2.5 text-xs font-medium text-foreground">
        {t(($) => $.connect.progress_title)}
      </p>
      <ol className="space-y-2">
        {items.map((item, index) => (
          <li key={item.label} className="flex items-center gap-2 text-xs">
            {item.done ? (
              <CircleCheck className="size-4 shrink-0 text-success" aria-hidden />
            ) : index === 0 || items[index - 1]?.done ? (
              <LoaderCircle className="size-4 shrink-0 animate-spin text-muted-foreground" aria-hidden />
            ) : (
              <Circle className="size-4 shrink-0 text-muted-foreground/50" aria-hidden />
            )}
            <span className={item.done ? "text-foreground" : "text-muted-foreground"}>
              {item.label}
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}

function ConnectedMachines({
  machines,
  runtimeCount,
}: {
  machines: ReturnType<typeof buildRuntimeMachines>;
  runtimeCount: number;
}) {
  const { t } = useT("runtimes");
  return (
    <div className="rounded-lg border p-3">
      <p className="text-xs font-medium text-foreground">
        {t(($) => $.connect.connected_summary, {
          runtimeCount,
          computerCount: machines.length,
        })}
      </p>
      <div className="mt-2 space-y-2">
        {machines.map((machine) => (
          <div key={machine.id} className="rounded-md bg-muted/50 px-3 py-2">
            <div className="flex items-center justify-between gap-3">
              <span className="truncate text-xs font-medium text-foreground">
                {machine.title}
              </span>
              <span className="shrink-0 text-[11px] text-muted-foreground">
                {t(($) => $.connect.runtime_count, { count: machine.runtimes.length })}
              </span>
            </div>
            <p className="mt-1 truncate text-[11px] text-muted-foreground">
              {machine.providerNames.join(" · ")}
            </p>
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * Selectable runtime list for onboarding: the connected machine can expose
 * several agent runtimes (Claude Code, Codex, Cursor, …), and the user picks
 * which one their first agent runs on. Restores the selection step that the
 * onboarding runtime picker used to own before the connect dialog was unified
 * (MUL-5112).
 */
function RuntimePicker({
  runtimes,
  selectedId,
  onSelect,
}: {
  runtimes: AgentRuntime[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const { t } = useT("runtimes");
  return (
    <div className="rounded-lg border p-3">
      <p className="mb-2.5 text-xs font-medium text-foreground">
        {t(($) => $.connect.choose_runtime)}
      </p>
      <div className="flex max-h-[240px] flex-col gap-2 overflow-y-auto">
        {runtimes.map((runtime) => (
          <CompactRuntimeRow
            key={runtime.id}
            runtime={runtime}
            selected={runtime.id === selectedId}
            onSelect={() => onSelect(runtime.id)}
          />
        ))}
      </div>
    </div>
  );
}
