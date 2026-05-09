"use client";

import { useEffect, useState } from "react";
import {
  ArrowLeft,
  Trash2,
  ChevronRight,
  Cpu,
  Globe,
  Lock,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type {
  AgentRuntime,
  Agent,
  MemberWithUser,
  RuntimeVisibility,
} from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import {
  useDeleteRuntime,
  useUpdateRuntimeVisibility,
} from "@multica/core/runtimes/mutations";
import { deriveRuntimeHealth } from "@multica/core/runtimes";
import {
  type AgentPresenceDetail,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
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
import { AppLink } from "../../navigation";
import { availabilityConfig, workloadConfig } from "../../agents/presence";
import { formatLastSeen } from "../utils";
import { HealthBadge } from "./shared";
import { ProviderLogo } from "./provider-logo";
import { UpdateSection } from "./update-section";
import { UsageSection } from "./usage-section";
import { useT } from "../../i18n";

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

          {/* Right rail: serving agents + visibility + diagnostics */}
          <div className="space-y-4">
            <ServingAgentsCard
              agents={servingAgents}
              presenceMap={presenceMap}
              agentHref={(id) => paths.agentDetail(id)}
            />
            <VisibilityCard runtime={runtime} isRuntimeOwner={!!isRuntimeOwner} />
            <DiagnosticsCard
              runtime={runtime}
              cliVersion={cliVersion}
              launchedBy={launchedBy}
              canDelete={!!canDelete}
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

// VisibilityCard — runtime owner controls workspace/private visibility.
// Non-owners (including admins) see a read-only label so they understand
// the row's semantics, but can't change it. List-level filtering means
// non-owners don't see "private" runtimes at all, so they only ever land
// here on workspace-visibility runtimes — but the label still fires for
// completeness if e.g. they navigate via a stale link.
function VisibilityCard({
  runtime,
  isRuntimeOwner,
}: {
  runtime: AgentRuntime;
  isRuntimeOwner: boolean;
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const mutation = useUpdateRuntimeVisibility(wsId);

  const flip = (visibility: RuntimeVisibility) => {
    if (visibility === runtime.visibility || mutation.isPending) return;
    mutation.mutate(
      { runtimeId: runtime.id, visibility },
      {
        onSuccess: () => toast.success(t(($) => $.detail.visibility_toast_updated)),
        onError: (e) =>
          toast.error(e instanceof Error ? e.message : t(($) => $.detail.visibility_toast_failed)),
      },
    );
  };

  if (!isRuntimeOwner) {
    const Icon = runtime.visibility === "private" ? Lock : Globe;
    const label =
      runtime.visibility === "private"
        ? t(($) => $.detail.visibility_static_private)
        : t(($) => $.detail.visibility_static_workspace);
    return (
      <div className="rounded-lg border">
        <div className="border-b px-4 py-2.5">
          <span className="text-xs font-semibold">{t(($) => $.detail.visibility_title)}</span>
        </div>
        <div className="flex items-center gap-2 p-4 text-sm text-muted-foreground">
          <Icon className="h-4 w-4 shrink-0" />
          {label}
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-lg border">
      <div className="border-b px-4 py-2.5">
        <span className="text-xs font-semibold">{t(($) => $.detail.visibility_title)}</span>
      </div>
      <div className="flex flex-col gap-2 p-4">
        <button
          type="button"
          onClick={() => flip("workspace")}
          disabled={mutation.isPending}
          className={`flex items-center gap-2 rounded-md border px-3 py-2.5 text-left text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
            runtime.visibility === "workspace"
              ? "border-primary bg-primary/5"
              : "border-border hover:bg-muted"
          }`}
        >
          <Globe className="h-4 w-4 shrink-0 text-muted-foreground" />
          <div>
            <div className="font-medium">{t(($) => $.detail.visibility_workspace)}</div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.detail.visibility_workspace_hint)}
            </div>
          </div>
        </button>
        <button
          type="button"
          onClick={() => flip("private")}
          disabled={mutation.isPending}
          className={`flex items-center gap-2 rounded-md border px-3 py-2.5 text-left text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
            runtime.visibility === "private"
              ? "border-primary bg-primary/5"
              : "border-border hover:bg-muted"
          }`}
        >
          <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
          <div>
            <div className="font-medium">{t(($) => $.detail.visibility_private)}</div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.detail.visibility_private_hint)}
            </div>
          </div>
        </button>
      </div>
    </div>
  );
}

function DiagnosticsCard({
  runtime,
  cliVersion,
  launchedBy,
  canDelete,
  onDelete,
}: {
  runtime: AgentRuntime;
  cliVersion: string | null;
  launchedBy: string | null;
  canDelete: boolean;
  onDelete: () => void;
}) {
  const { t } = useT("runtimes");
  const isLocal = runtime.runtime_mode === "local";
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
        {canDelete && (
          <div className={isLocal ? "border-t pt-3" : ""}>
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
