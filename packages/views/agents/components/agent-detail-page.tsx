"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRegisterCommandBarContext } from "../../search/register-context";
import {
  AlertCircle,
  Archive,
  Copy,
  Hash,
  Link2,
  Lock,
  MessageSquare,
  MoreHorizontal,
  Plus,
  RotateCcw,
  Server,
  Settings2,
  Square,
  Trash2,
} from "@multica/ui/icons";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Agent, UpdateAgentRequest } from "@multica/core/types";
import {
  type AgentPresenceDetail,
  isAgentRuntimeBound,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { api, ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentDetailOptions,
  agentListOptions,
  cacheAgentResponse,
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import { useAgentPermissions } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { CapabilityBanner } from "@multica/ui/components/common/capability-banner";
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
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { CollectionPageState } from "../../layout/collection-page";
import {
  OBJECT_PAGE_BODY,
  ObjectIdentity,
  ObjectPage,
} from "../../layout/object-page";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { availabilityConfig } from "../presence";
import {
  AgentAdvancedSettings,
  isAdvancedSection,
  type AdvancedSection,
} from "./agent-advanced-settings";
import { AgentInstructionsSection } from "./agent-instructions-section";
import { AgentPropertiesPanel } from "./agent-properties-panel";
import { describeCancelImpact } from "./agent-row-actions";
import { AgentSkillsSection } from "./agent-skills-section";
import { AgentWorkDialog } from "./agent-work-dialog";
import { AgentWorkSections } from "./tabs/activity-tab";

const AGENT_DETAIL_LAYOUT_ID = "multica_agent_detail_layout";

/**
 * The page's `?view=` deep link. Every view the old tabbed page had stays
 * linkable: settings sections open Advanced settings there, `work` opens the
 * work list, and the rest are the page itself. `?tab=` predates `?view=`.
 */
function viewFromParams(params: URLSearchParams): string | null {
  const view = params.get("view");
  if (view !== null) return view;
  const tab = params.get("tab");
  if (tab === "settings") return "general";
  if (tab === "capabilities") return "skills";
  return tab;
}

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

  const listAgent = agents.find((a) => a.id === agentId) ?? null;

  // The list remains the zero-request common path. When it has settled
  // without the requested agent, use the canonical detail query to resolve
  // direct links and distinguish 403/404 from transient failures. Creation
  // hydrates this same key, so a newly-created agent renders immediately.
  const detailQuery = useQuery({
    ...agentDetailOptions(wsId, agentId),
    enabled: !agentsLoading && !listAgent && !!agentId,
  });
  const detailError = detailQuery.error;
  const isForbidden =
    detailError instanceof ApiError && detailError.status === 403;
  const isNotFound =
    detailError instanceof ApiError && detailError.status === 404;
  // TanStack intentionally keeps successful data when a refetch fails. Do not
  // let that stale snapshot mask a later 403/404 after access is revoked or the
  // agent is deleted, but preserve it through transient network failures. A
  // still-visible list response remains authoritative in either case.
  const agent =
    listAgent ?? (isForbidden || isNotFound ? null : detailQuery.data) ?? null;
  const presence: AgentPresenceDetail | null =
    agent ? presenceMap.get(agent.id) ?? null : null;
  useRegisterCommandBarContext(
    useMemo(() => (agent ? { kind: "agent", agent } : null), [agent]),
  );

  // Backend gates archive and restore identically to edit, so a single
  // `canEdit` covers them all.
  const {
    canAssign,
    canEdit,
    isLoading: permissionsLoading,
  } = useAgentPermissions(agent, wsId);

  const [confirmArchive, setConfirmArchive] = useState(false);
  const [confirmCancelRuns, setConfirmCancelRuns] = useState(false);

  const urlView = viewFromParams(navigation.searchParams);
  const [advancedSection, setAdvancedSection] = useState<AdvancedSection | null>(
    () => (isAdvancedSection(urlView) ? urlView : null),
  );
  const [workOpen, setWorkOpen] = useState(() => urlView === "work");
  const lastUrlViewRef = useRef(urlView);

  // Back/forward or a link landing on this mounted page.
  useEffect(() => {
    if (urlView === lastUrlViewRef.current) return;
    lastUrlViewRef.current = urlView;
    setAdvancedSection(isAdvancedSection(urlView) ? urlView : null);
    setWorkOpen(urlView === "work");
  }, [urlView]);

  const writeView = useCallback(
    (view: string | null) => {
      lastUrlViewRef.current = view;
      const params = new URLSearchParams(navigation.searchParams);
      params.delete("tab");
      if (view === null) params.delete("view");
      else params.set("view", view);
      const query = params.toString();
      navigation.replace(`${navigation.pathname}${query ? `?${query}` : ""}`);
    },
    [navigation],
  );
  const openAdvanced = useCallback(
    (section: AdvancedSection | null) => {
      setAdvancedSection(section);
      writeView(section);
    },
    [writeView],
  );
  const openWork = useCallback(
    (open: boolean) => {
      setWorkOpen(open);
      writeView(open ? "work" : null);
    },
    [writeView],
  );

  const handleUpdate = useCallback(
    async (id: string, data: Record<string, unknown>) => {
      // Optimistic update: patch the matching agent in the cached list
      // BEFORE the network round-trip so the pickers flip to the new value
      // immediately on click. Without this, every picker waits 0.5-2s for
      // the API response + invalidate + refetch before the trigger updates —
      // readable as obvious lag in the UI.
      //
      // On error we rollback only the fields THIS call wrote, leaving any
      // other concurrently-mutated fields untouched, then invalidate so the
      // cache converges with the server. A whole-list snapshot rollback
      // would clobber a concurrent successful mutation if the failing call
      // resolves last (e.g. flipping visibility then runtime simultaneously
      // and only the visibility PATCH fails).
      const optimisticData =
        typeof data.runtime_id === "string"
          ? { ...data, runtime_bound: data.runtime_id.trim().length > 0 }
          : data;
      const queryKey = workspaceKeys.agents(wsId);
      const detailQueryKey = workspaceKeys.agent(wsId, id);
      const prevAgents = qc.getQueryData<Agent[]>(queryKey);
      const prevListAgent = prevAgents?.find((a) => a.id === id);
      const prevDetailAgent = qc.getQueryData<Agent>(detailQueryKey);
      const previousFields = (previousAgent: Agent | undefined) => {
        const fields: Record<string, unknown> = {};
        if (!previousAgent) return fields;
        for (const key of Object.keys(optimisticData)) {
          fields[key] = (
            previousAgent as unknown as Record<string, unknown>
          )[key];
        }
        return fields;
      };
      const prevListFields = previousFields(prevListAgent);
      const prevDetailFields = previousFields(prevDetailAgent);
      qc.setQueryData<Agent[]>(queryKey, (old) =>
        old?.map((a) =>
          a.id === id ? ({ ...a, ...optimisticData } as Agent) : a,
        ),
      );
      qc.setQueryData<Agent>(detailQueryKey, (old) =>
        old ? ({ ...old, ...optimisticData } as Agent) : old,
      );
      try {
        const updatedAgent = await api.updateAgent(
          id,
          data as UpdateAgentRequest,
        );
        cacheAgentResponse(qc, wsId, updatedAgent, { insertIntoList: false });
        void qc.invalidateQueries({ queryKey });
        toast.success(t(($) => $.detail.agent_updated_toast));
      } catch (e) {
        if (prevListAgent) {
          qc.setQueryData<Agent[]>(queryKey, (old) =>
            old?.map((a) =>
              a.id === id ? ({ ...a, ...prevListFields } as Agent) : a,
            ),
          );
        }
        if (prevDetailAgent) {
          qc.setQueryData<Agent>(detailQueryKey, (old) =>
            old ? ({ ...old, ...prevDetailFields } as Agent) : old,
          );
        }
        void qc.invalidateQueries({ queryKey });
        toast.error(e instanceof Error ? e.message : t(($) => $.detail.update_failed_toast));
        throw e;
      }
    },
    [qc, t, wsId],
  );

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

  const handleCancelRuns = async (id: string) => {
    try {
      const { cancelled } = await api.cancelAgentTasks(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(
        cancelled === 0
          ? t(($) => $.row_actions.no_tasks_to_cancel_toast)
          : t(($) => $.row_actions.cancelled_tasks_toast, { count: cancelled }),
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.row_actions.cancel_failed_toast));
    }
  };

  const listSegment = { href: paths.agents(), label: t(($) => $.page.title) };

  // --- Loading ---
  if (!agent && (agentsLoading || detailQuery.isPending)) {
    return (
      <ObjectPage
        layoutId={AGENT_DETAIL_LAYOUT_ID}
        segments={[listSegment]}
        leaf={<Skeleton className="h-4 w-40" />}
      >
        <DetailLoadingSkeleton />
      </ObjectPage>
    );
  }

  // --- No permission (private agent the caller is not in allowed_principals for) ---
  if (!agent && isForbidden) {
    return (
      <ObjectPage
        layoutId={AGENT_DETAIL_LAYOUT_ID}
        segments={[listSegment]}
        leaf={
          <span className="truncate text-muted-foreground">
            {t(($) => $.detail.no_access_title)}
          </span>
        }
      >
        <CollectionPageState
          icon={Lock}
          title={t(($) => $.detail.no_access_title)}
          description={t(($) => $.detail.no_access_hint)}
          actions={
            <Button
              size="sm"
              variant="outline"
              render={<AppLink href={paths.agents()} />}
              nativeButton={false}
            >
              {t(($) => $.detail.back_to_agents_full)}
            </Button>
          }
        />
      </ObjectPage>
    );
  }

  // --- Not found / error ---
  if (!agent) {
    const loadError = detailError ?? agentsError;
    const title = isNotFound
      ? t(($) => $.detail.not_found_title)
      : t(($) => $.detail.load_failed_title);
    return (
      <ObjectPage
        layoutId={AGENT_DETAIL_LAYOUT_ID}
        segments={[listSegment]}
        leaf={<span className="truncate text-muted-foreground">{title}</span>}
      >
        <CollectionPageState
          role="alert"
          tone="destructive"
          icon={AlertCircle}
          title={title}
          description={
            isNotFound
              ? t(($) => $.detail.not_found_default)
              : loadError instanceof Error
                ? loadError.message
                : t(($) => $.detail.load_failed_default)
          }
          actions={
            <>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => {
                  void Promise.all([refetchAgents(), detailQuery.refetch()]);
                }}
              >
                {t(($) => $.detail.try_again)}
              </Button>
              <Button
                size="sm"
                variant="outline"
                render={<AppLink href={paths.agents()} />}
                nativeButton={false}
              >
                {t(($) => $.detail.back_to_agents_full)}
              </Button>
            </>
          }
        />
      </ObjectPage>
    );
  }

  const isArchived = !!agent.archived_at;
  const runtimeBound = isAgentRuntimeBound(agent);
  const runtime = runtimeBound
    ? runtimes.find((r) => r.id === agent.runtime_id) ?? null
    : null;
  const owner = agent.owner_id
    ? members.find((m) => m.user_id === agent.owner_id) ?? null
    : null;
  const currentUserId = currentUser?.id ?? null;
  const runningCount = presence?.runningCount ?? 0;
  const queuedCount = presence?.queuedCount ?? 0;
  const hasActiveWork = runningCount + queuedCount > 0;
  // Multica's built-in agents cannot be archived — the server refuses it.
  const canArchive = canEdit.allowed && !isArchived && !agent.system_key;

  // Chat shares the invocation gate with assignment (MUL-3963): starting a
  // chat triggers agent runs. The button stays visible either way — a denied
  // click explains itself instead of the affordance silently missing. While
  // membership is still resolving the decision is undetermined, so the button
  // is disabled rather than toasting a false "no access" at a real member.
  //
  // The control is a real link, so a failed gate has to cancel the navigation
  // AppLink would otherwise perform — preventDefault is that cancel.
  const handleDm = (e: React.MouseEvent<HTMLAnchorElement>) => {
    if (permissionsLoading) {
      e.preventDefault();
      return;
    }
    if (!canAssign.allowed) {
      e.preventDefault();
      toast.error(t(($) => $.detail.dm_no_permission_toast));
      return;
    }
    if (!runtimeBound) {
      e.preventDefault();
      toast.error(t(($) => $.detail.runtime_required_toast));
    }
  };
  const handleAssign = () => {
    if (!runtimeBound) {
      toast.error(t(($) => $.detail.runtime_required_toast));
      return;
    }
    useModalStore
      .getState()
      .open("quick-create-issue", { agent_id: agent.id });
  };
  const handleCopy = (text: string, toastText: string) => {
    void copyText(text).then((ok) => {
      if (ok) toast.success(toastText);
    });
  };

  const actions = (
    <>
      {!isArchived && (
        <IconButton
          label={t(($) => $.detail.dm)}
          tooltipSide="bottom"
          className="text-muted-foreground data-disabled:pointer-events-none data-disabled:opacity-50"
          disabled={permissionsLoading}
          render={
            <AppLink href={`${paths.chat()}?agent=${agent.id}`} onClick={handleDm} />
          }
          nativeButton={false}
        >
          <MessageSquare />
        </IconButton>
      )}
      {!isArchived && canAssign.allowed && (
        <Button type="button" size="sm" onClick={handleAssign}>
          <Plus />
          {t(($) => $.detail.assign_work)}
        </Button>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <IconButton
              label={t(($) => $.detail.more_actions_aria)}
              tooltipSide="bottom"
              className="text-muted-foreground"
            >
              <MoreHorizontal />
            </IconButton>
          }
        />
        <DropdownMenuContent align="end" className="w-auto min-w-48">
          <DropdownMenuItem onClick={() => openAdvanced("general")}>
            <Settings2 />
            {t(($) => $.detail.advanced_settings)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() =>
              handleCopy(
                navigation.getShareableUrl(paths.agentDetail(agent.id)),
                t(($) => $.detail.link_copied_toast),
              )
            }
          >
            <Link2 />
            {t(($) => $.detail.copy_link)}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => handleCopy(agent.id, t(($) => $.detail.id_copied_toast))}
          >
            <Hash />
            {t(($) => $.detail.copy_id)}
          </DropdownMenuItem>
          {!isArchived && (
            <DropdownMenuItem
              render={
                <AppLink href={`${paths.newAgentManual()}?duplicate=${agent.id}`} />
              }
            >
              <Copy />
              {t(($) => $.row_actions.duplicate)}
            </DropdownMenuItem>
          )}
          {canEdit.allowed && !isArchived && hasActiveWork && (
            <DropdownMenuItem onClick={() => setConfirmCancelRuns(true)}>
              <Square />
              {t(($) => $.row_actions.cancel_all_tasks)}
            </DropdownMenuItem>
          )}
          {canEdit.allowed && isArchived && (
            <DropdownMenuItem onClick={() => handleRestore(agent.id)}>
              <RotateCcw />
              {t(($) => $.detail.restore)}
            </DropdownMenuItem>
          )}
          {canArchive && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setConfirmArchive(true)}
              >
                <Trash2 />
                {t(($) => $.detail.more_archive)}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  );

  const notices = [
    !canEdit.allowed && (
      <CapabilityBanner
        key="capability"
        reason={canEdit.reason}
        resource="agent"
        ownerName={owner?.name}
      />
    ),
    isArchived && (
      <Notice
        key="archived"
        icon={<Archive className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />}
        action={
          canEdit.allowed ? (
            <Button
              variant="outline"
              size="xs"
              onClick={() => handleRestore(agent.id)}
            >
              {t(($) => $.detail.restore)}
            </Button>
          ) : null
        }
      >
        {t(($) => $.detail.archived_banner)}
      </Notice>
    ),
    !isArchived && !runtimeBound && (
      <Notice
        key="runtime"
        tone="warning"
        icon={<Server className="size-3.5 shrink-0 text-warning" aria-hidden="true" />}
        action={
          canEdit.allowed ? (
            <Button
              variant="outline"
              size="xs"
              onClick={() => openAdvanced("general")}
            >
              {t(($) => $.detail.bind_runtime)}
            </Button>
          ) : null
        }
      >
        {t(($) => $.detail.runtime_required_banner)}
      </Notice>
    ),
  ].filter(Boolean);

  return (
    <>
      <ObjectPage
        layoutId={AGENT_DETAIL_LAYOUT_ID}
        segments={[listSegment]}
        leaf={
          <span className="flex min-w-0 items-center gap-1.5 text-foreground">
            <ActorAvatar
              actorType="agent"
              actorId={agent.id}
              size="xs"
              profileLink={false}
            />
            <span className="truncate">{agent.name}</span>
          </span>
        }
        actions={actions}
        panel={
          <AgentPropertiesPanel
            agent={agent}
            runtime={runtime}
            runtimes={runtimes}
            presence={presence}
            owner={owner}
            members={members}
            currentUserId={currentUserId}
            canEdit={canEdit.allowed}
            onUpdate={handleUpdate}
            onEditAccess={() => openAdvanced("access")}
          />
        }
      >
        <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
          <div className="space-y-4">
            <ObjectIdentity
              media={<AgentMark agent={agent} presence={presence} />}
              title={agent.name}
              description={agent.description || undefined}
              trailing={
                isArchived ? (
                  <span className="inline-flex shrink-0 items-center rounded-md bg-muted px-1.5 py-0.5 text-caption text-muted-foreground">
                    {t(($) => $.row.archived)}
                  </span>
                ) : null
              }
            />
            {notices.length > 0 && <div className="space-y-2">{notices}</div>}
          </div>

          <AgentWorkSections agent={agent} onViewAll={() => openWork(true)} />

          <AgentInstructionsSection
            agent={agent}
            canEdit={canEdit.allowed}
            onUpdate={handleUpdate}
          />

          {/* Qoder agents take their skills from Qoder Cloud Agents. */}
          {runtime?.provider === "qoder_cloud" ? null : (
            <AgentSkillsSection
              agent={agent}
              canEdit={canEdit.allowed}
              onManage={() => openAdvanced("skills")}
            />
          )}
        </div>
      </ObjectPage>

      <AgentAdvancedSettings
        agent={agent}
        runtime={runtime}
        runtimes={runtimes}
        members={members}
        onUpdate={handleUpdate}
        currentUserId={currentUserId}
        canEdit={canEdit.allowed}
        section={advancedSection}
        onSectionChange={openAdvanced}
      />

      <AgentWorkDialog agent={agent} open={workOpen} onOpenChange={openWork} />

      {confirmCancelRuns && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v) setConfirmCancelRuns(false);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.row_actions.cancel_dialog_title, { name: agent.name })}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {describeCancelImpact(runningCount, queuedCount, t)}
                {runningCount > 0 && t(($) => $.row_actions.cancel_dialog_running_note)}
                {t(($) => $.row_actions.cancel_dialog_irreversible)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.row_actions.cancel_dialog_keep)}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmCancelRuns(false);
                  void handleCancelRuns(agent.id);
                }}
              >
                {t(($) => $.row_actions.cancel_dialog_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}

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
                <DialogTitle className="text-body font-semibold">
                  {t(($) => $.detail.archive_dialog_title)}
                </DialogTitle>
                <DialogDescription className="text-caption">
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
    </>
  );
}

/** The 48px avatar with the agent's availability dot on its edge. */
function AgentMark({
  agent,
  presence,
}: {
  agent: Agent;
  presence: AgentPresenceDetail | null;
}) {
  const { t } = useT("agents");
  return (
    <span className="relative inline-flex [--avatar-slot-size:3rem]">
      <ActorAvatar
        actorType="agent"
        actorId={agent.id}
        size="2xl"
        profileLink={false}
        className="ring-1 ring-border"
      />
      {presence ? (
        <span
          role="img"
          aria-label={t(($) => $.presence_status_aria, {
            status: t(($) => $.availability[presence.availability]),
          })}
          className={cn(
            "absolute -right-0.5 -bottom-0.5 size-3 rounded-full ring-2 ring-background",
            availabilityConfig[presence.availability].dotClass,
          )}
        />
      ) : null}
    </span>
  );
}

function Notice({
  tone = "neutral",
  icon,
  action,
  children,
}: {
  tone?: "neutral" | "warning";
  icon: React.ReactNode;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div
      role="status"
      className={cn(
        "flex items-center gap-2 rounded-lg border px-3 py-2 text-caption",
        tone === "warning"
          ? "border-warning/30 bg-warning/10 text-foreground"
          : "bg-muted/40 text-muted-foreground",
      )}
    >
      {icon}
      <span className="min-w-0 flex-1">{children}</span>
      {action}
    </div>
  );
}

function DetailLoadingSkeleton() {
  return (
    <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
      <div className="flex items-center gap-3">
        <Skeleton className="size-12 rounded-full" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-4 w-3/4" />
        </div>
      </div>
      <div className="space-y-2">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-14 w-full rounded-lg" />
      </div>
      <div className="space-y-2">
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-32 w-full rounded-lg" />
      </div>
    </div>
  );
}
