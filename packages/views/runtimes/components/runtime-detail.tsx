"use client";

import { useEffect, useState } from "react";
import {
  ChevronRight,
  Lock,
  MoreHorizontal,
  Trash2,
} from "@multica/ui/icons";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type {
  AgentRuntime,
  Agent,
  MemberWithUser,
  RuntimeProfile,
} from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import {
  deriveRuntimeHealth,
  isRuntimeUsableForUser,
  runtimeDisplayName,
  runtimeProfileListOptions,
} from "@multica/core/runtimes";
import {
  type AgentPresenceDetail,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { useWorkspacePaths } from "@multica/core/paths";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  OBJECT_PAGE_BODY,
  ObjectIdentity,
  ObjectPage,
  ObjectPanelSection,
  ObjectProperty,
  ObjectSection,
  ObjectSectionCard,
} from "../../layout/object-page";
import { AppLink, useNavigation } from "../../navigation";
import { availabilityConfig, workloadConfig } from "../../agents/presence";
import { HealthBadge, HealthIcon, shortDaemonId, useHealthLabel } from "./shared";
import { ProviderLogo } from "./provider-logo";
import { UsageSection } from "./usage-section";
import { DeleteRuntimeDialog } from "./delete-runtime-dialog";
import { RuntimeAccessEditor, RuntimeAccessReadout } from "./runtime-access-editor";
import { DeleteRuntimeProfileDialog } from "./delete-runtime-profile-dialog";
import { runtimeRowLabel } from "./runtime-machines";
import { useT, useTimeAgo } from "../../i18n";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";

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

// 30s tick keeps derived runtime health honest as time-based windows
// (recently_lost → offline → long_offline) cross thresholds without any new
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

export function RuntimeDetail({
  runtime,
  machineHref,
  machineLabel,
  afterDeleteHref,
}: {
  runtime: AgentRuntime;
  machineHref?: string;
  machineLabel?: string;
  afterDeleteHref?: string;
}) {
  const { t } = useT("runtimes");
  const timeAgo = useTimeAgo();
  const cliVersion =
    runtime.runtime_mode === "local" ? getCliVersion(runtime.metadata) : null;
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: profiles = [] } = useQuery(runtimeProfileListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);
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
  const canEditRuntime = isAdmin || isRuntimeOwner;
  const canReadRuntime = isRuntimeUsableForUser(runtime, user?.id ?? null);
  const runtimeProfile: RuntimeProfile | null = runtime.profile_id
    ? profiles.find((p) => p.id === runtime.profile_id) ?? null
    : null;
  const isCustomRuntime = !!runtime.profile_id;
  const canDelete = isCustomRuntime
    ? isAdmin && !!runtimeProfile
    : canEditRuntime;

  const servingAgents = agents.filter(
    (a) => a.runtime_id === runtime.id && !a.archived_at,
  );

  const deleteDestination = afterDeleteHref ?? paths.runtimes();

  const handleRuntimeDeleted = () => {
    setDeleteOpen(false);
    navigation.replace(deleteDestination);
    toast.success(t(($) => $.detail.toast_deleted));
  };

  const handleProfileDeleted = () => {
    setDeleteOpen(false);
    navigation.replace(deleteDestination);
  };

  const lastSeen = runtime.last_seen_at
    ? timeAgo(runtime.last_seen_at)
    : t(($) => $.detail.never_seen);
  const runtimeName = machineLabel
    ? runtimeRowLabel(runtime, machineLabel)
    : runtimeDisplayName(runtime);
  const device = runtime.device_info ? parseDeviceInfo(runtime.device_info) : null;
  const summary = [device?.runtime, t(($) => $.detail.last_seen, { when: lastSeen })]
    .filter(Boolean)
    .join(" · ");

  return (
    <>
      <ObjectPage
        layoutId="multica_runtime_detail_layout"
        segments={[
          { href: paths.runtimes(), label: t(($) => $.page.title) },
          ...(machineHref && machineLabel
            ? [{ href: machineHref, label: machineLabel }]
            : []),
        ]}
        leaf={
          <span className="flex min-w-0 items-center gap-1.5">
            <ProviderLogo provider={runtime.provider} className="size-3.5 shrink-0" />
            <span className="truncate">{runtimeName}</span>
          </span>
        }
        actions={
          canDelete ? (
            <RuntimeActionsMenu onDelete={() => setDeleteOpen(true)} />
          ) : undefined
        }
        panel={
          <RuntimeProperties
            runtime={runtime}
            health={health}
            lastSeen={lastSeen}
            ownerMember={ownerMember}
            device={device}
            cliVersion={cliVersion}
            canEditVisibility={!!isRuntimeOwner}
          />
        }
      >
        <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
          <ObjectIdentity
            media={
              <div className="flex size-12 items-center justify-center rounded-xl border bg-muted/50">
                <ProviderLogo provider={runtime.provider} className="size-6" />
              </div>
            }
            title={runtimeName}
            trailing={
              <>
                <HealthBadge health={health} />
                {!canEditRuntime && (
                  <span className="inline-flex shrink-0 items-center gap-1 text-caption text-muted-foreground">
                    <Lock className="size-3" />
                    {t(($) => $.detail.read_only)}
                  </span>
                )}
              </>
            }
            description={summary}
          />

          <ObjectSection
            title={t(($) => $.page.columns.agents)}
            count={servingAgents.length}
          >
            <ServingAgentList
              agents={servingAgents}
              presenceMap={presenceMap}
              agentHref={(id) => paths.agentDetail(id)}
            />
          </ObjectSection>

          {canReadRuntime && (
            <ObjectSection title={t(($) => $.detail.usage_title)}>
              <UsageSection runtime={runtime} />
            </ObjectSection>
          )}
        </div>
      </ObjectPage>

      {isCustomRuntime && runtimeProfile ? (
        <DeleteRuntimeProfileDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          profile={runtimeProfile}
          wsId={wsId}
          onDeleted={handleProfileDeleted}
        />
      ) : (
        <DeleteRuntimeDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          runtime={runtime}
          wsId={wsId}
          onDeleted={handleRuntimeDeleted}
        />
      )}
    </>
  );
}

// `device_info` arrives as a single composite string the daemon assembles
// (e.g. "host.local · 2.1.121 (Claude Code)"). Splitting on the first
// " · " gives us a hostname half + a runtime-version half so each can be
// labelled separately. Older runtimes that report just a hostname still
// work — `runtime` is undefined in that case.
function parseDeviceInfo(raw: string): { hostname: string; runtime?: string } {
  const idx = raw.indexOf(" · ");
  if (idx < 0) return { hostname: raw };
  return {
    hostname: raw.slice(0, idx),
    runtime: raw.slice(idx + 3),
  };
}

function RuntimeActionsMenu({ onDelete }: { onDelete: () => void }) {
  const { t } = useT("runtimes");
  const { t: tCommon } = useT("common");
  // Delete stays available even when the runtime is a live local daemon
  // (self-healing, MUL-3352); the dialog explains the trade-off.
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton
            label={tCommon(($) => $.more_actions)}
            tooltipSide="bottom"
            className="text-muted-foreground"
          >
            <MoreHorizontal />
          </IconButton>
        }
      />
      <DropdownMenuContent align="end" className="w-auto">
        <DropdownMenuItem variant="destructive" onClick={onDelete}>
          <Trash2 className="size-3.5" />
          {t(($) => $.detail.delete_button)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function RuntimeProperties({
  runtime,
  health,
  lastSeen,
  ownerMember,
  device,
  cliVersion,
  canEditVisibility,
}: {
  runtime: AgentRuntime;
  health: ReturnType<typeof deriveRuntimeHealth>;
  lastSeen: string;
  ownerMember: MemberWithUser | null;
  device: { hostname: string; runtime?: string } | null;
  cliVersion: string | null;
  /**
   * Runtime owner only (MUL-6126). Sharing a machine with the workspace is
   * the owner's call, so a workspace admin sees the read-only chip even
   * though they may still rename or delete the runtime.
   */
  canEditVisibility: boolean;
}) {
  const { t } = useT("runtimes");
  const { t: tLayout } = useT("layout");
  const healthLabel = useHealthLabel();
  const daemonShort = shortDaemonId(runtime.daemon_id);

  return (
    <div className="space-y-5">
      <ObjectPanelSection title={tLayout(($) => $.object_page.properties)}>
        <ObjectProperty label={t(($) => $.page.columns.status)}>
          <HealthIcon health={health} />
          <span className="truncate">
            {health === "online" ? healthLabel(health) : `${healthLabel(health)} · ${lastSeen}`}
          </span>
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.fact_owner)}>
          {ownerMember ? (
            <>
              <ActorAvatar
                actorType="member"
                actorId={ownerMember.user_id}
                size="xs"
                enableHoverCard
              />
              <span className="truncate">{ownerMember.name}</span>
            </>
          ) : (
            <span className="text-muted-foreground">—</span>
          )}
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.fact_device)}>
          {device?.hostname ? (
            <TruncatedTooltip content={device.hostname}>
              <span className="block truncate font-mono">{device.hostname}</span>
            </TruncatedTooltip>
          ) : (
            <span className="text-muted-foreground">—</span>
          )}
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.fact_runtime)}>
          <span className="truncate">
            {device?.runtime ?? <span className="capitalize">{runtime.provider}</span>}
          </span>
        </ObjectProperty>
        {cliVersion && (
          <ObjectProperty label={t(($) => $.detail.fact_daemon_cli)}>
            <span className="truncate font-mono">{cliVersion}</span>
          </ObjectProperty>
        )}
        {daemonShort && (
          <ObjectProperty label={t(($) => $.detail.fact_daemon_id)}>
            <span className="truncate font-mono">{daemonShort}</span>
          </ObjectProperty>
        )}
      </ObjectPanelSection>
      <ObjectPanelSection title={t(($) => $.detail.diagnostics_visibility)}>
        <div className="pr-0.5 pt-1">
          {canEditVisibility ? (
            <RuntimeAccessEditor runtime={runtime} />
          ) : (
            <RuntimeAccessReadout runtime={runtime} />
          )}
        </div>
      </ObjectPanelSection>
    </div>
  );
}

export function ServingAgentList({
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
  if (agents.length === 0) {
    return (
      <p className="rounded-lg border border-dashed px-4 py-6 text-center text-caption text-muted-foreground">
        {t(($) => $.detail.no_agents)}
      </p>
    );
  }
  return (
    <ObjectSectionCard>
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
            className="group flex items-center gap-2.5 px-3 py-2 transition-colors hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none"
          >
            <ActorAvatar actorType="agent" actorId={agent.id} size="sm" enableHoverCard showStatusDot />
            <span className="min-w-0 flex-1 truncate text-body font-medium">
              {agent.name}
            </span>
            <span className="flex shrink-0 items-center gap-x-1.5 text-caption">
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
            </span>
            <ChevronRight className="h-3.5 w-3.5 shrink-0 text-faint-foreground transition-colors group-hover:text-muted-foreground" />
          </AppLink>
        );
      })}
    </ObjectSectionCard>
  );
}
