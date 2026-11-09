"use client";

import { useState } from "react";
import { AlertCircle, Plus } from "@multica/ui/icons";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { isRuntimeUsableForUser } from "@multica/core/runtimes";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import {
  QODER_REGIONS,
  qoderOptions,
  useDisconnectQoder,
  useResumeQoder,
  useStopQoder,
  type QoderConnection,
} from "@multica/core/runtimes/qoder";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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
import { cn } from "@multica/ui/lib/utils";
import { CollectionPageState } from "../../layout/collection-page";
import {
  OBJECT_PAGE_BODY,
  ObjectIdentity,
  ObjectPage,
  ObjectSection,
} from "../../layout/object-page";
import { AppLink, useNavigation } from "../../navigation";
import { createPathWithParams } from "../../agents/create/squad-param";
import { useT } from "../../i18n";
import { ProviderLogo } from "./provider-logo";
import { QoderConfiguration } from "./qoder-configuration";
import {
  QoderConnectionCard,
  QoderLimitsCard,
  QoderRepositoriesCard,
  QoderSection,
  savedQoderInput,
} from "./qoder-connection-cards";
import { QoderStatusBadge } from "./qoder-status-badge";
import { ServingAgentList } from "./runtime-detail";
import { UsageSection } from "./usage-section";
import { parseActiveAgentsConflict } from "./delete-runtime-dialog";

/** Route locator of the workspace's Qoder Cloud Agents page. */
export const QODER_RUNTIME_LOCATOR = "qoder";

const LAYOUT_ID = "multica_runtime_detail_layout";

/**
 * Whether a runtimes route locator names Qoder: its own locator, the machine
 * id its runtime registers under, or the id of a Qoder runtime.
 */
export function isQoderLocator(
  locator: string,
  runtimes: AgentRuntime[],
): boolean {
  return (
    locator === QODER_RUNTIME_LOCATOR ||
    locator.startsWith("cloud:qoder-managed-") ||
    runtimes.some(
      (runtime) => runtime.id === locator && runtime.provider === "qoder_cloud",
    )
  );
}

/**
 * The workspace's Qoder Cloud Agents: one shared connection, so the page
 * manages the connection directly instead of listing harnesses on a machine.
 */
export function QoderDetailPage() {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const connection = useQuery(qoderOptions(wsId));
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);

  const title = t(($) => $.qoder.cloud_title);
  const segments = [{ href: paths.runtimes(), label: t(($) => $.page.title) }];
  const leaf = (
    <span className="flex min-w-0 items-center gap-1.5">
      <ProviderLogo provider="qoder_cloud" className="size-3.5 shrink-0" />
      <span className="truncate">{title}</span>
    </span>
  );

  if (connection.isPending || runtimesLoading) {
    return (
      <ObjectPage layoutId={LAYOUT_ID} segments={segments} leaf={leaf}>
        <QoderBodySkeleton />
      </ObjectPage>
    );
  }
  const data = connection.data;
  if (connection.isError || data?.available !== true) {
    return (
      <ObjectPage layoutId={LAYOUT_ID} segments={segments} leaf={leaf}>
        <CollectionPageState
          icon={AlertCircle}
          tone="destructive"
          title={
            connection.isError
              ? connection.error.message
              : t(($) => $.qoder.unavailable)
          }
          actions={
            <Button
              size="sm"
              variant="outline"
              render={<AppLink href={paths.runtimes()} />}
              nativeButton={false}
            >
              {t(($) => $.detail.all_runtimes)}
            </Button>
          }
        />
      </ObjectPage>
    );
  }

  const member = members.find((m) => m.user_id === userId);
  const isAdmin = member?.role === "owner" || member?.role === "admin";
  const qoderRuntimes = runtimes.filter(
    (runtime) => runtime.provider === "qoder_cloud",
  );
  const runtime =
    qoderRuntimes.find((item) => item.status === "online") ??
    qoderRuntimes[0] ??
    null;
  const boundAgents = agents.filter(
    (agent) =>
      !agent.archived_at &&
      qoderRuntimes.some((item) => item.id === agent.runtime_id),
  );
  const ready =
    data.configured &&
    data.enabled &&
    data.status === "online" &&
    runtime?.status === "online";

  const failing = data.enabled && data.status === "error" && !!data.lastError;

  return (
    <ObjectPage
      layoutId={LAYOUT_ID}
      segments={segments}
      leaf={leaf}
      actions={
        ready && runtime ? (
          <Button
            size="sm"
            onClick={() =>
              navigation.push(
                createPathWithParams(paths.newAgentManual(), {
                  runtime: runtime.id,
                }),
              )
            }
          >
            <Plus aria-hidden="true" className="size-3.5" />
            {t(($) => $.qoder.create_agent)}
          </Button>
        ) : undefined
      }
    >
      <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
        <div className="space-y-3">
          <ObjectIdentity
            media={
              <div className="flex size-12 items-center justify-center rounded-xl border bg-muted/50">
                <ProviderLogo provider="qoder_cloud" className="size-6" />
              </div>
            }
            title={title}
            trailing={<QoderStatusBadge connection={data} />}
            description={
              data.configured ? (
                <QoderSummary data={data} agentCount={boundAgents.length} />
              ) : undefined
            }
          />
          {failing && (
            <p
              role="alert"
              className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2 text-caption text-destructive"
            >
              {data.lastError}
            </p>
          )}
        </div>

        {data.configured ? (
          <>
            <QoderConnectionCard wsId={wsId} data={data} canManage={isAdmin} />
            <QoderRepositoriesCard wsId={wsId} data={data} canManage={isAdmin} />
          </>
        ) : (
          <QoderSection title={t(($) => $.qoder.page.configuration)}>
            <div className="p-4">
              {isAdmin ? (
                <QoderConfiguration />
              ) : (
                <div className="py-6 text-center">
                  <p className="text-body font-medium">
                    {t(($) => $.qoder.page.not_connected_title)}
                  </p>
                  <p className="mt-1 text-caption text-muted-foreground">
                    {t(($) => $.qoder.page.not_connected_hint)}
                  </p>
                </div>
              )}
            </div>
          </QoderSection>
        )}

        <ObjectSection
          title={t(($) => $.page.columns.agents)}
          count={boundAgents.length}
        >
          <ServingAgentList
            agents={boundAgents}
            presenceMap={presenceMap}
            agentHref={(id) => paths.agentDetail(id)}
          />
        </ObjectSection>

        {data.configured && (
          <QoderLimitsCard wsId={wsId} data={data} canManage={isAdmin} />
        )}
        {runtime && isRuntimeUsableForUser(runtime, userId) && (
          <ObjectSection title={t(($) => $.detail.usage_title)}>
            <UsageSection runtime={runtime} />
          </ObjectSection>
        )}
        {isAdmin && data.configured && (
          <ManageCard
            wsId={wsId}
            data={data}
            boundAgents={boundAgents}
            onDisconnected={() => {
              navigation.replace(paths.runtimes());
              toast.success(t(($) => $.qoder.disconnect_confirm.done));
            }}
          />
        )}
      </div>
    </ObjectPage>
  );
}

function QoderSummary({
  data,
  agentCount,
}: {
  data: QoderConnection;
  agentCount: number;
}) {
  const { t } = useT("runtimes");
  const region =
    data.baseUrl === QODER_REGIONS.cn
      ? t(($) => $.qoder.region_cn)
      : t(($) => $.qoder.region_global);
  return (
    <span className="flex flex-wrap items-center gap-x-1.5">
      <span>{region}</span>
      <span aria-hidden="true">·</span>
      <span>{t(($) => $.qoder.agent_count, { count: agentCount })}</span>
      <span aria-hidden="true">·</span>
      <span className="tabular-nums">
        {data.enabled && data.status === "online"
          ? t(($) => $.qoder.usage, {
              active: data.activeRuns,
              limit: data.maxConcurrency,
              queued: data.queuedRuns,
            })
          : t(($) => $.qoder.page.workload_idle)}
      </span>
    </span>
  );
}

function ManageCard({
  wsId,
  data,
  boundAgents,
  onDisconnected,
}: {
  wsId: string;
  data: QoderConnection;
  boundAgents: Agent[];
  onDisconnected: () => void;
}) {
  const { t } = useT("runtimes");
  const stop = useStopQoder(wsId);
  const resumeConnection = useResumeQoder(wsId);
  const [pauseOpen, setPauseOpen] = useState(false);
  const [disconnectOpen, setDisconnectOpen] = useState(false);
  const busy = stop.isPending || resumeConnection.isPending;

  const resume = () =>
    resumeConnection.mutate(savedQoderInput(data, t(($) => $.qoder.cloud_title)), {
      onSuccess: () => toast.success(t(($) => $.qoder.page.resumed)),
      onError: (err) => toast.error(err.message),
    });

  return (
    <QoderSection title={t(($) => $.qoder.page.manage_title)}>
      <div className="divide-y">
        {data.enabled ? (
          <ManageRow
            title={t(($) => $.qoder.page.pause_title)}
            hint={t(($) => $.qoder.page.pause_hint)}
          >
            <Button
              variant="outline"
              size="xs"
              disabled={busy}
              onClick={() => setPauseOpen(true)}
            >
              {t(($) => $.qoder.stop)}
            </Button>
          </ManageRow>
        ) : (
          <ManageRow
            title={t(($) => $.qoder.page.resume_title)}
          >
            <Button
              variant="outline"
              size="xs"
              disabled={busy}
              aria-busy={resumeConnection.isPending}
              onClick={resume}
            >
              {t(($) => $.qoder.page.resume)}
            </Button>
          </ManageRow>
        )}
        <ManageRow
          title={t(($) => $.qoder.page.disconnect_title)}
          hint={t(($) => $.qoder.page.disconnect_hint)}
          destructive
        >
          <Button
            variant="destructive"
            size="xs"
            disabled={busy}
            onClick={() => setDisconnectOpen(true)}
          >
            {t(($) => $.qoder.page.disconnect)}
          </Button>
        </ManageRow>
      </div>

      <AlertDialog
        open={pauseOpen}
        onOpenChange={(open) => {
          if (!open && !stop.isPending) setPauseOpen(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.qoder.stop_confirm.title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {boundAgents.length > 0
                ? t(($) => $.qoder.stop_confirm.description_agents, {
                    count: boundAgents.length,
                  })
                : t(($) => $.qoder.stop_confirm.description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AgentNames agents={boundAgents} />
          <AlertDialogFooter>
            <AlertDialogCancel disabled={stop.isPending}>
              {t(($) => $.qoder.stop_confirm.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={stop.isPending}
              onClick={() =>
                stop.mutate(undefined, {
                  onError: (err) => toast.error(err.message),
                  onSettled: () => setPauseOpen(false),
                })
              }
            >
              {t(($) => $.qoder.stop)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {disconnectOpen && (
        <DisconnectQoderDialog
          wsId={wsId}
          agents={boundAgents}
          onClose={() => setDisconnectOpen(false)}
          onDisconnected={onDisconnected}
        />
      )}
    </QoderSection>
  );
}

function ManageRow({
  title,
  hint,
  destructive,
  children,
}: {
  title: string;
  hint?: string;
  destructive?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1 px-4 py-3">
      <div className="flex items-center justify-between gap-3">
        <p
          className={cn(
            "min-w-0 text-body font-medium",
            destructive && "text-destructive",
          )}
        >
          {title}
        </p>
        <div className="shrink-0">{children}</div>
      </div>
      {hint ? <p className="text-pretty text-caption text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

function AgentNames({ agents }: { agents: Agent[] }) {
  if (agents.length === 0) return null;
  return (
    <ul className="max-h-40 space-y-1 overflow-y-auto rounded-md bg-muted/60 px-3 py-2 text-caption">
      {agents.map((agent) => (
        <li key={agent.id} className="truncate">
          {agent.name}
        </li>
      ))}
    </ul>
  );
}

/**
 * Confirms the agents that lose their runtime. The server refuses when that
 * set changed since the dialog opened; the dialog then shows the new set and
 * asks again.
 */
export function DisconnectQoderDialog({
  wsId,
  agents,
  onClose,
  onDisconnected,
}: {
  wsId: string;
  agents: Agent[];
  onClose: () => void;
  onDisconnected: () => void;
}) {
  const { t } = useT("runtimes");
  const disconnect = useDisconnectQoder(wsId);
  const [planAgents, setPlanAgents] = useState(agents);
  const [notice, setNotice] = useState<string | null>(null);

  const confirm = () =>
    disconnect.mutate(
      planAgents.map((agent) => agent.id),
      {
        onSuccess: onDisconnected,
        onError: (err) => {
          const conflict = parseActiveAgentsConflict(err);
          if (conflict?.code === "runtime_delete_plan_changed") {
            setPlanAgents(conflict.activeAgents);
            setNotice(t(($) => $.qoder.disconnect_confirm.plan_changed));
            return;
          }
          toast.error(err.message);
        },
      },
    );

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !disconnect.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t(($) => $.qoder.disconnect_confirm.title)}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.qoder.disconnect_confirm.description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {planAgents.length > 0 && (
          <div className="space-y-2">
            <p className="text-caption text-muted-foreground">
              {t(($) => $.qoder.disconnect_confirm.description_agents, {
                count: planAgents.length,
              })}
            </p>
            <AgentNames agents={planAgents} />
          </div>
        )}
        {notice && (
          <p role="alert" className="text-caption text-warning">
            {notice}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={disconnect.isPending}>
            {t(($) => $.qoder.disconnect_confirm.cancel)}
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={disconnect.isPending}
            aria-busy={disconnect.isPending}
            onClick={confirm}
          >
            {disconnect.isPending
              ? t(($) => $.qoder.disconnect_confirm.working)
              : t(($) => $.qoder.disconnect_confirm.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function QoderBodySkeleton() {
  return (
    <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
      <div className="flex items-center gap-3">
        <Skeleton className="size-12 rounded-xl" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-5 w-56" />
          <Skeleton className="h-4 w-40" />
        </div>
      </div>
      <Skeleton className="h-64 w-full rounded-lg" />
    </div>
  );
}
