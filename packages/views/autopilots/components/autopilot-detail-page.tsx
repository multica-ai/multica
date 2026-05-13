"use client";

import { useState } from "react";
import { Zap, Play, Clock, Plus, Trash2, CheckCircle2, XCircle, Loader2, Pencil, ChevronDown, ChevronRight, ExternalLink } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { autopilotDetailOptions, autopilotRunsOptions } from "@multica/core/autopilots/queries";
import {
  useUpdateAutopilot,
  useDeleteAutopilot,
  useTriggerAutopilot,
  useCreateAutopilotTrigger,
  useDeleteAutopilotTrigger,
} from "@multica/core/autopilots/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { useNavigation, AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { ActorAvatar } from "../../common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
  TriggerConfigSection,
  getDefaultTriggerConfig,
  toCronExpression,
} from "./trigger-config";
import type { TriggerConfig } from "./trigger-config";
import type { AutopilotExecutionMode, AutopilotRun, AutopilotTrigger } from "@multica/core/types";
import type { AgentTask } from "@multica/core/types/agent";
import { ReadonlyContent } from "../../editor";
import { TranscriptButton } from "../../common/task-transcript";
import { AutopilotDialog } from "./autopilot-dialog";
import { useT } from "../../i18n";

function formatDate(date: string): string {
  return new Date(date).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatDuration(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  if (minutes < 60) return `${minutes}m ${secs}s`;
  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  return `${hours}h ${mins}m`;
}

interface RunResult {
  output?: string;
  pr_url?: string;
  session_id?: string;
}

function parseRunResult(result: unknown): RunResult | null {
  if (!result || typeof result !== "object") return null;
  return result as RunResult;
}

type RunStatus = "issue_created" | "running" | "completed" | "failed";

const RUN_VISUAL: Record<RunStatus, { color: string; icon: typeof CheckCircle2; spin?: boolean }> = {
  issue_created: { color: "text-blue-500", icon: Clock },
  running: { color: "text-blue-500", icon: Loader2, spin: true },
  completed: { color: "text-emerald-500", icon: CheckCircle2 },
  failed: { color: "text-destructive", icon: XCircle },
};

function RunRow({
  run,
  agentId,
  agentName,
  isExpanded,
  onToggle,
}: {
  run: AutopilotRun;
  agentId: string;
  agentName: string;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const { t } = useT("autopilots");
  const wsPaths = useWorkspacePaths();
  const status = (RUN_VISUAL[run.status as RunStatus] ? (run.status as RunStatus) : "issue_created");
  const visual = RUN_VISUAL[status];
  const StatusIcon = visual.icon;

  const result = parseRunResult(run.result);
  const outputSummary = result?.output ? result.output.slice(0, 150) : null;

  const durationMs = run.completed_at && run.triggered_at
    ? new Date(run.completed_at).getTime() - new Date(run.triggered_at).getTime()
    : null;

  // For runs with a task_id (run_only mode), build a minimal AgentTask so
  // TranscriptButton can lazy-load the execution transcript.
  const syntheticTask: AgentTask | null = run.task_id
    ? {
        id: run.task_id,
        agent_id: agentId,
        runtime_id: "",
        issue_id: "",
        status:
          run.status === "running" ? "running" :
          run.status === "completed" ? "completed" :
          run.status === "failed" ? "failed" :
          "queued",
        priority: 0,
        dispatched_at: null,
        started_at: run.triggered_at || null,
        completed_at: run.completed_at || null,
        result: null,
        error: run.failure_reason || null,
        created_at: run.created_at,
      }
    : null;

  const hasExpandableContent = !!(result?.output || result?.pr_url || result?.session_id);
  const canExpand = hasExpandableContent && !run.issue_id;

  const rowClass = "flex items-center gap-3 px-4 py-2.5 text-sm hover:bg-accent/30 transition-colors";

  const headerContent = (
    <>
      {canExpand ? (
        isExpanded
          ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      ) : (
        <span className="w-3.5 shrink-0" />
      )}
      <StatusIcon className={cn("h-4 w-4 shrink-0", visual.color, visual.spin && "animate-spin")} />
      <span className={cn("w-24 shrink-0 text-xs font-medium", visual.color)}>
        {t(($) => $.run_status[status])}
      </span>
      <span className="w-16 shrink-0 text-xs text-muted-foreground capitalize">{run.source}</span>
      <span className="flex-1 min-w-0 text-xs text-muted-foreground truncate">
        {run.issue_id ? (
          t(($) => $.run.issue_linked)
        ) : outputSummary ? (
          outputSummary + (result!.output!.length > 150 ? "…" : "")
        ) : run.failure_reason ? (
          <span className="text-destructive">{run.failure_reason}</span>
        ) : null}
      </span>
      <span className="w-32 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
        {formatDate(run.triggered_at || run.created_at)}
      </span>
      {durationMs != null && durationMs >= 0 && (
        <span className="w-20 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
          {formatDuration(durationMs)}
        </span>
      )}
      {syntheticTask && !run.issue_id && (
        <TranscriptButton
          task={syntheticTask}
          agentName={agentName}
          isLive={run.status === "running"}
          title={t(($) => $.run.view_log)}
        />
      )}
    </>
  );

  const expandedPanel = isExpanded && (
    <div className="px-4 pb-3 pt-0 border-t border-border/50 bg-accent/10">
      <div className="space-y-2 pt-2">
        {run.completed_at && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span className="font-medium">{t(($) => $.run.completed_at)}:</span>
            <span>{formatDate(run.completed_at)}</span>
          </div>
        )}
        {result?.output ? (
          <div className="space-y-1">
            <span className="text-xs font-medium text-muted-foreground">{t(($) => $.run.output_summary)}:</span>
            <div className="rounded-md border bg-background p-3 text-sm max-h-80 overflow-y-auto">
              <ReadonlyContent content={result.output} />
            </div>
          </div>
        ) : (
          <div className="text-xs text-muted-foreground italic">{t(($) => $.run.no_output)}</div>
        )}
        {result?.pr_url && (
          <div className="flex items-center gap-2 text-xs">
            <span className="font-medium text-muted-foreground">{t(($) => $.run.pr_link)}:</span>
            <a href={result.pr_url} target="_blank" rel="noopener noreferrer" className="text-blue-500 hover:underline inline-flex items-center gap-1">
              {result.pr_url}
              <ExternalLink className="h-3 w-3" />
            </a>
          </div>
        )}
        {result?.session_id && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span className="font-medium">{t(($) => $.run.session_id)}:</span>
            <code className="bg-muted px-1.5 py-0.5 rounded text-[11px]">{result.session_id}</code>
          </div>
        )}
      </div>
    </div>
  );

  if (run.issue_id) {
    return (
      <AppLink href={wsPaths.issueDetail(run.issue_id)} className={cn(rowClass, "cursor-pointer")}>
        {headerContent}
      </AppLink>
    );
  }

  return (
    <div>
      <div
        className={cn(rowClass, canExpand && "cursor-pointer")}
        onClick={canExpand ? onToggle : undefined}
      >
        {headerContent}
      </div>
      {expandedPanel}
    </div>
  );
}

function TriggerRow({ trigger, autopilotId }: { trigger: AutopilotTrigger; autopilotId: string }) {
  const { t } = useT("autopilots");
  const deleteTrigger = useDeleteAutopilotTrigger();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteTrigger.mutateAsync({ autopilotId, triggerId: trigger.id });
      toast.success(t(($) => $.trigger_row.toast_deleted));
      setConfirmOpen(false);
    } catch {
      toast.error(t(($) => $.trigger_row.toast_delete_failed));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="flex items-center gap-3 rounded-md border px-3 py-2">
      <Clock className="h-4 w-4 shrink-0 text-muted-foreground" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium capitalize">{trigger.kind}</span>
          {trigger.label && (
            <span className="text-xs text-muted-foreground">({trigger.label})</span>
          )}
          {!trigger.enabled && (
            <span className="text-xs bg-muted px-1.5 py-0.5 rounded">
              {t(($) => $.trigger_row.disabled_badge)}
            </span>
          )}
        </div>
        {trigger.cron_expression && (
          <div className="text-xs text-muted-foreground mt-0.5">
            {trigger.cron_expression}
            {trigger.timezone && ` (${trigger.timezone})`}
          </div>
        )}
        {trigger.next_run_at && (
          <div className="text-xs text-muted-foreground">
            {t(($) => $.trigger_row.next_label, { date: formatDate(trigger.next_run_at) })}
          </div>
        )}
      </div>
      <Button
        size="icon"
        variant="ghost"
        className="h-7 w-7 shrink-0"
        onClick={() => setConfirmOpen(true)}
      >
        <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
      </Button>
      <AlertDialog open={confirmOpen} onOpenChange={(v) => { if (!v && !deleting) setConfirmOpen(false); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.trigger_row.delete_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.trigger_row.delete_dialog.description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>
              {t(($) => $.trigger_row.delete_dialog.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              disabled={deleting}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {deleting
                ? t(($) => $.trigger_row.delete_dialog.deleting)
                : t(($) => $.trigger_row.delete_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function AddTriggerDialog({
  open,
  onOpenChange,
  autopilotId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  autopilotId: string;
}) {
  const { t } = useT("autopilots");
  const createTrigger = useCreateAutopilotTrigger();
  const [config, setConfig] = useState<TriggerConfig>(getDefaultTriggerConfig);
  const [label, setLabel] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async () => {
    if (submitting) return;
    const cronExpr = toCronExpression(config);
    if (!cronExpr.trim()) return;
    setSubmitting(true);
    try {
      await createTrigger.mutateAsync({
        autopilotId,
        kind: "schedule",
        cron_expression: cronExpr,
        timezone: config.timezone || undefined,
        label: label.trim() || undefined,
      });
      onOpenChange(false);
      setConfig(getDefaultTriggerConfig());
      setLabel("");
      toast.success(t(($) => $.add_trigger_dialog.toast_added));
    } catch {
      toast.error(t(($) => $.add_trigger_dialog.toast_add_failed));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogTitle>{t(($) => $.add_trigger_dialog.title)}</DialogTitle>
        <div className="space-y-4 pt-2">
          <TriggerConfigSection config={config} onChange={setConfig} />
          <div>
            <label className="text-xs font-medium text-muted-foreground">
              {t(($) => $.add_trigger_dialog.label_field)}
            </label>
            <input
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder={t(($) => $.add_trigger_dialog.label_placeholder)}
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <div className="flex justify-end pt-1">
            <Button size="sm" onClick={handleSubmit} disabled={submitting}>
              {submitting
                ? t(($) => $.add_trigger_dialog.submitting)
                : t(($) => $.add_trigger_dialog.submit)}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function AutopilotDetailPage({ autopilotId }: { autopilotId: string }) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const { getActorName } = useActorName();

  const { data, isLoading } = useQuery(autopilotDetailOptions(wsId, autopilotId));
  const { data: runs = [], isLoading: runsLoading } = useQuery(autopilotRunsOptions(wsId, autopilotId));
  const updateAutopilot = useUpdateAutopilot();
  const deleteAutopilot = useDeleteAutopilot();
  const triggerAutopilot = useTriggerAutopilot();

  const [triggerDialogOpen, setTriggerDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [expandedRunId, setExpandedRunId] = useState<string | null>(null);

  if (isLoading) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-5">
          <Skeleton className="h-4 w-4" />
          <span className="text-muted-foreground">/</span>
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="flex-1 overflow-y-auto">
          <div className="max-w-4xl mx-auto p-6 space-y-8">
            <section className="space-y-4">
              <Skeleton className="h-3 w-20" />
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1">
                  <Skeleton className="h-3 w-12" />
                  <Skeleton className="h-5 w-32" />
                </div>
                <div className="space-y-1">
                  <Skeleton className="h-3 w-12" />
                  <Skeleton className="h-5 w-24" />
                </div>
              </div>
            </section>
            <section className="space-y-3">
              <Skeleton className="h-4 w-16" />
              <Skeleton className="h-10 w-full rounded-md" />
            </section>
            <section className="space-y-3">
              <Skeleton className="h-4 w-24" />
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </section>
          </div>
        </div>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        {t(($) => $.detail.not_found)}
      </div>
    );
  }

  const { autopilot, triggers } = data;

  const handleRunNow = async () => {
    try {
      await triggerAutopilot.mutateAsync(autopilotId);
      toast.success(t(($) => $.detail.toast_triggered));
    } catch (e: any) {
      toast.error(e?.message || t(($) => $.detail.toast_trigger_failed));
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteAutopilot.mutateAsync(autopilotId);
      toast.success(t(($) => $.detail.toast_deleted));
      router.push(wsPaths.autopilots());
    } catch {
      toast.error(t(($) => $.detail.toast_delete_failed));
      setDeleting(false);
    }
  };

  const handleToggleStatus = (checked: boolean) => {
    updateAutopilot.mutate({ id: autopilotId, status: checked ? "active" : "paused" });
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <AppLink href={wsPaths.autopilots()} className="text-muted-foreground hover:text-foreground transition-colors">
            <Zap className="h-4 w-4" />
          </AppLink>
          <span className="text-muted-foreground">/</span>
          <h1 className="text-sm font-medium truncate">{autopilot.title}</h1>
          <div className="ml-1 flex items-center gap-1.5">
            <Switch
              size="sm"
              checked={autopilot.status === "active"}
              onCheckedChange={handleToggleStatus}
              disabled={autopilot.status === "archived"}
              aria-label={
                autopilot.status === "active"
                  ? t(($) => $.detail.pause_aria)
                  : t(($) => $.detail.activate_aria)
              }
            />
            <span className={cn(
              "text-xs font-medium",
              autopilot.status === "active" ? "text-emerald-500" :
              autopilot.status === "paused" ? "text-amber-500" :
              "text-muted-foreground",
            )}>
              {t(($) => $.status[autopilot.status])}
            </span>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => setEditDialogOpen(true)}>
            <Pencil className="h-3.5 w-3.5 mr-1" />
            {t(($) => $.detail.edit)}
          </Button>
          <Button size="sm" onClick={handleRunNow} disabled={autopilot.status !== "active" || triggerAutopilot.isPending}>
            <Play className="h-3.5 w-3.5 mr-1" />
            {triggerAutopilot.isPending
              ? t(($) => $.detail.running)
              : t(($) => $.detail.run_now)}
          </Button>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="max-w-4xl mx-auto p-6 space-y-8">
          {/* Properties */}
          <section className="space-y-4">
            <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
              {t(($) => $.detail.section_properties)}
            </h2>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <label className="text-xs text-muted-foreground">{t(($) => $.detail.field_agent)}</label>
                <div className="mt-1 flex items-center gap-2">
                  <ActorAvatar actorType="agent" actorId={autopilot.assignee_id} size={20} enableHoverCard showStatusDot />
                  <span className="cursor-pointer">{getActorName("agent", autopilot.assignee_id)}</span>
                </div>
              </div>
              <div>
                <label className="text-xs text-muted-foreground">{t(($) => $.detail.field_output_mode)}</label>
                <div className="mt-1">
                  {t(($) => $.execution_mode[autopilot.execution_mode as AutopilotExecutionMode])}
                </div>
              </div>
              {autopilot.description && (
                <div className="col-span-2">
                  <label className="text-xs text-muted-foreground">{t(($) => $.detail.field_prompt)}</label>
                  <div className="mt-1">
                    <ReadonlyContent content={autopilot.description} />
                  </div>
                </div>
              )}
            </div>
          </section>

          {/* Triggers */}
          <section className="space-y-3">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
                {t(($) => $.detail.section_triggers)}
              </h2>
              <Button size="sm" variant="outline" onClick={() => setTriggerDialogOpen(true)}>
                <Plus className="h-3.5 w-3.5 mr-1" />
                {t(($) => $.detail.add_trigger)}
              </Button>
            </div>
            {triggers.length === 0 ? (
              <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
                {t(($) => $.detail.no_triggers)}
              </div>
            ) : (
              <div className="space-y-2">
                {triggers.map((trig) => (
                  <TriggerRow key={trig.id} trigger={trig} autopilotId={autopilotId} />
                ))}
              </div>
            )}
          </section>

          {/* Run History */}
          <section className="space-y-3">
            <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
              {t(($) => $.detail.section_run_history)}
            </h2>
            {runsLoading ? (
              <div className="space-y-1">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : runs.length === 0 ? (
              <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
                {t(($) => $.detail.no_runs)}
              </div>
            ) : (
              <div className="rounded-md border overflow-hidden divide-y">
                {runs.map((run) => (
                  <RunRow
                    key={run.id}
                    run={run}
                    agentId={autopilot.assignee_id}
                    agentName={getActorName("agent", autopilot.assignee_id)}
                    isExpanded={expandedRunId === run.id}
                    onToggle={() => setExpandedRunId(expandedRunId === run.id ? null : run.id)}
                  />
                ))}
              </div>
            )}
          </section>

          {/* Danger zone */}
          <section className="space-y-3 pt-4 border-t">
            <h2 className="text-sm font-medium text-destructive uppercase tracking-wider">
              {t(($) => $.detail.section_danger)}
            </h2>
            <Button size="sm" variant="destructive" onClick={() => setDeleteConfirmOpen(true)}>
              <Trash2 className="h-3.5 w-3.5 mr-1" />
              {t(($) => $.detail.delete_button)}
            </Button>
          </section>
        </div>
      </div>

      <AddTriggerDialog
        open={triggerDialogOpen}
        onOpenChange={setTriggerDialogOpen}
        autopilotId={autopilotId}
      />
      {editDialogOpen && (
        <AutopilotDialog
          mode="edit"
          open={editDialogOpen}
          onOpenChange={setEditDialogOpen}
          autopilotId={autopilot.id}
          initial={{
            title: autopilot.title,
            description: autopilot.description ?? "",
            assignee_id: autopilot.assignee_id,
            execution_mode: autopilot.execution_mode as AutopilotExecutionMode,
          }}
          triggers={triggers}
        />
      )}
      <AlertDialog
        open={deleteConfirmOpen}
        onOpenChange={(v) => { if (!v && !deleting) setDeleteConfirmOpen(false); }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.detail.delete_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.detail.delete_dialog.description, { title: autopilot.title })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>
              {t(($) => $.detail.delete_dialog.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              disabled={deleting}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {deleting
                ? t(($) => $.detail.delete_dialog.deleting)
                : t(($) => $.detail.delete_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
