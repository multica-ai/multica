"use client";

import { useState } from "react";
import {
  AlertCircle,
  ArrowLeft,
  Bot,
  Clock3,
  Lock,
  MoreHorizontal,
  Plus,
  Server,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  Agent,
  AgentRuntime,
  UpdateAgentRequest,
} from "@multica/core/types";
import {
  type AgentPresenceDetail,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { api, ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import { useAgentPermissions } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { CapabilityBanner } from "@multica/ui/components/common/capability-banner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink, useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { ActorAvatar } from "../../common/actor-avatar";
import { AgentPresenceIndicator } from "./agent-presence-indicator";
import { VisibilityBadge } from "./visibility-badge";
import { AgentOverviewPane, type DetailTab } from "./agent-overview-pane";
import { useT, useTimeAgo } from "../../i18n";

interface AgentDetailPageProps {
  agentId: string;
}

export function AgentDetailPage({ agentId }: AgentDetailPageProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const qc = useQueryClient();
  const currentUser = useAuthStore((s) => s.user);

  const {
    data: agents = [],
    isLoading: agentsLoading,
    error: agentsError,
    refetch: refetchAgents,
  } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  // Single workspace-level presence pass; this page just reads its slot.
  // The hook owns the 30s tick so the failed-window auto-clears here too.
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);

  const agent = agents.find((a) => a.id === agentId) ?? null;
  const presence: AgentPresenceDetail | null =
    agent ? presenceMap.get(agent.id) ?? null : null;

  // Fallback fetch: when the agent is missing from the workspace list, hit
  // GET /api/agents/{id} directly to disambiguate "doesn't exist" (404) from
  // "you can't see this private agent" (403). Only fires after the list has
  // settled, so the common path makes zero extra requests.
  const { error: detailError } = useQuery({
    queryKey: ["agent-detail-probe", wsId, agentId],
    queryFn: () => api.getAgent(agentId),
    enabled: !agentsLoading && !agent && !!agentId,
    retry: false,
  });
  const isForbidden =
    detailError instanceof ApiError && detailError.status === 403;

  // Permission hook MUST be called unconditionally — its `agent | null`
  // signature handles the not-found / loading case internally so the early
  // returns below don't violate the rules of hooks. Backend gates archive
  // and restore identically to edit, so a single `canEdit` covers them all.
  const { canAssign, canEdit } = useAgentPermissions(agent, wsId);

  const [confirmArchive, setConfirmArchive] = useState(false);

  // One-shot channel: the inspector's compact Lark status row asks the
  // overview pane to focus a tab. The pane clears it after consuming.
  const [tabNavIntent, setTabNavIntent] = useState<DetailTab | null>(null);

  const handleUpdate = async (id: string, data: Record<string, unknown>) => {
    // Optimistic update: patch the matching agent in the cached list
    // BEFORE the network round-trip so the inspector picker chips flip to
    // the new value immediately on click. Without this, every inspector
    // picker (thinking / visibility / concurrency / model / runtime) waits
    // 0.5-2s for the API response + invalidate + refetch before the trigger
    // updates — readable as obvious lag in the UI.
    //
    // On error we rollback only the fields THIS call wrote, leaving any
    // other concurrently-mutated fields untouched, then invalidate so the
    // cache converges with the server. A whole-list snapshot rollback
    // would clobber a concurrent successful mutation if the failing call
    // resolves last (e.g. flipping visibility then runtime simultaneously
    // and only the visibility PATCH fails).
    const queryKey = workspaceKeys.agents(wsId);
    const prevAgents = qc.getQueryData<Agent[]>(queryKey);
    const prevAgent = prevAgents?.find((a) => a.id === id);
    const prevFields: Record<string, unknown> = {};
    if (prevAgent) {
      for (const key of Object.keys(data)) {
        prevFields[key] = (prevAgent as unknown as Record<string, unknown>)[key];
      }
    }
    qc.setQueryData<Agent[]>(queryKey, (old) =>
      old?.map((a) => (a.id === id ? ({ ...a, ...data } as Agent) : a)),
    );
    try {
      await api.updateAgent(id, data as UpdateAgentRequest);
      qc.invalidateQueries({ queryKey });
      toast.success(t(($) => $.detail.agent_updated_toast));
    } catch (e) {
      if (prevAgent) {
        qc.setQueryData<Agent[]>(queryKey, (old) =>
          old?.map((a) =>
            a.id === id ? ({ ...a, ...prevFields } as Agent) : a,
          ),
        );
      }
      qc.invalidateQueries({ queryKey });
      toast.error(e instanceof Error ? e.message : t(($) => $.detail.update_failed_toast));
      throw e;
    }
  };

  const handleArchive = async (id: string) => {
    try {
      await api.archiveAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.detail.agent_archived_toast));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.detail.archive_failed_toast));
    }
  };

  const handleRestore = async (id: string) => {
    try {
      await api.restoreAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.detail.agent_restored_toast));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.detail.restore_failed_toast));
    }
  };

  // --- Loading ---
  if (agentsLoading && !agent) {
    return <DetailLoadingSkeleton />;
  }

  // --- No permission (private agent the caller is not in allowed_principals for) ---
  if (!agent && isForbidden) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <BackHeader paths={paths.agents()} title={t(($) => $.detail.back_to_agents)} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <Lock className="h-8 w-8 text-muted-foreground" />
          <div>
            <p className="text-sm font-medium">{t(($) => $.detail.no_access_title)}</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {t(($) => $.detail.no_access_hint)}
            </p>
          </div>
          <Button
            type="button"
            size="sm"
            onClick={() => navigation.push(paths.agents())}
          >
            {t(($) => $.detail.back_to_agents_full)}
          </Button>
        </div>
      </div>
    );
  }

  // --- Not found / error ---
  if (!agent) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <BackHeader paths={paths.agents()} title={t(($) => $.detail.back_to_agents)} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <div>
            <p className="text-sm font-medium">{t(($) => $.detail.not_found_title)}</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {agentsError instanceof Error
                ? agentsError.message
                : t(($) => $.detail.not_found_default)}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => refetchAgents()}
            >
              {t(($) => $.detail.try_again)}
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={() => navigation.push(paths.agents())}
            >
              {t(($) => $.detail.back_to_agents_full)}
            </Button>
          </div>
        </div>
      </div>
    );
  }

  const isArchived = !!agent.archived_at;
  const runtime = agent.runtime_id
    ? runtimes.find((r) => r.id === agent.runtime_id) ?? null
    : null;
  const owner = agent.owner_id
    ? members.find((m) => m.user_id === agent.owner_id) ?? null
    : null;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <DetailHeader
        agent={agent}
        runtime={runtime}
        presence={presence}
        backHref={paths.agents()}
        canAssign={canAssign.allowed}
        canArchive={canEdit.allowed}
        onAssign={() =>
          useModalStore
            .getState()
            .open("quick-create-issue", { agent_id: agent.id })
        }
        onArchive={() => setConfirmArchive(true)}
      />

      {!canEdit.allowed && (
        <div className="px-6 pt-3">
          <CapabilityBanner
            reason={canEdit.reason}
            resource="agent"
            ownerName={owner?.name}
          />
        </div>
      )}

      {isArchived && (
        <div className="flex shrink-0 items-center gap-2 border-b bg-muted/50 px-6 py-2 text-xs text-muted-foreground">
          <AlertCircle className="h-3.5 w-3.5 shrink-0" />
          <span className="flex-1">
            {t(($) => $.detail.archived_banner)}
          </span>
          {canEdit.allowed && (
            <Button
              variant="outline"
              size="sm"
              className="h-6 text-xs"
              onClick={() => handleRestore(agent.id)}
            >
              {t(($) => $.detail.restore)}
            </Button>
          )}
        </div>
      )}

      <div className="flex flex-1 min-h-0 flex-col overflow-hidden">
        <AgentOverviewPane
          agent={agent}
          runtime={runtime}
          owner={owner}
          runtimes={runtimes}
          members={members}
          onUpdate={handleUpdate}
          currentUserId={currentUser?.id ?? null}
          canEdit={canEdit.allowed}
          navIntent={tabNavIntent}
          onNavIntentHandled={() => setTabNavIntent(null)}
        />
      </div>

      {confirmArchive && (
        <Dialog
          open
          onOpenChange={(v) => {
            if (!v) setConfirmArchive(false);
          }}
        >
          <DialogContent className="max-w-sm" showCloseButton={false}>
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
                <AlertCircle className="h-5 w-5 text-destructive" />
              </div>
              <DialogHeader className="flex-1 gap-1">
                <DialogTitle className="text-sm font-semibold">
                  {t(($) => $.detail.archive_dialog_title)}
                </DialogTitle>
                <DialogDescription className="text-xs">
                  {t(($) => $.detail.archive_dialog_description, { name: agent.name })}
                </DialogDescription>
              </DialogHeader>
            </div>
            <DialogFooter>
              <Button
                variant="ghost"
                onClick={() => setConfirmArchive(false)}
              >
                {t(($) => $.detail.archive_dialog_cancel)}
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  setConfirmArchive(false);
                  handleArchive(agent.id);
                  navigation.push(paths.agents());
                }}
              >
                {t(($) => $.detail.archive_dialog_confirm)}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function DetailHeader({
  agent,
  runtime,
  presence,
  backHref,
  canAssign,
  canArchive,
  onAssign,
  onArchive,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  presence: AgentPresenceDetail | null;
  backHref: string;
  canAssign: boolean;
  canArchive: boolean;
  onAssign: () => void;
  onArchive: () => void;
}) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const isArchived = !!agent.archived_at;

  return (
    <header className="shrink-0 border-b bg-background px-4 pb-5 pt-3 sm:px-6">
      <div className="mx-auto max-w-[1440px]">
        <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <AppLink
            href={backHref}
            className="rounded-sm transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {t(($) => $.page.title)}
          </AppLink>
          <span aria-hidden="true">/</span>
          <span className="truncate text-foreground">{agent.name}</span>
        </div>

        <div className="mt-4 flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div className="flex min-w-0 items-start gap-4">
            <ActorAvatar
              actorType="agent"
              actorId={agent.id}
              size="2xl"
              profileLink={false}
              className="ring-1 ring-border"
            />
            <div className="min-w-0 pt-0.5">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                <h1 className="min-w-0 text-balance text-xl font-semibold tracking-tight sm:text-2xl">
                  {agent.name}
                </h1>
                <AgentPresenceIndicator detail={presence} />
              </div>
              <p className="mt-1 max-w-2xl text-pretty text-sm leading-6 text-muted-foreground">
                {agent.description || t(($) => $.inspector.no_description_placeholder)}
              </p>
              <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
                <span className="inline-flex min-w-0 items-center gap-1.5">
                  <Bot className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                  <span className="truncate">{agent.model || t(($) => $.pickers.model_default)}</span>
                </span>
                <span className="inline-flex min-w-0 items-center gap-1.5">
                  <Server className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                  <span className="truncate">
                    {runtime?.name ?? t(($) => $.pickers.runtime_none)}
                  </span>
                </span>
                <VisibilityBadge value={agent.visibility} />
                <span className="inline-flex items-center gap-1.5">
                  <Clock3 className="h-3.5 w-3.5" aria-hidden="true" />
                  {t(($) => $.detail.updated, { when: timeAgo(agent.updated_at) })}
                </span>
              </div>
            </div>
          </div>

          <div className="flex shrink-0 items-center gap-2 self-end lg:self-start">
            {!isArchived && canAssign && (
              <Button type="button" size="sm" onClick={onAssign}>
                <Plus className="h-4 w-4" aria-hidden="true" />
                {t(($) => $.detail.assign_work)}
              </Button>
            )}
            {!isArchived && canArchive ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon-sm" />}
              aria-label={t(($) => $.detail.more_actions_aria)}
            >
              <MoreHorizontal
                className="h-4 w-4 text-muted-foreground"
                aria-hidden="true"
              />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-auto">
              <DropdownMenuItem
                variant="destructive"
                onClick={onArchive}
              >
                <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
                {t(($) => $.detail.more_archive)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
            ) : null}
          </div>
        </div>
      </div>
    </header>
  );
}

function BackHeader({ paths, title }: { paths: string; title: string }) {
  return (
    <PageHeader className="justify-between px-5">
      <div className="flex items-center gap-2">
        <AppLink
          href={paths}
          className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          {title}
        </AppLink>
      </div>
    </PageHeader>
  );
}

function DetailLoadingSkeleton() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <div className="shrink-0 border-b px-6 pb-5 pt-3">
        <Skeleton className="h-4 w-48" />
        <div className="mt-4 flex items-start gap-4">
          <Skeleton className="h-14 w-14 rounded-full" />
          <div className="flex-1 space-y-3">
            <Skeleton className="h-7 w-64" />
            <Skeleton className="h-4 w-full max-w-xl" />
            <Skeleton className="h-4 w-full max-w-lg" />
          </div>
        </div>
      </div>
      <div className="flex flex-1 flex-col p-6">
        <Skeleton className="h-9 w-96" />
        <div className="mt-6 grid flex-1 gap-6 xl:grid-cols-[minmax(0,1fr)_320px]">
          <div className="space-y-5">
            <Skeleton className="h-48 w-full" />
            <Skeleton className="h-64 w-full" />
          </div>
          <Skeleton className="h-96 w-full" />
        </div>
      </div>
    </div>
  );
}
