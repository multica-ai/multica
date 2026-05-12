"use client";

// CEREBRO-PATCH(runtime-detail-sandbox): cerebro adds the per-runtime sandbox
// override toggle inside DiagnosticsCard. Everything else follows upstream's
// HeroCard / ServingAgentsCard / DiagnosticsCard layout introduced in the
// 2026-W19 sync. PingSection (cerebro-only) and the metadata <pre> dump were
// removed — they have no upstream counterpart and the diagnostic info they
// surfaced is now visible in the Hero card.

import { useEffect, useState } from "react";
import {
  ArrowLeft,
  Trash2,
  ChevronRight,
  Cpu,
  Lock,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime, Agent, MemberWithUser } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
// CEREBRO-PATCH(runtime-detail-sandbox-mutation): keep useUpdateRuntimeSandbox import
import { useDeleteRuntime, useUpdateRuntimeSandbox } from "@multica/core/runtimes/mutations";
import { deriveRuntimeHealth } from "@multica/core/runtimes";
// CEREBRO-PATCH(runtime-detail-pause): pause/resume controls live in cerebro-runtime.
import { PauseRuntimeButton, PauseBanner } from "@multica/cerebro-runtime/views";
import {
  type AgentPresenceDetail,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { MobileSidebarTrigger } from "../../layout/page-header";
import { AppLink } from "../../navigation";
import { availabilityConfig, workloadConfig } from "../../agents/presence";
import { formatLastSeen } from "../utils";
import { HealthBadge } from "./shared";
import { ProviderLogo } from "./provider-logo";
import { UpdateSection } from "./update-section";
import { UsageSection } from "./usage-section";
import { useT } from "../../i18n";
// CEREBRO-PATCH(runtime-detail-accounts): JEH-999 filter card to runtime's own account
import { RuntimeAccountsCard } from "@multica/cerebro-runtime";

function getCliVersion(metadata: Record<string, unknown>): string | null {
  if (
    metadata &&
    typeof metadata.cli_version === "string" &&
    metadata.cli_version
  ) {
    return metadata.cli_version;
  }
  return null;
}

function getLaunchedBy(metadata: Record<string, unknown>): string | null {
  if (
    metadata &&
    typeof metadata.launched_by === "string" &&
    metadata.launched_by
  ) {
    return metadata.launched_by;
  }
  return null;
}

function shortDaemonId(id: string | null): string | null {
  if (!id) return null;
  if (id.length <= 10) return id;
  return `${id.slice(0, 6)}··${id.slice(-2)}`;
}

// 30s tick keeps derived runtime health honest as time-based windows
// (recently_lost → offline → about_to_gc) cross thresholds without any new
// query data arriving. Agent presence has no time windows anymore, so it
// doesn't need this — but useWorkspacePresenceMap is the dependency we
// already mounted on this page, and that's wired to query data, not `now`.
function useNowTick(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function RuntimeDetail({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const cliVersion =
    runtime.runtime_mode === "local" ? getCliVersion(runtime.metadata) : null;
  const launchedBy =
    runtime.runtime_mode === "local" ? getLaunchedBy(runtime.metadata) : null;

  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);
  const deleteMutation = useDeleteRuntime(wsId);
  // CEREBRO-PATCH(runtime-detail-sandbox-state): per-runtime sandbox override mutation + error surface
  const sandboxMutation = useUpdateRuntimeSandbox(wsId);
  const now = useNowTick();

  const [deleteOpen, setDeleteOpen] = useState(false);

  const health = deriveRuntimeHealth(runtime, now);
  const ownerMember = runtime.owner_id
    ? members.find((m) => m.user_id === runtime.owner_id) ?? null
    : null;

  const currentMember = user
    ? members.find((m) => m.user_id === user.id)
    : null;
  const isAdmin = currentMember
    ? currentMember.role === "owner" || currentMember.role === "admin"
    : false;
  const isRuntimeOwner = user && runtime.owner_id === user.id;
  const canDelete = isAdmin || isRuntimeOwner;

  const servingAgents = agents.filter(
    (a) => a.runtime_id === runtime.id && !a.archived_at,
  );

  const handleDelete = () => {
    deleteMutation.mutate(runtime.id, {
      onSuccess: () => {
        toast.success(t(($) => $.detail.toast_deleted));
        setDeleteOpen(false);
      },
      onError: (e) => {
        toast.error(e instanceof Error ? e.message : t(($) => $.detail.toast_delete_failed));
      },
    });
  };

  const daemonShort = shortDaemonId(runtime.daemon_id);
  const lastSeen = formatLastSeen(runtime.last_seen_at);

  return (
    <div className="flex h-full flex-col">
      {/* Topbar — back link + breadcrumb + right-side actions. Mirrors the
          skill-detail-page topbar so users build one mental model for
          "go back to the index" across the dashboard. */}
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
        {/* CEREBRO-PATCH(runtime-detail-mobile-nav): expose global nav on mobile detail pages. */}
        <MobileSidebarTrigger className="mr-0" />
        <Button
          variant="ghost"
          size="xs"
          render={<AppLink href={paths.runtimes()} />}
        >
          <ArrowLeft className="h-3 w-3" />
          {t(($) => $.detail.all_runtimes)}
        </Button>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="truncate font-mono text-xs text-foreground">
          {runtime.name}
        </span>
        <div className="ml-auto flex items-center gap-2">
          {!canDelete && (
            <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
              <Lock className="h-3 w-3" />
              {t(($) => $.detail.read_only)}
            </span>
          )}
          {/* CEREBRO-PATCH(runtime-detail-pause): pause/resume button next to delete. */}
          {canDelete && <PauseRuntimeButton runtime={runtime} workspaceId={wsId} compact />}
          {canDelete && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setDeleteOpen(true)}
                    className="text-muted-foreground hover:text-destructive"
                    aria-label={t(($) => $.detail.delete_aria)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                }
              />
              <TooltipContent>{t(($) => $.detail.delete_tooltip)}</TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>

      {/* Body — single scroll container that owns the Hero card AND the
          analytic blocks below. Putting Hero inside the scroll (instead of
          pinning it under the topbar) means the scroll bar starts at the
          page boundary rather than mid-content; the topbar stays sticky on
          its own because it's navigation, not data. */}
      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="grid grid-cols-1 gap-4 p-6 lg:grid-cols-[minmax(0,1fr)_320px]">
          <div className="min-w-0 space-y-5">
            {/* CEREBRO-PATCH(runtime-detail-pause): pause-state banner above HeroCard. */}
            <PauseBanner runtime={runtime} />
            <HeroCard
              runtime={runtime}
              health={health}
              lastSeen={lastSeen}
              ownerMember={ownerMember}
              cliVersion={cliVersion}
              daemonShort={daemonShort}
            />
            <UsageSection runtimeId={runtime.id} />
          </div>

          {/* Right rail: serving agents + diagnostics */}
          <div className="space-y-4">
            <ServingAgentsCard
              agents={servingAgents}
              presenceMap={presenceMap}
              agentHref={(id) => paths.agentDetail(id)}
            />
            {/* CEREBRO-PATCH(runtime-detail-accounts): JEH-999 filter card to runtime's own account */}
            <RuntimeAccountsCard runtime={runtime} />
            <DiagnosticsCard
              runtime={runtime}
              cliVersion={cliVersion}
              launchedBy={launchedBy}
              canDelete={!!canDelete}
              isAdmin={isAdmin}
              sandboxMutation={sandboxMutation}
              onDelete={() => setDeleteOpen(true)}
            />
          </div>
        </div>
      </div>

      {/* Delete confirmation */}
      <AlertDialog open={deleteOpen} onOpenChange={(v) => { if (!v) setDeleteOpen(false); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.detail.delete_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.detail.delete_dialog.description, { name: runtime.name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.detail.delete_dialog.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? t(($) => $.detail.delete_dialog.deleting) : t(($) => $.detail.delete_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// `device_info` arrives as a single composite string the daemon assembles
// (e.g. "host.local · 2.1.121 (Claude Code)"). Splitting on the first
// " · " gives us a hostname half + a runtime-version half so each can be
// labelled separately in the Hero card. Older runtimes that report just a
// hostname still work — `runtime` is undefined in that case.
function parseDeviceInfo(raw: string): { hostname: string; runtime?: string } {
  const idx = raw.indexOf(" · ");
  if (idx < 0) return { hostname: raw };
  return {
    hostname: raw.slice(0, idx),
    runtime: raw.slice(idx + 3),
  };
}

function HeroCard({
  runtime,
  health,
  lastSeen,
  ownerMember,
  cliVersion,
  daemonShort,
}: {
  runtime: AgentRuntime;
  health: ReturnType<typeof deriveRuntimeHealth>;
  lastSeen: string;
  ownerMember: MemberWithUser | null;
  cliVersion: string | null;
  daemonShort: string | null;
}) {
  const { t } = useT("runtimes");
  const [showDetails, setShowDetails] = useState(false);
  const device = runtime.device_info ? parseDeviceInfo(runtime.device_info) : null;
  const hasTechDetails = !!cliVersion || !!daemonShort;

  return (
    <div className="rounded-lg border bg-card">
      {/* Identity row — provider logo, name, status badge, last seen. */}
      <div className="flex items-start gap-3 border-b p-4">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-card">
          <ProviderLogo provider={runtime.provider} className="h-5 w-5" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <h2 className="truncate text-base font-semibold tracking-tight">
              {runtime.name}
            </h2>
            <HealthBadge health={health} />
            <span className="text-xs text-muted-foreground">
              {t(($) => $.detail.last_seen, { when: lastSeen })}
            </span>
          </div>
        </div>
      </div>

      {/* User-visible facts — Owner / Device / Runtime, each labelled.
          Replaces the older dense `·`-separated meta strip that mixed
          everything (including dev-only IDs) at the same visual weight. */}
      <dl className="grid grid-cols-1 divide-y sm:grid-cols-3 sm:divide-x sm:divide-y-0">
        <Fact label="Owner">
          {ownerMember ? (
            <span className="inline-flex min-w-0 items-center gap-1.5">
              <ActorAvatar
                actorType="member"
                actorId={ownerMember.user_id}
                size={18}
                enableHoverCard
              />
              <span className="cursor-pointer truncate text-sm">{ownerMember.name}</span>
            </span>
          ) : (
            <span className="text-sm text-muted-foreground">—</span>
          )}
        </Fact>
        <Fact label="Device">
          {device?.hostname ? (
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="block truncate font-mono text-xs">
                    {device.hostname}
                  </span>
                }
              />
              <TooltipContent>{device.hostname}</TooltipContent>
            </Tooltip>
          ) : (
            <span className="text-sm text-muted-foreground">—</span>
          )}
        </Fact>
        <Fact label="Runtime">
          <span className="block truncate text-sm">
            {device?.runtime ?? (
              <span className="capitalize">{runtime.provider}</span>
            )}
          </span>
        </Fact>
      </dl>

      {/* Diagnostic IDs — multica CLI git hash + truncated daemon UUID.
          Only useful when filing an issue or reading logs; folded by
          default so they don't compete with the user-visible facts above. */}
      {hasTechDetails && (
        <div className="border-t">
          <button
            type="button"
            onClick={() => setShowDetails((v) => !v)}
            className="flex w-full items-center gap-1 px-4 py-2 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            <ChevronRight
              className={`h-3 w-3 transition-transform ${
                showDetails ? "rotate-90" : ""
              }`}
            />
            {t(($) => $.detail.technical_details)}
          </button>
          {showDetails && (
            <dl className="grid grid-cols-1 gap-y-2 border-t bg-muted/30 px-4 py-3 sm:grid-cols-2">
              {cliVersion && (
                <Fact label="Daemon CLI" mono compact>
                  {cliVersion}
                </Fact>
              )}
              {daemonShort && (
                <Fact label="Daemon ID" mono compact>
                  {daemonShort}
                </Fact>
              )}
            </dl>
          )}
        </div>
      )}
    </div>
  );
}

function Fact({
  label,
  children,
  mono,
  compact,
}: {
  label: string;
  children: React.ReactNode;
  mono?: boolean;
  compact?: boolean;
}) {
  return (
    <div className={`min-w-0 ${compact ? "" : "px-4 py-3"}`}>
      <dt className="text-[11px] uppercase tracking-wider text-muted-foreground">
        {label}
      </dt>
      <dd className={`mt-1 ${mono ? "font-mono text-xs" : ""}`}>{children}</dd>
    </div>
  );
}

function ServingAgentsCard({
  agents,
  presenceMap,
  agentHref,
}: {
  agents: Agent[];
  presenceMap: Map<string, AgentPresenceDetail>;
  agentHref: (agentId: string) => string;
}) {
  const { t } = useT("runtimes");
  const { t: tAgents } = useT("agents");
  return (
    <div className="rounded-lg border">
      <div className="flex items-center justify-between border-b px-4 py-2.5">
        <span className="text-xs font-semibold">{t(($) => $.detail.serving_title)}</span>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.detail.serving_count, { count: agents.length })}
        </span>
      </div>
      {agents.length === 0 ? (
        <div className="flex flex-col items-center px-4 py-6 text-center">
          <Cpu className="h-5 w-5 text-muted-foreground/40" />
          <p className="mt-2 text-xs text-muted-foreground">
            {t(($) => $.detail.no_agents)}
          </p>
        </div>
      ) : (
        <div className="divide-y">
          {agents.map((agent) => {
            const detail = presenceMap.get(agent.id);
            const av = detail
              ? availabilityConfig[detail.availability]
              : availabilityConfig.offline;
            const avLabel = tAgents(($) => $.availability[detail?.availability ?? "offline"]);
            const wl = detail ? workloadConfig[detail.workload] : null;
            const running = detail?.runningCount ?? 0;
            const queued = detail?.queuedCount ?? 0;
            return (
              <AppLink
                key={agent.id}
                href={agentHref(agent.id)}
                className="group flex items-center gap-2 px-4 py-2 transition-colors hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
              >
                <ActorAvatar actorType="agent" actorId={agent.id} size={20} enableHoverCard showStatusDot />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-xs font-medium">
                    {agent.name}
                  </div>
                  <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs">
                    <span className="inline-flex items-center gap-1.5">
                      <span className={`h-1.5 w-1.5 rounded-full ${av.dotClass}`} />
                      <span className={av.textClass}>{avLabel}</span>
                    </span>
                    {wl && detail && detail.workload !== "idle" && (
                      <span className={`inline-flex items-center gap-1 ${wl.textClass}`}>
                        <span className="text-muted-foreground">·</span>
                        <wl.icon
                          className={`h-3 w-3 ${detail.workload === "working" ? "animate-spin" : ""}`}
                        />
                        {tAgents(($) => $.workload[detail.workload])}
                        {running > 0 && (
                          <span className="text-muted-foreground">{t(($) => $.detail.running_chip, { count: running })}</span>
                        )}
                        {queued > 0 && (
                          <span className="text-muted-foreground">{t(($) => $.detail.queued_chip, { count: queued })}</span>
                        )}
                      </span>
                    )}
                  </div>
                </div>
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground/40 transition-colors group-hover:text-muted-foreground" />
              </AppLink>
            );
          })}
        </div>
      )}
    </div>
  );
}

function DiagnosticsCard({
  runtime,
  cliVersion,
  launchedBy,
  canDelete,
  isAdmin,
  sandboxMutation,
  onDelete,
}: {
  runtime: AgentRuntime;
  cliVersion: string | null;
  launchedBy: string | null;
  canDelete: boolean;
  isAdmin: boolean;
  sandboxMutation: ReturnType<typeof useUpdateRuntimeSandbox>;
  onDelete: () => void;
}) {
  const { t } = useT("runtimes");
  const isLocal = runtime.runtime_mode === "local";

  // CEREBRO-PATCH(runtime-detail-sandbox-section): admin-only sandbox override
  // toggle. Rendered for every member for transparency about the current
  // state, but the toggle is interactive only for owner/admin (matches the
  // server permission gate).
  const sandboxOverride = runtime.sandbox_enabled;
  // For the binary Switch we treat null (no override) as "on" — matches the
  // daemon-wide darwin default. The "Reset to default" link below shows up
  // whenever an explicit override is in place, so the inherit state is
  // recoverable without a tri-state control.
  const sandboxChecked = sandboxOverride === null ? true : sandboxOverride;
  const [sandboxError, setSandboxError] = useState<string | null>(null);

  const handleSandboxMutate = (next: boolean | null) => {
    setSandboxError(null);
    sandboxMutation.mutate(
      { runtimeId: runtime.id, sandboxEnabled: next },
      {
        onError: (e) => {
          // Server returns a JSON {error: "..."} body. The api client surfaces
          // it as Error.message — show it verbatim so the user sees the actual
          // reason (permission, validation, server-side state) instead of a
          // generic "Failed".
          const msg = e instanceof Error ? e.message : "Unknown error";
          setSandboxError(msg);
        },
      },
    );
  };

  return (
    <div className="rounded-lg border">
      <div className="border-b px-4 py-2.5">
        <span className="text-xs font-semibold">{t(($) => $.detail.diagnostics_title)}</span>
      </div>
      <div className="space-y-3 p-4">
        {isLocal && (
          <div>
            <div className="mb-1.5 text-[11px] uppercase tracking-wide text-muted-foreground">
              {t(($) => $.detail.diagnostics_cli)}
            </div>
            <UpdateSection
              runtimeId={runtime.id}
              currentVersion={cliVersion}
              isOnline={runtime.status === "online"}
              launchedBy={launchedBy}
            />
          </div>
        )}

        {/* CEREBRO-PATCH(runtime-detail-sandbox-block): macOS seatbelt sandbox
            override (local runtimes only). The sandbox is a no-op on Linux
            cloud runtimes, so showing the control there would just confuse
            operators. */}
        {isLocal && (
          <div className={isLocal ? "border-t pt-3" : ""}>
            <div className="mb-1.5 text-[11px] uppercase tracking-wide text-muted-foreground">
              Sandbox
            </div>
            <div className="rounded-md border p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="space-y-1">
                  <div className="text-xs font-medium">
                    Run agent CLIs in macOS sandbox
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {sandboxOverride === null
                      ? "Inheriting the daemon's MULTICA_ENABLE_SANDBOX setting (default on macOS)."
                      : sandboxOverride
                        ? "Sandbox is forced on for this runtime."
                        : "Sandbox is disabled for this runtime — agents run with full host access."}
                  </p>
                </div>
                <Switch
                  checked={sandboxChecked}
                  onCheckedChange={(next) => handleSandboxMutate(next)}
                  disabled={!isAdmin || sandboxMutation.isPending}
                  aria-label="Sandbox enabled"
                />
              </div>
              {!isAdmin && (
                <p className="mt-2 text-xs text-muted-foreground italic">
                  Only workspace owners and admins can change this setting.
                </p>
              )}
              {isAdmin && sandboxOverride !== null && (
                <button
                  type="button"
                  className="mt-2 text-xs text-muted-foreground underline-offset-2 hover:underline disabled:opacity-50"
                  onClick={() => handleSandboxMutate(null)}
                  disabled={sandboxMutation.isPending}
                >
                  Reset to default
                </button>
              )}
              {sandboxError && (
                <div
                  role="alert"
                  className="mt-2 rounded-md border border-destructive/50 bg-destructive/10 p-2 text-xs"
                >
                  <div className="font-medium text-destructive">
                    Couldn&apos;t update sandbox setting
                  </div>
                  <p className="mt-1 text-destructive/90 break-words">
                    {sandboxError}
                  </p>
                  <button
                    type="button"
                    className="mt-1.5 text-destructive/80 underline-offset-2 hover:underline"
                    onClick={() => setSandboxError(null)}
                  >
                    Dismiss
                  </button>
                </div>
              )}
            </div>
          </div>
        )}

        {canDelete && (
          <div className="border-t pt-3">
            <Button
              variant="ghost"
              size="sm"
              className="h-8 w-full justify-start gap-2 text-destructive hover:bg-destructive/10 hover:text-destructive"
              onClick={onDelete}
            >
              <Trash2 className="h-3.5 w-3.5" />
              {t(($) => $.detail.delete_button)}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
