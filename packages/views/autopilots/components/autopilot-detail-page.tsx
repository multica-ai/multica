"use client";

import { useMemo, useState, type ReactNode } from "react";
import {
  Zap, Play, Clock, Plus, Trash2, CheckCircle2, XCircle, Loader2, Pencil,
  Ban, ChevronDown, ChevronRight, MoreHorizontal, Link2, ArrowUpRight, FilePlus2,
  Webhook, RotateCw, Server,
} from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import { autopilotDetailOptions, autopilotRunsOptions, autopilotRunOptions } from "@multica/core/autopilots/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import {
  useUpdateAutopilot,
  useDeleteAutopilot,
  useCreateAutopilotTrigger,
  useDeleteAutopilotTrigger,
  useRotateAutopilotTriggerWebhookToken,
} from "@multica/core/autopilots/mutations";
import { buildAutopilotWebhookUrl } from "@multica/core/autopilots";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useNavigation, AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
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
import { ScheduleEditor } from "./schedule-editor/schedule-editor";
import { WebhookUrlField } from "./webhook-url-field";
import { getDefaultScheduleConfig, type ScheduleConfig } from "./schedule-editor/model";
import { browserTimezone } from "../../common/timezone-select";
import { cronFields, parseCron, toCron } from "./schedule-editor/cron-mapping";
import { useDescribeSchedule } from "./schedule-editor/describe";
import { formatInTimeZone } from "../../common/format-in-time-zone";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { useScheduleSubmitGate } from "./schedule-editor/validate";
import type {
  Autopilot,
  AutopilotExecutionMode,
  AutopilotRun,
  AutopilotTrigger,
} from "@multica/core/types";
import type { AgentTask } from "@multica/core/types/agent";
import { ReadonlyContent } from "../../editor";
import { TranscriptButton } from "../../common/task-transcript";
import { AutopilotDialog } from "./autopilot-dialog";
import { EditScheduleTriggerDialog } from "./edit-schedule-trigger-dialog";
import { useRunAutopilotNow } from "./use-run-autopilot-now";
import { useRegisterCommandBarContext } from "../../search/register-context";
import { WebhookPayloadPreview } from "./webhook-payload-preview";
import { WebhookDeliveriesSection } from "./webhook-deliveries-section";
import { AgentPicker } from "./pickers/agent-picker";
import { ProjectPicker } from "../../projects/components/project-picker";
import { ProjectIcon } from "../../projects/components/project-icon";
import {
  PickerEmpty,
  PickerItem,
  PropertyPicker,
} from "../../issues/components/pickers/property-picker";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import {
  OBJECT_PAGE_BODY,
  ObjectIconMark,
  ObjectIdentity,
  ObjectPage,
  ObjectPanelSection,
  ObjectProperty,
  ObjectSection,
  ObjectSectionAction,
  ObjectSectionCard,
} from "../../layout/object-page";
import { useT } from "../../i18n";

const LAYOUT_ID = "multica_autopilot_detail_layout";

const TRIGGER_ICON: Record<AutopilotTrigger["kind"], typeof Clock> = {
  schedule: Clock,
  webhook: Webhook,
  api: Zap,
};

// A property value that opens its picker; the row around it carries the hover.
const PROPERTY_TRIGGER =
  "flex min-w-0 items-center gap-1.5 rounded-sm text-caption outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-default";

const OUTPUT_MODES: AutopilotExecutionMode[] = ["create_issue", "run_only"];
const OUTPUT_MODE_ICONS: Record<AutopilotExecutionMode, typeof FilePlus2> = {
  create_issue: FilePlus2,
  run_only: Play,
};

// A run that already happened is an instant in the reader's day, so it reads in
// the reader's zone (no timeZone passed). A run that is still to come belongs to
// the schedule that will fire it — see the trigger row, which passes the
// trigger's own timezone.
type RunStatus = "issue_created" | "running" | "skipped" | "completed" | "failed";

const RUN_VISUAL: Record<RunStatus, { color: string; icon: typeof CheckCircle2; spin?: boolean }> = {
  issue_created: { color: "text-info", icon: Clock },
  running: { color: "text-info", icon: Loader2, spin: true },
  // `skipped` (admission check found the assignee runtime offline,
  // MUL-1899) is muted so it doesn't read as a failure-ratio inflator.
  // The row still shows failure_reason which carries the skip context.
  skipped: { color: "text-muted-foreground", icon: Ban },
  completed: { color: "text-success", icon: CheckCircle2 },
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
  const { t, i18n } = useT("autopilots");
  const wsPaths = useWorkspacePaths();
  const status = (RUN_VISUAL[run.status as RunStatus] ? (run.status as RunStatus) : "issue_created");
  const visual = RUN_VISUAL[status];
  const StatusIcon = visual.icon;
  const statusLabel = t(($) => $.run_status[status]);

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
      <span className="flex shrink-0" title={statusLabel}>
        <StatusIcon aria-hidden="true" className={cn("size-4", visual.color, visual.spin && "animate-spin")} />
        <span className="sr-only">{statusLabel}</span>
      </span>
      <span className="w-28 shrink-0 text-caption tabular-nums text-muted-foreground">
        {formatInTimeZone(run.triggered_at || run.created_at, undefined, i18n.language)}
      </span>
      <span
        className={cn(
          "min-w-0 flex-1 truncate",
          !run.issue_id && run.failure_reason ? "text-destructive" : "text-foreground",
        )}
      >
        {run.issue_id
          ? t(($) => $.run.issue_linked)
          : run.failure_reason || statusLabel}
      </span>
      <span className="shrink-0 text-caption text-muted-foreground">
        {t(($) => $.run_source[run.source as "schedule" | "manual" | "webhook" | "api"]) ?? run.source}
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

  const rowClass = "flex min-h-10 items-center gap-3 px-3 py-2 text-body transition-colors hover:bg-accent/30";

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
    <ObjectSectionCard>
      {visibleRuns.map((run) => (
        <RunRow key={run.id} run={run} agentId={agentId} agentName={agentName} />
      ))}
      {skippedRuns.length > 0 && (
        <SkippedRunsGroup runs={skippedRuns} agentId={agentId} agentName={agentName} />
      )}
    </ObjectSectionCard>
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
  const { t, i18n } = useT("autopilots");
  const [open, setOpen] = useState(false);
  const latestRun = runs[0];
  const ToggleIcon = open ? ChevronDown : ChevronRight;

  return (
    <div className="bg-muted/20">
      <button
        type="button"
        className="flex min-h-10 w-full items-center gap-3 px-3 py-2 text-left text-body transition-colors hover:bg-accent/30"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
      >
        <ToggleIcon className="size-4 shrink-0 text-muted-foreground" />
        {latestRun && (
          <span className="w-28 shrink-0 text-caption tabular-nums text-muted-foreground">
            {formatInTimeZone(latestRun.triggered_at || latestRun.created_at, undefined, i18n.language)}
          </span>
        )}
        <span className="min-w-0 flex-1 truncate text-muted-foreground">
          <span className="font-medium">{t(($) => $.run.skipped_group.label)}</span>
          {" · "}
          {t(($) => $.run.skipped_group.summary, { count: runs.length })}
        </span>
        <Ban className="size-4 shrink-0 text-muted-foreground" />
      </button>
      {open && (
        <div className="divide-y border-t bg-background">
          {runs.map((run) => (
            <RunRow key={run.id} run={run} agentId={agentId} agentName={agentName} />
          ))}
        </div>
      )}
    </div>
  );
}

export function TriggerRow({ trigger, autopilotId, canWrite }: { trigger: AutopilotTrigger; autopilotId: string; canWrite: boolean }) {
  const { t, i18n } = useT("autopilots");
  const describeSchedule = useDescribeSchedule();
  const deleteTrigger = useDeleteAutopilotTrigger();
  const rotateToken = useRotateAutopilotTriggerWebhookToken();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [rotateOpen, setRotateOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

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

  const Icon = TRIGGER_ICON[trigger.kind] ?? Clock;
  const showWebhookUrlRow = isWebhook && webhookUrl;
  // null when the expression is beyond the structured model — those rows keep
  // showing the raw cron on its own.
  const scheduleConfig = trigger.cron_expression
    ? parseCron(trigger.cron_expression, trigger.timezone ?? "UTC")
    : null;
  const scheduleDescription = scheduleConfig ? describeSchedule(scheduleConfig) : null;
  const scheduleText = trigger.cron_expression
    ? [scheduleDescription ?? trigger.cron_expression, trigger.timezone].filter(Boolean).join(" · ")
    : null;

  // A webhook trigger's Delete sits with Copy / Rotate on its URL row, where
  // the other actions on that trigger live; every other kind keeps it at the
  // end of the summary line.
  const deleteButton = canWrite ? (
    <IconButton
      size="icon-sm"
      variant="ghost"
      className="shrink-0 text-muted-foreground"
      onClick={() => setConfirmOpen(true)}
      label={t(($) => $.trigger_row.delete_dialog.confirm)}
    >
      <Trash2 className="size-3.5" />
    </IconButton>
  ) : null;

  // Schedule rows only: cron and timezone are the fields this dialog edits, and
  // the API rejects them on any other kind. It rides alongside Delete so the
  // row that states a schedule is also the row that can change it — without it
  // an autopilot with two schedules has no editable schedule at all (MUL-7478).
  const editButton =
    canWrite && trigger.kind === "schedule" ? (
      <IconButton
        size="icon-sm"
        variant="ghost"
        className="shrink-0 text-muted-foreground"
        onClick={() => setEditOpen(true)}
        label={t(($) => $.trigger_row.edit_schedule)}
      >
        <Pencil className="size-3.5" />
      </IconButton>
    ) : null;

  return (
    <div className="px-3 py-2">
      <div className="flex min-h-7 items-center gap-2">
        <Icon className="size-4 shrink-0 text-muted-foreground" />
        <span className="shrink-0 text-body font-medium">{t(($) => $.trigger_kind[trigger.kind])}</span>
        {trigger.label && (
          <span className="min-w-0 shrink truncate text-caption text-muted-foreground">({trigger.label})</span>
        )}
        {!trigger.enabled && (
          <span className="shrink-0 rounded-xs bg-muted px-1.5 py-0.5 text-caption">
            {t(($) => $.trigger_row.disabled_badge)}
          </span>
        )}
        {isApi && (
          <span className="shrink-0 rounded-xs bg-muted px-1.5 py-0.5 text-caption">
            {t(($) => $.trigger_row.deprecated_badge)}
          </span>
        )}
        {scheduleText && (
          <span className="min-w-0 truncate text-caption text-muted-foreground" title={scheduleText}>
            {scheduleText}
          </span>
        )}
        <div className="ml-auto flex shrink-0 items-center gap-0.5">
          {/* A disabled trigger keeps the next_run_at it had — the dispatcher
              filters on `enabled` instead of clearing it — so the row would
              otherwise carry the Disabled badge and a promise to run at 09:00
              in the same breath. The badge is the true one. */}
          {trigger.next_run_at && trigger.enabled && (
            <span className="mr-1 hidden text-caption tabular-nums text-muted-foreground sm:inline">
              {t(($) => $.trigger_row.next_label, {
                date: formatInTimeZone(
                  trigger.next_run_at,
                  trigger.timezone ?? undefined,
                  i18n.language,
                ),
              })}
            </span>
          )}
          {editButton}
          {!showWebhookUrlRow && deleteButton}
        </div>
      </div>
      {scheduleDescription !== null && scheduleConfig !== null && (
        // Fields only: the zone already reads out in the line above, where a
        // person can use it — same rule as the editor's readback.
        <div className="pl-6 font-mono text-micro text-muted-foreground">
          {cronFields(scheduleConfig)}
        </div>
      )}
      {showWebhookUrlRow && (
        <div className="mt-1.5 pl-6">
          <WebhookUrlField
            url={webhookUrl}
            actions={
              <>
                {canWrite && (
                  <IconButton
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7 shrink-0"
                    onClick={() => setRotateOpen(true)}
                    label={t(($) => $.trigger_row.rotate_url)}
                    disabled={rotateToken.isPending}
                  >
                    <RotateCw className={cn("h-3.5 w-3.5 text-muted-foreground", rotateToken.isPending && "animate-spin")} />
                  </IconButton>
                )}
                {deleteButton}
              </>
            }
          />
        </div>
      )}
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
      <EditScheduleTriggerDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        autopilotId={autopilotId}
        trigger={trigger}
      />
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
  const wsId = useWorkspaceId();
  const createTrigger = useCreateAutopilotTrigger();
  const [kind, setKind] = useState<"schedule" | "webhook">("schedule");
  const [config, setConfig] = useState<ScheduleConfig>(() =>
    getDefaultScheduleConfig(browserTimezone()),
  );
  const [label, setLabel] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const scheduleGate = useScheduleSubmitGate(wsId);
  const canSubmit = !submitting && (kind !== "schedule" || scheduleGate.scheduleValid);

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      if (kind === "schedule") {
        if (!(await scheduleGate.ensureAccepted(config))) {
          setSubmitting(false);
          return;
        }
        const cronExpr = toCron(config);
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
      setConfig(getDefaultScheduleConfig(browserTimezone()));
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
        {/* DialogContent is a grid, so without min-w-0 this item's min-width is
            its content's — and the cron readback is one unbreakable line that
            would push the track past the dialog instead of truncating. */}
        <div className="min-w-0 space-y-4 pt-2">
          <div>
            <label className="text-caption font-medium text-muted-foreground">
              {t(($) => $.add_trigger_dialog.type_label)}
            </label>
            <div className="mt-1">
              <SegmentedToggle
                value={kind}
                onChange={setKind}
                buttonClassName="px-3 py-1.5 text-body"
                options={[
                  [
                    "schedule",
                    <span key="schedule" className="flex items-center justify-center gap-1.5">
                      <Clock className="h-3.5 w-3.5" />
                      {t(($) => $.add_trigger_dialog.type_schedule)}
                    </span>,
                  ],
                  [
                    "webhook",
                    <span key="webhook" className="flex items-center justify-center gap-1.5">
                      <Webhook className="h-3.5 w-3.5" />
                      {t(($) => $.add_trigger_dialog.type_webhook)}
                    </span>,
                  ],
                ]}
              />
            </div>
          </div>

          {kind === "schedule" ? (
            <ScheduleEditor
              value={config}
              onChange={(next) => {
                scheduleGate.clearRejection();
                setConfig(next);
              }}
              wsId={wsId}
              onValidityChange={scheduleGate.onValidityChange}
              // Same reason as the autopilot dialog: the submit path reads the
              // schedule, validates it over the network, then writes what it
              // read — an edit landing inside that window would be discarded.
              disabled={submitting}
            />
          ) : (
            <p className="rounded-md bg-muted/50 px-3 py-2 text-caption text-muted-foreground">
              {t(($) => $.add_trigger_dialog.webhook_help)}
            </p>
          )}

          <div>
            <label className="text-caption font-medium text-muted-foreground">
              {t(($) => $.add_trigger_dialog.label_field)}
            </label>
            <input
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder={t(($) => $.add_trigger_dialog.label_placeholder)}
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-body outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <div className="flex justify-end pt-1">
            <Button size="sm" onClick={handleSubmit} disabled={!canSubmit}>
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

/**
 * Member subscribers as one property: the picker edits a draft while it is
 * open and saves the whole list once on close, because the server replaces
 * the list on every write.
 */
function SubscribersProperty({ autopilot, canWrite }: { autopilot: Autopilot; canWrite: boolean }) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const updateAutopilot = useUpdateAutopilot();
  const { data: members = [] } = useQuery({ ...memberListOptions(wsId), enabled: canWrite });
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const [draft, setDraft] = useState<string[] | null>(null);

  const savedIds = useMemo(
    () => (autopilot.subscribers ?? []).filter((s) => s.user_type === "member").map((s) => s.user_id),
    [autopilot.subscribers],
  );
  const selectedIds = draft ?? savedIds;

  const value =
    selectedIds.length === 0 ? (
      <span className="truncate text-muted-foreground">{t(($) => $.detail.field_subscribers_none)}</span>
    ) : selectedIds.length === 1 ? (
      <>
        <ActorAvatar actorType="member" actorId={selectedIds[0]!} size="xs" />
        <span className="truncate">{getActorName("member", selectedIds[0]!)}</span>
      </>
    ) : (
      <>
        <span className="flex shrink-0 items-center -space-x-1">
          {selectedIds.slice(0, 4).map((id) => (
            <span key={id} className="inline-flex rounded-full ring-2 ring-background">
              <ActorAvatar actorType="member" actorId={id} size="xs" />
            </span>
          ))}
        </span>
        <span className="truncate tabular-nums text-muted-foreground">{selectedIds.length}</span>
      </>
    );

  if (!canWrite) return value;

  const query = filter.trim().toLowerCase();
  const filtered = members.filter(
    (m) => !query || m.name.toLowerCase().includes(query) || matchesPinyin(m.name, query),
  );

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (next) {
      setDraft(savedIds);
      return;
    }
    setFilter("");
    const changed =
      draft !== null &&
      (draft.length !== savedIds.length || draft.some((id) => !savedIds.includes(id)));
    setDraft(null);
    if (!changed) return;
    updateAutopilot.mutate(
      {
        id: autopilot.id,
        subscribers: draft.map((user_id) => ({ user_type: "member" as const, user_id })),
      },
      { onError: () => toast.error(t(($) => $.dialog.toast_update_failed)) },
    );
  };

  const toggle = (userId: string) => {
    setDraft((prev) => {
      const current = prev ?? savedIds;
      return current.includes(userId) ? current.filter((id) => id !== userId) : [...current, userId];
    });
  };

  return (
    <PropertyPicker
      open={open}
      onOpenChange={handleOpenChange}
      width="w-64"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.dialog.subscribers_search_placeholder)}
      onSearchChange={setFilter}
      triggerRender={<button type="button" className={PROPERTY_TRIGGER} />}
      trigger={value}
    >
      {filtered.length === 0 ? (
        <PickerEmpty />
      ) : (
        filtered.map((m) => (
          <PickerItem key={m.user_id} selected={selectedIds.includes(m.user_id)} onClick={() => toggle(m.user_id)}>
            <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
            <span className="truncate">{m.name}</span>
          </PickerItem>
        ))
      )}
    </PropertyPicker>
  );
}

function AutopilotProperties({ autopilot, canWrite }: { autopilot: Autopilot; canWrite: boolean }) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { getActorName } = useActorName();
  const updateAutopilot = useUpdateAutopilot();
  const projectId = autopilot.project_id ?? null;
  const { data: project, isLoading: projectLoading } = useQuery({
    ...projectDetailOptions(wsId, projectId ?? ""),
    enabled: Boolean(projectId),
  });

  const update = (fields: Omit<Parameters<typeof updateAutopilot.mutate>[0], "id">) =>
    updateAutopilot.mutate(
      { id: autopilot.id, ...fields },
      { onError: () => toast.error(t(($) => $.dialog.toast_update_failed)) },
    );

  const mode = autopilot.execution_mode as AutopilotExecutionMode;
  const ModeIcon = OUTPUT_MODE_ICONS[mode] ?? FilePlus2;

  const assigneeValue = (
    <>
      <ActorAvatar
        actorType={autopilot.assignee_type}
        actorId={autopilot.assignee_id}
        size="xs"
        enableHoverCard={autopilot.assignee_type === "agent"}
        showStatusDot={autopilot.assignee_type === "agent"}
      />
      <span className="truncate">{getActorName(autopilot.assignee_type, autopilot.assignee_id)}</span>
    </>
  );

  const projectValue = !projectId ? (
    <span className="truncate text-muted-foreground">{t(($) => $.detail.no_project)}</span>
  ) : projectLoading ? (
    <Skeleton className="h-4 w-24" />
  ) : project ? (
    <>
      <ProjectIcon project={project} size="sm" />
      <span className="truncate">{project.title}</span>
    </>
  ) : (
    <span className="truncate text-muted-foreground">{t(($) => $.detail.project_unavailable)}</span>
  );

  return (
    <>
      <ObjectProperty label={t(($) => $.page.table.status)}>
        <Switch
          size="sm"
          checked={autopilot.status === "active"}
          onCheckedChange={(checked) => update({ status: checked ? "active" : "paused" })}
          disabled={!canWrite || autopilot.status === "archived"}
          aria-label={
            autopilot.status === "active"
              ? t(($) => $.detail.pause_aria)
              : t(($) => $.detail.activate_aria)
          }
        />
        <span className="truncate">{t(($) => $.status[autopilot.status])}</span>
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.detail.field_agent)}>
        {canWrite ? (
          <AgentPicker
            assignee={{ type: autopilot.assignee_type, id: autopilot.assignee_id }}
            onChange={(next) => {
              if (next.type === autopilot.assignee_type && next.id === autopilot.assignee_id) return;
              update({ assignee_type: next.type, assignee_id: next.id });
            }}
            triggerRender={<button type="button" className={PROPERTY_TRIGGER} />}
            trigger={assigneeValue}
          />
        ) : (
          assigneeValue
        )}
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.detail.field_output_mode)}>
        {canWrite ? (
          <DropdownMenu>
            <DropdownMenuTrigger render={<button type="button" className={PROPERTY_TRIGGER} />}>
              <ModeIcon className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{t(($) => $.execution_mode[mode])}</span>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-auto min-w-44">
              <DropdownMenuRadioGroup
                value={mode}
                onValueChange={(next) => {
                  if (next !== mode) update({ execution_mode: next as AutopilotExecutionMode });
                }}
              >
                {OUTPUT_MODES.map((key) => {
                  const Icon = OUTPUT_MODE_ICONS[key];
                  return (
                    <DropdownMenuRadioItem key={key} value={key}>
                      <Icon className="size-3.5 text-muted-foreground" />
                      {t(($) => $.execution_mode[key])}
                    </DropdownMenuRadioItem>
                  );
                })}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <>
            <ModeIcon className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{t(($) => $.execution_mode[mode])}</span>
          </>
        )}
      </ObjectProperty>
      {/* Shown for BOTH output modes (MUL-6681): a run_only autopilot's
          project decides its execution environment (repository /
          local_directory, and therefore worktree isolation), so an
          operator debugging a run needs to see it here. */}
      <ObjectProperty label={t(($) => $.detail.field_project)}>
        {canWrite ? (
          <ProjectPicker
            projectId={projectId}
            onUpdate={(u) => {
              const next = u.project_id ?? null;
              if (next !== projectId) update({ project_id: next });
            }}
            triggerRender={<button type="button" className={PROPERTY_TRIGGER} />}
          />
        ) : (
          projectValue
        )}
        {project && (
          <AppLink
            href={wsPaths.projectDetail(project.id)}
            aria-label={t(($) => $.detail.open_project)}
            title={t(($) => $.detail.open_project)}
            className="ml-auto flex shrink-0 rounded-sm p-0.5 text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowUpRight className="size-3.5" />
          </AppLink>
        )}
      </ObjectProperty>
      {autopilot.execution_mode === "create_issue" && (
        <ObjectProperty label={t(($) => $.detail.field_subscribers)}>
          <SubscribersProperty autopilot={autopilot} canWrite={canWrite} />
        </ObjectProperty>
      )}
      <ObjectProperty label={t(($) => $.detail.field_created_by)}>
        {/* Creator may be a member or an agent: the HTTP create path stamps
            member today, but backend logic also writes created_by_type=agent.
            ActorAvatar/getActorName resolve both, so never assume member. */}
        <ActorAvatar
          actorType={autopilot.created_by_type}
          actorId={autopilot.created_by_id}
          size="xs"
          enableHoverCard
          showStatusDot={autopilot.created_by_type === "agent"}
        />
        <span className="truncate">{getActorName(autopilot.created_by_type, autopilot.created_by_id)}</span>
      </ObjectProperty>
    </>
  );
}

/** One line on what the autopilot does: when it fires, then what the run produces. */
function useAutopilotSummary(autopilot: Autopilot | null, triggers: AutopilotTrigger[]) {
  const { t } = useT("autopilots");
  const { getActorName } = useActorName();
  const describeSchedule = useDescribeSchedule();
  if (!autopilot) return { icon: Zap, text: "" };
  const primary = triggers.find((trig) => trig.enabled) ?? triggers[0];
  const agent = getActorName(autopilot.assignee_type, autopilot.assignee_id);
  const action =
    autopilot.execution_mode === "run_only"
      ? t(($) => $.detail.summary.run_only, { agent })
      : t(($) => $.detail.summary.create_issue, { agent });
  if (!primary) return { icon: Zap, text: action };
  const config = primary.cron_expression
    ? parseCron(primary.cron_expression, primary.timezone ?? "UTC")
    : null;
  const when = (config && describeSchedule(config)) || t(($) => $.trigger_kind[primary.kind]);
  return {
    icon: TRIGGER_ICON[primary.kind] ?? Zap,
    text: t(($) => $.detail.summary.with_trigger, { trigger: when, action }),
  };
}

function SectionSkeleton() {
  return (
    <div className="space-y-2">
      <Skeleton className="h-4 w-24" />
      <Skeleton className="h-16 w-full rounded-lg" />
    </div>
  );
}

export function AutopilotDetailPage({ autopilotId }: { autopilotId: string }) {
  const { t } = useT("autopilots");
  const { t: tLayout } = useT("layout");
  const { t: tCommon } = useT("common");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const { getActorName } = useActorName();

  const { data, isLoading } = useQuery(autopilotDetailOptions(wsId, autopilotId));
  const { data: runs = [], isLoading: runsLoading } = useQuery(autopilotRunsOptions(wsId, autopilotId));
  const deleteAutopilot = useDeleteAutopilot();
  const { runNow, isPending: runNowPending } = useRunAutopilotNow();
  const loadedAutopilot = data?.autopilot ?? null;
  useRegisterCommandBarContext(
    useMemo(
      () => (loadedAutopilot ? { kind: "autopilot", autopilot: loadedAutopilot } : null),
      [loadedAutopilot],
    ),
  );
  const summary = useAutopilotSummary(loadedAutopilot, data?.triggers ?? []);

  const [triggerDialogOpen, setTriggerDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const segments = [{ href: wsPaths.autopilots(), label: tLayout(($) => $.panels.autopilots.list) }];

  const shell = (leaf: ReactNode, children: ReactNode) => (
    <ObjectPage layoutId={LAYOUT_ID} segments={segments} leaf={leaf}>
      {children}
    </ObjectPage>
  );

  if (isLoading) {
    return shell(
      <Skeleton className="h-4 w-32" />,
      <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
        <div className="flex items-center gap-3">
          <Skeleton className="size-12 rounded-xl" />
          <div className="space-y-1.5">
            <Skeleton className="h-5 w-40" />
            <Skeleton className="h-4 w-64" />
          </div>
        </div>
        <SectionSkeleton />
        <SectionSkeleton />
        <SectionSkeleton />
      </div>,
    );
  }

  if (!data) {
    return shell(
      <span className="min-w-0 truncate text-muted-foreground">{t(($) => $.detail.not_found)}</span>,
      <div className="flex h-full items-center justify-center text-body text-muted-foreground">
        {t(($) => $.detail.not_found)}
      </div>,
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

  const copyLink = () => {
    void copyText(router.getShareableUrl(wsPaths.autopilotDetail(autopilotId))).then((ok) => {
      if (ok) toast.success(t(($) => $.detail.link_copied));
      else toast.error(t(($) => $.detail.link_copy_failed));
    });
  };

  return (
    <ObjectPage
      layoutId={LAYOUT_ID}
      segments={segments}
      leaf={<span className="min-w-0 truncate">{autopilot.title}</span>}
      actions={
        <>
          {canWrite && (
            <Button
              size="sm"
              onClick={() => runNow(autopilotId)}
              disabled={autopilot.status !== "active" || runNowPending}
            >
              {runNowPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Play className="size-3.5" />
              )}
              {runNowPending ? t(($) => $.detail.running) : t(($) => $.detail.run_now)}
            </Button>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <IconButton
                  label={tCommon(($) => $.more_actions)}
                  tooltipSide="bottom"
                  className="text-muted-foreground"
                />
              }
            >
              <MoreHorizontal />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-auto min-w-44">
              {canWrite && (
                <DropdownMenuItem onClick={() => setEditDialogOpen(true)}>
                  <Pencil className="size-3.5" />
                  {t(($) => $.detail.edit)}
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onClick={copyLink}>
                <Link2 className="size-3.5" />
                {t(($) => $.detail.copy_link)}
              </DropdownMenuItem>
              {canWrite && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onClick={() => setDeleteConfirmOpen(true)}>
                    <Trash2 className="size-3.5" />
                    {t(($) => $.detail.delete_button)}
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </>
      }
      panel={
        <ObjectPanelSection title={tLayout(($) => $.object_page.properties)}>
          <AutopilotProperties autopilot={autopilot} canWrite={canWrite} />
        </ObjectPanelSection>
      }
    >
      <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
        {autopilot.pause_reason === "agent_runtime_required" && (
          <div
            role="status"
            className="flex items-center gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-caption"
          >
            <Server className="size-3.5 shrink-0 text-warning" />
            <span className="flex-1">
              {t(($) => $.detail.paused_runtime_required)}
            </span>
            {autopilot.assignee_type === "agent" && (
              <AppLink
                href={`${wsPaths.agentDetail(autopilot.assignee_id)}?view=general`}
                className="font-medium underline underline-offset-2"
              >
                {t(($) => $.detail.bind_runtime)}
              </AppLink>
            )}
          </div>
        )}

        <ObjectIdentity
          media={<ObjectIconMark icon={summary.icon} />}
          title={autopilot.title}
          description={summary.text}
        />

        <ObjectSection
          title={t(($) => $.detail.field_prompt)}
          action={
            canWrite ? (
              <ObjectSectionAction icon={Pencil} onClick={() => setEditDialogOpen(true)}>
                {t(($) => $.detail.edit)}
              </ObjectSectionAction>
            ) : undefined
          }
        >
          {autopilot.description ? (
            <ObjectSectionCard className="px-3 py-2.5">
              <ReadonlyContent content={autopilot.description} />
            </ObjectSectionCard>
          ) : (
            <div className="rounded-lg border border-dashed p-4 text-center text-body text-muted-foreground">
              {t(($) => $.detail.no_prompt)}
            </div>
          )}
        </ObjectSection>

        <ObjectSection
          title={t(($) => $.detail.section_triggers)}
          count={triggers.length}
          action={
            canWrite ? (
              <ObjectSectionAction icon={Plus} onClick={() => setTriggerDialogOpen(true)}>
                {t(($) => $.detail.add_trigger)}
              </ObjectSectionAction>
            ) : undefined
          }
        >
          {triggers.length === 0 ? (
            <div className="rounded-lg border border-dashed p-4 text-center text-body text-muted-foreground">
              {t(($) => $.detail.no_triggers)}
            </div>
          ) : (
            <ObjectSectionCard>
              {triggers.map((trig) => (
                <TriggerRow key={trig.id} trigger={trig} autopilotId={autopilotId} canWrite={canWrite} />
              ))}
            </ObjectSectionCard>
          )}
        </ObjectSection>

        {/* Webhook deliveries — only renders when at least one webhook
            trigger is configured. The component does its own fetch so
            schedule-only autopilots don't pay for an empty list query. */}
        <WebhookDeliveriesSection
          autopilotId={autopilotId}
          hasWebhookTrigger={triggers.some((trig) => trig.kind === "webhook")}
        />

        <ObjectSection
          title={t(($) => $.detail.section_run_history)}
          count={runsLoading ? undefined : runs.length}
        >
          {runsLoading ? (
            <div className="space-y-1">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : runs.length === 0 ? (
            <div className="rounded-lg border border-dashed p-4 text-center text-body text-muted-foreground">
              {t(($) => $.detail.no_runs)}
            </div>
          ) : (
            <RunHistoryList
              runs={runs}
              agentId={autopilot.assignee_id}
              agentName={getActorName(autopilot.assignee_type, autopilot.assignee_id)}
            />
          )}
        </ObjectSection>
      </div>

      {/* Mounted only while open, like the edit dialog: otherwise a rejected
          cron leaves scheduleValid=false behind for the next open. */}
      {triggerDialogOpen && (
        <AddTriggerDialog
          open={triggerDialogOpen}
          onOpenChange={setTriggerDialogOpen}
          autopilotId={autopilotId}
        />
      )}
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
    </ObjectPage>
  );
}
