"use client";

import { useState } from "react";
import {
  Zap, Play, Clock, Plus, Trash2, CheckCircle2, XCircle, Loader2, Pencil,
  Ban, ChevronDown, ChevronRight,
  Webhook, Copy, Check, RotateCw,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { autopilotDetailOptions, autopilotRunsOptions, autopilotRunOptions } from "@multica/core/autopilots/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import {
  useUpdateAutopilot,
  useDeleteAutopilot,
  useTriggerAutopilot,
  useCreateAutopilotTrigger,
  useDeleteAutopilotTrigger,
  useRotateAutopilotTriggerWebhookToken,
} from "@multica/core/autopilots/mutations";
import { buildAutopilotWebhookUrl } from "@multica/core/autopilots";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { useNavigation, AppLink } from "../../navigation";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { ActorAvatar } from "../../common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
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
import type {
  AutopilotExecutionMode,
  AutopilotRun,
  AutopilotSubscriber,
  AutopilotTrigger,
} from "@multica/core/types";
import type { AgentTask } from "@multica/core/types/agent";
import { ReadonlyContent } from "../../editor";
import { TranscriptButton } from "../../common/task-transcript";
import { AutopilotDialog } from "./autopilot-dialog";
import { WebhookPayloadPreview } from "./webhook-payload-preview";
import { WebhookDeliveriesSection } from "./webhook-deliveries-section";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";

function formatDate(date: string): string {
  return new Date(date).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

type RunStatus = "issue_created" | "running" | "skipped" | "completed" | "failed";

const RUN_VISUAL: Record<RunStatus, { color: string; icon: typeof CheckCircle2; spin?: boolean }> = {
  issue_created: { color: "text-blue-500", icon: Clock },
  running: { color: "text-blue-500", icon: Loader2, spin: true },
  // `skipped` (admission check found the assignee runtime offline,
  // MUL-1899) is muted so it doesn't read as a failure-ratio inflator.
  // The row still shows failure_reason which carries the skip context.
  skipped: { color: "text-muted-foreground", icon: Ban },
  completed: { color: "text-emerald-500", icon: CheckCircle2 },
  failed: { color: "text-destructive", icon: XCircle },
};

// WebhookPayloadSlot lazy-fetches the full run (incl. trigger_payload) once
// the parent dialog actually mounts this slot. The list endpoint omits
// trigger_payload to keep responses small (worst case 256 KiB × N runs),
// so the detail-on-demand fetch lives here.
function WebhookPayloadSlot({ autopilotId, runId }: { autopilotId: string; runId: string }) {
  const wsId = useWorkspaceId();
  const { data, isLoading } = useQuery(
    autopilotRunOptions(wsId, autopilotId, runId),
  );
  if (isLoading) {
    return <Skeleton className="h-9 w-full" />;
  }
  if (!data || data.trigger_payload == null) {
    return null;
  }
  return <WebhookPayloadPreview payload={data.trigger_payload} />;
}

function RunRow({ run, agentId, agentName }: { run: AutopilotRun; agentId: string; agentName: string }) {
  const { t } = useT("autopilots");
  const wsPaths = useWorkspacePaths();
  const status = (RUN_VISUAL[run.status as RunStatus] ? (run.status as RunStatus) : "issue_created");
  const visual = RUN_VISUAL[status];
  const StatusIcon = visual.icon;

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

  const content = (
    <>
      <StatusIcon className={cn("h-4 w-4 shrink-0", visual.color, visual.spin && "animate-spin")} />
      <span className={cn("w-24 shrink-0 text-xs font-medium", visual.color)}>
        {t(($) => $.run_status[status])}
      </span>
      <span className="w-20 shrink-0 text-xs text-muted-foreground">
        {t(($) => $.run_source[run.source as "schedule" | "manual" | "webhook" | "api"]) ?? run.source}
      </span>
      <span className="flex-1 min-w-0 text-xs text-muted-foreground truncate">
        {run.issue_id ? (
          t(($) => $.run.issue_linked)
        ) : run.failure_reason ? (
          <span className="text-destructive">{run.failure_reason}</span>
        ) : null}
      </span>
      <span className="w-32 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
        {formatDate(run.triggered_at || run.created_at)}
      </span>
      {syntheticTask && !run.issue_id && (
        <TranscriptButton
          task={syntheticTask}
          agentName={agentName}
          isLive={run.status === "running"}
          title={t(($) => $.run.view_log)}
          headerSlot={
            run.source === "webhook" ? (
              <WebhookPayloadSlot autopilotId={run.autopilot_id} runId={run.id} />
            ) : undefined
          }
        />
      )}
    </>
  );

  const rowClass = "flex items-center gap-3 px-4 py-2.5 text-sm hover:bg-accent/30 transition-colors";

  if (run.issue_id) {
    return (
      <AppLink href={wsPaths.issueDetail(run.issue_id)} className={cn(rowClass, "cursor-pointer")}>
        {content}
      </AppLink>
    );
  }

  return <div className={rowClass}>{content}</div>;
}

function RunHistoryList({
  runs,
  agentId,
  agentName,
}: {
  runs: AutopilotRun[];
  agentId: string;
  agentName: string;
}) {
  const visibleRuns = runs.filter((run) => run.status !== "skipped");
  const skippedRuns = runs.filter((run) => run.status === "skipped");

  return (
    <div className="rounded-md border overflow-hidden">
      {visibleRuns.map((run) => (
        <RunRow key={run.id} run={run} agentId={agentId} agentName={agentName} />
      ))}
      {skippedRuns.length > 0 && (
        <SkippedRunsGroup runs={skippedRuns} agentId={agentId} agentName={agentName} />
      )}
    </div>
  );
}

function SkippedRunsGroup({
  runs,
  agentId,
  agentName,
}: {
  runs: AutopilotRun[];
  agentId: string;
  agentName: string;
}) {
  const { t } = useT("autopilots");
  const [open, setOpen] = useState(false);
  const latestRun = runs[0];
  const ToggleIcon = open ? ChevronDown : ChevronRight;

  return (
    <div className="border-t bg-muted/20">
      <button
        type="button"
        className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm hover:bg-accent/30 transition-colors"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
      >
        <ToggleIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
        <Ban className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="w-24 shrink-0 text-xs font-medium text-muted-foreground">
          {t(($) => $.run.skipped_group.label)}
        </span>
        <span className="flex-1 min-w-0 text-xs text-muted-foreground truncate">
          {t(($) => $.run.skipped_group.summary, { count: runs.length })}
        </span>
        {latestRun && (
          <span className="w-32 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
            {formatDate(latestRun.triggered_at || latestRun.created_at)}
          </span>
        )}
      </button>
      {open && (
        <div className="border-t bg-background">
          {runs.map((run) => (
            <RunRow key={run.id} run={run} agentId={agentId} agentName={agentName} />
          ))}
        </div>
      )}
    </div>
  );
}

function TriggerRow({ trigger, autopilotId, canWrite }: { trigger: AutopilotTrigger; autopilotId: string; canWrite: boolean }) {
  const { t } = useT("autopilots");
  const deleteTrigger = useDeleteAutopilotTrigger();
  const rotateToken = useRotateAutopilotTriggerWebhookToken();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [rotateOpen, setRotateOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [copied, setCopied] = useState(false);

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteTrigger.mutateAsync({ autopilotId, triggerId: trigger.id });
      toast.success(t(($) => $.trigger_row.toast_deleted));
      setConfirmOpen(false);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.trigger_row.toast_delete_failed),
      );
    } finally {
      setDeleting(false);
    }
  };

  const isWebhook = trigger.kind === "webhook";
  const isApi = trigger.kind === "api";
  // Resolve the URL from the server's webhook_url first, then compose
  // from the API base URL (desktop) or window.origin (web). Falls back
  // to the relative path if neither is available.
  const webhookUrl = isWebhook
    ? buildAutopilotWebhookUrl({
        trigger,
        apiBaseUrl: api.getBaseUrl(),
        currentOrigin: typeof window !== "undefined" ? window.location.origin : undefined,
      })
    : null;

  const handleCopy = async () => {
    if (!webhookUrl) return;
    if (await copyText(webhookUrl)) {
      setCopied(true);
      toast.success(t(($) => $.trigger_row.url_copied));
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t(($) => $.trigger_row.url_copy_failed));
    }
  };

  const handleRotate = async () => {
    try {
      await rotateToken.mutateAsync({ autopilotId, triggerId: trigger.id });
      toast.success(t(($) => $.trigger_row.toast_rotated));
      setRotateOpen(false);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.trigger_row.toast_rotate_failed),
      );
    }
  };

  const Icon = isWebhook ? Webhook : isApi ? Zap : Clock;
  const showWebhookUrlRow = isWebhook && webhookUrl;

  // Delete control extracted so a webhook trigger can render it inline
  // with Copy / Rotate on the URL action row (where the other action
  // buttons live), while schedule / api triggers — which have no URL row
  // — keep it pinned to the row's top-right corner. Without this the
  // trash icon visually floats above the URL action buttons because the
  // outer flex uses `items-start`.
  const deleteButton = canWrite ? (
    <Button
      size="icon"
      variant="ghost"
      className="h-7 w-7 shrink-0"
      onClick={() => setConfirmOpen(true)}
      title={t(($) => $.trigger_row.delete_dialog.confirm)}
    >
      <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
    </Button>
  ) : null;

  return (
    <div className="flex items-start gap-3 rounded-md border px-3 py-2">
      <Icon className="h-4 w-4 shrink-0 text-muted-foreground mt-0.5" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-medium">{t(($) => $.trigger_kind[trigger.kind])}</span>
          {trigger.label && (
            <span className="text-xs text-muted-foreground">({trigger.label})</span>
          )}
          {!trigger.enabled && (
            <span className="text-xs bg-muted px-1.5 py-0.5 rounded">
              {t(($) => $.trigger_row.disabled_badge)}
            </span>
          )}
          {isApi && (
            <span className="text-xs bg-muted px-1.5 py-0.5 rounded">
              {t(($) => $.trigger_row.deprecated_badge)}
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
        {showWebhookUrlRow && (
          <div className="mt-1.5 flex items-center gap-1.5">
            <code className="flex-1 min-w-0 truncate rounded bg-muted px-2 py-1 text-xs font-mono text-foreground">
              {webhookUrl}
            </code>
            <Button
              size="icon"
              variant="ghost"
              className="h-7 w-7 shrink-0"
              onClick={handleCopy}
              title={t(($) => $.trigger_row.copy_url)}
            >
              {copied ? <Check className="h-3.5 w-3.5 text-emerald-500" /> : <Copy className="h-3.5 w-3.5 text-muted-foreground" />}
            </Button>
            {canWrite && (
              <Button
                size="icon"
                variant="ghost"
                className="h-7 w-7 shrink-0"
                onClick={() => setRotateOpen(true)}
                title={t(($) => $.trigger_row.rotate_url)}
                disabled={rotateToken.isPending}
              >
                <RotateCw className={cn("h-3.5 w-3.5 text-muted-foreground", rotateToken.isPending && "animate-spin")} />
              </Button>
            )}
            {deleteButton}
          </div>
        )}
      </div>
      {!showWebhookUrlRow && deleteButton}
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
      <AlertDialog open={rotateOpen} onOpenChange={(v) => { if (!v && !rotateToken.isPending) setRotateOpen(false); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.trigger_row.rotate_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.trigger_row.rotate_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={rotateToken.isPending}>
              {t(($) => $.trigger_row.rotate_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleRotate} disabled={rotateToken.isPending}>
              {rotateToken.isPending
                ? t(($) => $.trigger_row.rotate_in_progress)
                : t(($) => $.trigger_row.rotate_confirm_action)}
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
  const [kind, setKind] = useState<"schedule" | "webhook">("schedule");
  const [config, setConfig] = useState<TriggerConfig>(getDefaultTriggerConfig);
  const [label, setLabel] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      if (kind === "schedule") {
        const cronExpr = toCronExpression(config);
        if (!cronExpr.trim()) {
          setSubmitting(false);
          return;
        }
        await createTrigger.mutateAsync({
          autopilotId,
          kind: "schedule",
          cron_expression: cronExpr,
          timezone: config.timezone || undefined,
          label: label.trim() || undefined,
        });
        toast.success(t(($) => $.add_trigger_dialog.toast_added_schedule));
      } else {
        await createTrigger.mutateAsync({
          autopilotId,
          kind: "webhook",
          label: label.trim() || undefined,
        });
        toast.success(t(($) => $.add_trigger_dialog.toast_added_webhook));
      }
      onOpenChange(false);
      setKind("schedule");
      setConfig(getDefaultTriggerConfig());
      setLabel("");
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.add_trigger_dialog.toast_add_failed),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogTitle>{t(($) => $.add_trigger_dialog.title)}</DialogTitle>
        <div className="space-y-4 pt-2">
          <div>
            <label className="text-xs font-medium text-muted-foreground">
              {t(($) => $.add_trigger_dialog.type_label)}
            </label>
            <div className="mt-1 grid grid-cols-2 gap-1 rounded-md bg-muted p-1">
              <button
                type="button"
                onClick={() => setKind("schedule")}
                className={cn(
                  "flex items-center justify-center gap-1.5 rounded px-3 py-1.5 text-sm transition-colors",
                  kind === "schedule"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                <Clock className="h-3.5 w-3.5" />
                {t(($) => $.add_trigger_dialog.type_schedule)}
              </button>
              <button
                type="button"
                onClick={() => setKind("webhook")}
                className={cn(
                  "flex items-center justify-center gap-1.5 rounded px-3 py-1.5 text-sm transition-colors",
                  kind === "webhook"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                <Webhook className="h-3.5 w-3.5" />
                {t(($) => $.add_trigger_dialog.type_webhook)}
              </button>
            </div>
          </div>

          {kind === "schedule" ? (
            <TriggerConfigSection config={config} onChange={setConfig} />
          ) : (
            <p className="rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
              {t(($) => $.add_trigger_dialog.webhook_help)}
            </p>
          )}

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

// Read-only chip row; edits flow through AutopilotDialog → SubscriberMultiSelect
// so the detail page never holds in-flight selection state.
function SubscriberChips({
  subscribers,
}: {
  subscribers: AutopilotSubscriber[] | undefined;
}) {
  const { t } = useT("autopilots");
  const { getActorName } = useActorName();
  const members = (subscribers ?? []).filter((s) => s.user_type === "member");
  if (members.length === 0) {
    return (
      <div className="mt-1 text-sm text-muted-foreground">
        {t(($) => $.detail.field_subscribers_none)}
      </div>
    );
  }
  return (
    <div className="mt-1 flex flex-wrap gap-1.5">
      {members.map((s) => (
        <span
          key={`${s.user_type}:${s.user_id}`}
          className="inline-flex items-center gap-1 rounded-full border bg-background px-2 py-0.5 text-xs"
        >
          <ActorAvatar actorType="member" actorId={s.user_id} size="xs" />
          <span className="max-w-[14rem] truncate">
            {getActorName("member", s.user_id)}
          </span>
        </span>
      ))}
    </div>
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
  const projectId = data?.autopilot.project_id ?? null;
  const { data: project, isLoading: projectLoading } = useQuery({
    ...projectDetailOptions(wsId, projectId ?? ""),
    enabled: Boolean(projectId),
  });

  const [triggerDialogOpen, setTriggerDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

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
  const collaborators = data.collaborators ?? [];
  // Treat an absent can_write (older server) as "allowed" — the backend is the
  // real gate, so the UI only hides controls when the server explicitly says
  // the caller cannot write.
  const canWrite = autopilot.can_write !== false;
  // Managing the access list is narrower than write: granted collaborators can
  // edit/run but cannot grant/revoke. Fall back to canWrite when the server
  // doesn't send the field (older backend).
  const canManageAccess = autopilot.can_manage_access ?? canWrite;

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
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.detail.toast_delete_failed),
      );
      setDeleting(false);
    }
  };

  const handleToggleStatus = (checked: boolean) => {
    updateAutopilot.mutate({ id: autopilotId, status: checked ? "active" : "paused" });
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <BreadcrumbHeader
        segments={[{ href: wsPaths.autopilots(), label: t(($) => $.page.title) }]}
        leaf={
          <>
            <h1 className="min-w-0 truncate text-sm font-medium text-foreground">{autopilot.title}</h1>
            <div className="ml-1 flex items-center gap-1.5 shrink-0">
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
                "text-xs font-medium hidden sm:inline",
                autopilot.status === "active" ? "text-emerald-500" :
                autopilot.status === "paused" ? "text-amber-500" :
                "text-muted-foreground",
              )}>
                {t(($) => $.status[autopilot.status])}
              </span>
            </div>
          </>
        }
        actions={
          canWrite ? (
            <>
              <Button size="sm" variant="outline" onClick={() => setEditDialogOpen(true)} className="px-2 sm:px-2.5" aria-label={t(($) => $.detail.edit)}>
                <Pencil className="h-3.5 w-3.5 sm:mr-1" />
                <span className="hidden sm:inline">{t(($) => $.detail.edit)}</span>
              </Button>
              <Button
                size="sm"
                onClick={handleRunNow}
                disabled={autopilot.status !== "active" || triggerAutopilot.isPending}
                className="px-2 sm:px-2.5"
                aria-label={triggerAutopilot.isPending ? t(($) => $.detail.running) : t(($) => $.detail.run_now)}
              >
                {triggerAutopilot.isPending ? (
                  <Loader2 className="h-3.5 w-3.5 sm:mr-1 animate-spin" />
                ) : (
                  <Play className="h-3.5 w-3.5 sm:mr-1" />
                )}
                <span className="hidden sm:inline">
                  {triggerAutopilot.isPending
                    ? t(($) => $.detail.running)
                    : t(($) => $.detail.run_now)}
                </span>
              </Button>
            </>
          ) : null
        }
      />

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
                  <ActorAvatar
                    actorType={autopilot.assignee_type}
                    actorId={autopilot.assignee_id}
                    size="sm"
                    enableHoverCard={autopilot.assignee_type === "agent"}
                    showStatusDot={autopilot.assignee_type === "agent"}
                  />
                  <span className="cursor-pointer">
                    {getActorName(autopilot.assignee_type, autopilot.assignee_id)}
                  </span>
                </div>
              </div>
              <div>
                <label className="text-xs text-muted-foreground">{t(($) => $.detail.field_created_by)}</label>
                <div className="mt-1 flex items-center gap-2">
                  {/* Creator may be a member or an agent: the HTTP create path stamps
                      member today, but backend logic also writes created_by_type=agent.
                      ActorAvatar/getActorName resolve both, so never assume member. */}
                  <ActorAvatar
                    actorType={autopilot.created_by_type}
                    actorId={autopilot.created_by_id}
                    size="sm"
                    enableHoverCard
                    showStatusDot={autopilot.created_by_type === "agent"}
                  />
                  <span className="cursor-pointer">
                    {getActorName(autopilot.created_by_type, autopilot.created_by_id)}
                  </span>
                </div>
              </div>
              <div>
                <label className="text-xs text-muted-foreground">{t(($) => $.detail.field_output_mode)}</label>
                <div className="mt-1">
                  {t(($) => $.execution_mode[autopilot.execution_mode as AutopilotExecutionMode])}
                </div>
              </div>
              {autopilot.execution_mode === "create_issue" && (
                <div>
                  <label className="text-xs text-muted-foreground">{t(($) => $.detail.field_project)}</label>
                  <div className="mt-1 min-w-0">
                    {!autopilot.project_id ? (
                      <span className="text-muted-foreground">{t(($) => $.detail.no_project)}</span>
                    ) : projectLoading ? (
                      <Skeleton className="h-5 w-32" />
                    ) : project ? (
                      <AppLink
                        href={wsPaths.projectDetail(project.id)}
                        className="inline-flex max-w-full items-center gap-1.5 text-foreground hover:underline"
                      >
                        <ProjectIcon project={project} size="md" />
                        <span className="truncate">{project.title}</span>
                      </AppLink>
                    ) : (
                      <span className="text-muted-foreground">{t(($) => $.detail.project_unavailable)}</span>
                    )}
                  </div>
                </div>
              )}
              {autopilot.execution_mode === "create_issue" && (
                <div className="col-span-2">
                  <label className="text-xs text-muted-foreground">
                    {t(($) => $.detail.field_subscribers)}
                  </label>
                  <SubscriberChips
                    subscribers={autopilot.subscribers}
                  />
                </div>
              )}
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
              {canWrite && (
                <Button size="sm" variant="outline" onClick={() => setTriggerDialogOpen(true)}>
                  <Plus className="h-3.5 w-3.5 mr-1" />
                  {t(($) => $.detail.add_trigger)}
                </Button>
              )}
            </div>
            {triggers.length === 0 ? (
              <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
                {t(($) => $.detail.no_triggers)}
              </div>
            ) : (
              <div className="space-y-2">
                {triggers.map((trig) => (
                  <TriggerRow key={trig.id} trigger={trig} autopilotId={autopilotId} canWrite={canWrite} />
                ))}
              </div>
            )}
          </section>

          {/* Webhook deliveries — only renders when at least one webhook
              trigger is configured. The component does its own fetch so
              schedule-only autopilots don't pay for an empty list query. */}
          <WebhookDeliveriesSection
            autopilotId={autopilotId}
            hasWebhookTrigger={triggers.some((trig) => trig.kind === "webhook")}
          />

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
              <RunHistoryList
                runs={runs}
                agentId={autopilot.assignee_id}
                agentName={getActorName(autopilot.assignee_type, autopilot.assignee_id)}
              />
            )}
          </section>

          {/* Danger zone */}
          {canWrite && (
            <section className="space-y-3 pt-4 border-t">
              <h2 className="text-sm font-medium text-destructive uppercase tracking-wider">
                {t(($) => $.detail.section_danger)}
              </h2>
              <Button size="sm" variant="destructive" onClick={() => setDeleteConfirmOpen(true)}>
                <Trash2 className="h-3.5 w-3.5 mr-1" />
                {t(($) => $.detail.delete_button)}
              </Button>
            </section>
          )}
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
            project_id: autopilot.project_id ?? null,
            assignee_type: autopilot.assignee_type,
            assignee_id: autopilot.assignee_id,
            execution_mode: autopilot.execution_mode as AutopilotExecutionMode,
            subscriber_user_ids:
              autopilot.subscribers
                ?.filter((s) => s.user_type === "member")
                .map((s) => s.user_id) ?? [],
          }}
          triggers={triggers}
          collaborators={collaborators}
          canManageAccess={canManageAccess}
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
