"use client";

import { useMemo, useState, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  RotateCcw,
  GitBranch,
  FolderOpen,
  FileCode2,
  Clock,
  CheckCircle2,
  XCircle,
  Ban,
  Loader2,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@wallts/core/api";
import { issueKeys } from "@wallts/core/issues/queries";
import { useResetSession } from "@wallts/core/issues/mutations";
import type {
  AgentSession,
  AgentSessionRun,
  AgentSessionStatus,
} from "@wallts/core/types";
import { Skeleton } from "@wallts/ui/components/ui/skeleton";
import { Button } from "@wallts/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@wallts/ui/components/ui/dialog";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@wallts/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { useActorName } from "@wallts/core/workspace/hooks";
import { useTimeAgo } from "../../i18n";
import { useT } from "../../i18n";
import { cn } from "@wallts/ui/lib/utils";

interface SessionManagementPanelProps {
  issueId: string;
}

function StatusBadge({
  status,
  t,
}: {
  status: AgentSessionStatus;
  t: ReturnType<typeof useT<"issues">>["t"];
}) {
  const cfg: Record<
    AgentSessionStatus,
    { label: string; cls: string; icon: React.ReactNode }
  > = {
    active: {
      label: t(($) => $.sessions_active),
      cls: "bg-green-500/10 text-green-600 dark:text-green-400",
      icon: <CheckCircle2 className="h-3 w-3" />,
    },
    expired: {
      label: t(($) => $.sessions_expired),
      cls: "bg-muted text-muted-foreground",
      icon: <Clock className="h-3 w-3" />,
    },
    reset: {
      label: t(($) => $.sessions_reset),
      cls: "bg-yellow-500/10 text-yellow-600 dark:text-yellow-400",
      icon: <RotateCcw className="h-3 w-3" />,
    },
  };
  const c = cfg[status] ?? cfg.expired;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium",
        c.cls,
      )}
    >
      {c.icon}
      {c.label}
    </span>
  );
}

function RunStatusIcon({ status }: { status: AgentSessionRun["status"] }) {
  switch (status) {
    case "completed":
      return <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />;
    case "failed":
      return <XCircle className="h-3.5 w-3.5 text-red-500" />;
    case "cancelled":
      return <Ban className="h-3.5 w-3.5 text-muted-foreground" />;
    case "running":
      return <Loader2 className="h-3.5 w-3.5 text-blue-500 animate-spin" />;
    default:
      return <Clock className="h-3.5 w-3.5 text-muted-foreground" />;
  }
}

function SessionDetailInline({
  sessionId,
  issueId,
  onClose,
}: {
  sessionId: string;
  issueId: string;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const resetSession = useResetSession(issueId);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const { data: detail, isLoading } = useQuery({
    queryKey: issueKeys.sessionDetail(sessionId),
    queryFn: () => api.getSessionDetail(sessionId),
    staleTime: 30_000,
  });

  const handleReset = useCallback(() => {
    resetSession.mutate(sessionId, {
      onSuccess: () => {
        toast.success(t(($) => $.sessions_reset_success));
        setConfirmOpen(false);
        onClose();
      },
      onError: () => {
        toast.error(t(($) => $.sessions_reset_error));
      },
    });
  }, [sessionId, resetSession, t, onClose]);

  if (isLoading) {
    return (
      <div className="flex flex-col gap-2 px-3 py-2">
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    );
  }

  if (!detail) return null;

  return (
    <div className="flex flex-col gap-3 px-3 py-2 text-xs">
      {detail.summary && (
        <div className="flex flex-col gap-1">
          <span className="font-medium text-foreground">
            {t(($) => $.sessions_summary)}
          </span>
          <p className="text-muted-foreground leading-relaxed">
            {detail.summary}
          </p>
        </div>
      )}

      <div className="flex flex-col gap-1.5">
        <span className="font-medium text-foreground">
          {t(($) => $.sessions_working_state)}
        </span>
        <div className="flex flex-col gap-1 pl-1">
          {detail.state.branch && (
            <div className="flex items-center gap-1.5 text-muted-foreground">
              <GitBranch className="h-3 w-3 shrink-0" />
              <span>
                {t(($) => $.sessions_branch)}: {detail.state.branch}
              </span>
            </div>
          )}
          {detail.state.working_directory && (
            <div className="flex items-center gap-1.5 text-muted-foreground">
              <FolderOpen className="h-3 w-3 shrink-0" />
              <span className="truncate">
                {t(($) => $.sessions_workdir)}:{" "}
                {detail.working_directory}
              </span>
            </div>
          )}
          {detail.files_modified_count > 0 && (
            <div className="flex items-center gap-1.5 text-muted-foreground">
              <FileCode2 className="h-3 w-3 shrink-0" />
              <span>
                {t(($) => $.sessions_files_other, {
                  count: detail.files_modified_count,
                })}
              </span>
            </div>
          )}
        </div>
      </div>

      <div className="flex items-center gap-4 text-muted-foreground">
        <Tooltip>
          <TooltipTrigger
            render={
              <span>
                {t(($) => $.sessions_created)}: {timeAgo(detail.created_at)}
              </span>
            }
          />
          <TooltipContent>
            {new Date(detail.created_at).toLocaleString()}
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger
            render={
              <span>
                {t(($) => $.sessions_last_active)}:{" "}
                {timeAgo(detail.last_active)}
              </span>
            }
          />
          <TooltipContent>
            {new Date(detail.last_active).toLocaleString()}
          </TooltipContent>
        </Tooltip>
      </div>

      {detail.runs.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <span className="font-medium text-foreground">
            {t(($) => $.sessions_run_history)}
          </span>
          <div className="flex flex-col gap-1">
            {detail.runs.map((run) => (
              <div key={run.task_id} className="flex items-center gap-2 pl-1">
                <RunStatusIcon status={run.status} />
                <span className="shrink-0 font-mono text-[11px]">
                  {t(($) => $.sessions_run, { number: run.run_number })}
                </span>
                {run.summary && (
                  <span className="truncate text-muted-foreground">
                    {run.summary}
                  </span>
                )}
                <span className="ml-auto shrink-0 text-[11px] text-muted-foreground">
                  {timeAgo(run.started_at)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      {detail.status === "active" && (
        <>
          <Button
            variant="outline"
            size="sm"
            className="self-start mt-1"
            onClick={() => setConfirmOpen(true)}
          >
            <RotateCcw className="h-3.5 w-3.5 mr-1.5" />
            {t(($) => $.sessions_reset_button)}
          </Button>

          <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>
                  {t(($) => $.sessions_reset_confirm_title)}
                </DialogTitle>
                <DialogDescription>
                  {t(($) => $.sessions_reset_confirm_desc)}
                </DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setConfirmOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={handleReset}
                  disabled={resetSession.isPending}
                >
                  {resetSession.isPending && (
                    <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                  )}
                  {t(($) => $.sessions_reset_button)}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </>
      )}
    </div>
  );
}

function SessionRow({
  session,
  issueId,
  t,
}: {
  session: AgentSession;
  issueId: string;
  t: ReturnType<typeof useT<"issues">>["t"];
}) {
  const { getActorName } = useActorName();
  const [expanded, setExpanded] = useState(false);
  const agentName =
    session.agent_name || getActorName("agent", session.agent_id);

  return (
    <div className="flex flex-col">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 rounded-md px-2 py-1.5 text-xs hover:bg-accent/50 transition-colors text-left w-full"
      >
        <ChevronRight
          className={cn(
            "h-3 w-3 shrink-0 text-muted-foreground transition-transform",
            expanded && "rotate-90",
          )}
        />
        <ActorAvatar actorType="agent" actorId={session.agent_id} size={20} />
        <span className="flex-1 min-w-0 truncate font-medium">
          {agentName}
        </span>
        <StatusBadge status={session.status} t={t} />
      </button>
      {expanded && (
        <SessionDetailInline
          sessionId={session.id}
          issueId={issueId}
          onClose={() => setExpanded(false)}
        />
      )}
    </div>
  );
}

export function SessionManagementPanel({
  issueId,
}: SessionManagementPanelProps) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const [showPast, setShowPast] = useState(false);

  const { data: sessions = [], isLoading } = useQuery({
    queryKey: issueKeys.sessions(issueId),
    queryFn: () => api.listSessionsByIssue(issueId),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  });

  const { activeSessions, pastSessions } = useMemo(() => {
    const active: AgentSession[] = [];
    const past: AgentSession[] = [];
    for (const s of sessions) {
      if (s.status === "active") {
        active.push(s);
      } else {
        past.push(s);
      }
    }
    past.sort(
      (a, b) =>
        new Date(b.last_active).getTime() - new Date(a.last_active).getTime(),
    );
    return { activeSessions: active, pastSessions: past };
  }, [sessions]);

  const totalCount = sessions.length;
  const pastCount = pastSessions.length;

  if (!isLoading && totalCount === 0) return null;

  return (
    <div>
      <button
        type="button"
        className={cn(
          "flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70",
          !open && "text-muted-foreground hover:text-foreground",
        )}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.section_sessions)}
        {totalCount > 0 && (
          <span className="tabular-nums">{" · "} {totalCount}</span>
        )}
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
      </button>
      {open && (
        <div className="flex flex-col gap-1 pl-2">
          {isLoading && (
            <div className="flex flex-col gap-2 px-2 py-1">
              <Skeleton className="h-5 w-full" />
              <Skeleton className="h-5 w-3/4" />
            </div>
          )}

          {!isLoading && totalCount === 0 && (
            <p className="px-2 py-1 text-xs text-muted-foreground">
              {t(($) => $.sessions_empty)}
            </p>
          )}

          {activeSessions.map((session) => (
            <SessionRow
              key={session.id}
              session={session}
              issueId={issueId}
              t={t}
            />
          ))}

          {pastCount > 0 && (
            <>
              {!showPast && (
                <button
                  type="button"
                  onClick={() => setShowPast(true)}
                  className="flex items-center gap-1 px-2 py-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
                >
                  <ChevronRight className="h-3 w-3 shrink-0" />
                  <span>
                    {t(($) => $.sessions_expired)} ({pastCount})
                  </span>
                </button>
              )}
              {showPast &&
                pastSessions.map((session) => (
                  <SessionRow
                    key={session.id}
                    session={session}
                    issueId={issueId}
                    t={t}
                  />
                ))}
            </>
          )}
        </div>
      )}
    </div>
  );
}
