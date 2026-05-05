"use client";

import { useState } from "react";
import {
  AlertCircle,
  ArrowLeft,
  MoreHorizontal,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Agent, UpdateAgentRequest } from "@multica/core/types";
import {
  type AgentPresenceDetail,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import { useT } from "@multica/i18n/react";
import { Button } from "@multica/ui/components/ui/button";
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
import { availabilityConfig, availabilityLabel } from "../presence";
import { AgentDetailInspector } from "./agent-detail-inspector";
import { AgentOverviewPane } from "./agent-overview-pane";

interface AgentDetailPageProps {
  agentId: string;
}

export function AgentDetailPage({ agentId }: AgentDetailPageProps) {
  const t = useT("agents");
  const c = useT("common");
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

  const [confirmArchive, setConfirmArchive] = useState(false);

  const handleUpdate = async (id: string, data: Record<string, unknown>) => {
    try {
      await api.updateAgent(id, data as UpdateAgentRequest);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t("toast_updated"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("toast_failed_update"));
      throw e;
    }
  };

  const handleArchive = async (id: string) => {
    try {
      await api.archiveAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t("toast_archived"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("toast_failed_archive"));
    }
  };

  const handleRestore = async (id: string) => {
    try {
      await api.restoreAgent(id);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t("toast_restored"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("toast_failed_restore"));
    }
  };

  // --- Loading ---
  if (agentsLoading && !agent) {
    return <DetailLoadingSkeleton />;
  }

  // --- Not found / error ---
  if (!agent) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <BackHeader paths={paths.agents()} title={t("page_title")} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <div>
            <p className="text-sm font-medium">{t("detail_not_found")}</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {agentsError instanceof Error
                ? agentsError.message
                : t("detail_not_found_desc")}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => refetchAgents()}
            >
              {t("error_retry")}
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={() => navigation.push(paths.agents())}
            >
              {t("detail_back")}
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
        presence={presence}
        backHref={paths.agents()}
        onArchive={() => setConfirmArchive(true)}
        t={t}
      />

      {isArchived && (
        <div className="flex shrink-0 items-center gap-2 border-b bg-muted/50 px-6 py-2 text-xs text-muted-foreground">
          <AlertCircle className="h-3.5 w-3.5 shrink-0" />
          <span className="flex-1">
            {t("detail_archived_banner")}
          </span>
          <Button
            variant="outline"
            size="sm"
            className="h-6 text-xs"
            onClick={() => handleRestore(agent.id)}
          >
            {t("detail_restore")}
          </Button>
        </div>
      )}

      <div className="grid flex-1 min-h-0 grid-cols-[320px_minmax(0,1fr)] gap-4 p-6">
        <AgentDetailInspector
          agent={agent}
          runtime={runtime}
          owner={owner}
          presence={presence}
          runtimes={runtimes}
          members={members}
          currentUserId={currentUser?.id ?? null}
          onUpdate={handleUpdate}
        />

        <AgentOverviewPane
          agent={agent}
          runtimes={runtimes}
          onUpdate={handleUpdate}
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
                  {t("detail_archive_title")}
                </DialogTitle>
                <DialogDescription className="text-xs">
                  {t("detail_archive_desc", { name: agent.name })}
                </DialogDescription>
              </DialogHeader>
            </div>
            <DialogFooter>
              <Button
                variant="ghost"
                onClick={() => setConfirmArchive(false)}
              >
                {c("cancel")}
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  setConfirmArchive(false);
                  handleArchive(agent.id);
                  navigation.push(paths.agents());
                }}
              >
                {t("detail_archive")}
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
  presence,
  backHref,
  onArchive,
  t,
}: {
  agent: Agent;
  presence: AgentPresenceDetail | null;
  backHref: string;
  onArchive: () => void;
  t: ReturnType<typeof useT>;
}) {
  const isArchived = !!agent.archived_at;
  const av = presence ? availabilityConfig[presence.availability] : null;
  // Last-task state is intentionally not surfaced in the header — the
  // Recent work section on this page already shows the same information
  // (and richer: titles, timestamps, error messages). Showing "Completed"
  // up here was redundant chrome.

  return (
    <PageHeader className="justify-between gap-3 px-5">
      <div className="flex min-w-0 items-center gap-2">
        <AppLink
          href={backHref}
          className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          {t("page_title")}
        </AppLink>
        <span className="text-muted-foreground/40">/</span>
        <h1 className="truncate text-sm font-medium">{agent.name}</h1>
        {!isArchived && av && presence && (
          <span
            className={`inline-flex items-center gap-1.5 rounded-md border px-1.5 py-0.5 text-xs ${av.textClass}`}
          >
            <span className={`h-1.5 w-1.5 rounded-full ${av.dotClass}`} />
            {availabilityLabel(t, presence.availability)}
          </span>
        )}
      </div>

      {!isArchived && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={<Button variant="ghost" size="icon-sm" />}
          >
            <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuItem
              className="text-destructive"
              onClick={onArchive}
            >
              <Trash2 className="h-3.5 w-3.5" />
              {t("detail_archive_agent")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </PageHeader>
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
      <PageHeader className="px-5">
        <Skeleton className="h-5 w-48" />
      </PageHeader>
      <div className="grid flex-1 min-h-0 grid-cols-[320px_minmax(0,1fr)] gap-4 p-6">
        <div className="flex flex-col gap-4 rounded-lg border p-5">
          <Skeleton className="h-14 w-14 rounded-lg" />
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-3 w-full" />
          <div className="space-y-2">
            <Skeleton className="h-3 w-3/4" />
            <Skeleton className="h-3 w-2/3" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        </div>
        <div className="flex flex-col gap-4 rounded-lg border p-6">
          <Skeleton className="h-6 w-64" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
          <Skeleton className="h-4 w-4/6" />
        </div>
      </div>
    </div>
  );
}
