"use client";

import { useState } from "react";
import { CheckCircle2, Clock, Copy, Loader2, Pencil, Play, Plus, Trash2, XCircle, Zap } from "lucide-react";
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
import { ReadonlyContent } from "../../editor";
import { AutopilotDialog } from "./autopilot-dialog";
import { buildAutopilotDuplicatePreset } from "./autopilot-duplicate";

function formatDate(date: string): string {
  return new Date(date).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

const RUN_STATUS_CONFIG: Record<string, { label: string; color: string; icon: typeof CheckCircle2; spin?: boolean }> = {
  issue_created: { label: "Issue Created", color: "text-blue-500", icon: Clock },
  running: { label: "Running", color: "text-blue-500", icon: Loader2, spin: true },
  completed: { label: "Completed", color: "text-emerald-500", icon: CheckCircle2 },
  failed: { label: "Failed", color: "text-destructive", icon: XCircle },
};

function RunRow({ run }: { run: AutopilotRun }) {
  const wsPaths = useWorkspacePaths();
  const cfg = (RUN_STATUS_CONFIG[run.status] ?? RUN_STATUS_CONFIG["issue_created"])!;
  const StatusIcon = cfg.icon;

  const content = (
    <>
      <StatusIcon className={cn("h-4 w-4 shrink-0", cfg.color, cfg.spin && "animate-spin")} />
      <span className={cn("w-24 shrink-0 text-xs font-medium", cfg.color)}>{cfg.label}</span>
      <span className="w-16 shrink-0 text-xs text-muted-foreground capitalize">{run.source}</span>
      <span className="flex-1 min-w-0 text-xs text-muted-foreground truncate">
        {run.issue_id ? (
          "Issue linked"
        ) : run.failure_reason ? (
          <span className="text-destructive">{run.failure_reason}</span>
        ) : null}
      </span>
      <span className="w-32 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
        {formatDate(run.triggered_at || run.created_at)}
      </span>
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

function TriggerRow({ trigger, autopilotId }: { trigger: AutopilotTrigger; autopilotId: string }) {
  const deleteTrigger = useDeleteAutopilotTrigger();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteTrigger.mutateAsync({ autopilotId, triggerId: trigger.id });
      toast.success("Trigger deleted");
      setConfirmOpen(false);
    } catch {
      toast.error("Failed to delete trigger");
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
            <span className="text-xs bg-muted px-1.5 py-0.5 rounded">Disabled</span>
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
            Next: {formatDate(trigger.next_run_at)}
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
            <AlertDialogTitle>Delete trigger</AlertDialogTitle>
            <AlertDialogDescription>
              This trigger will be removed and the autopilot will stop firing on this schedule. This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              disabled={deleting}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {deleting ? "Deleting..." : "Delete"}
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
      toast.success("Trigger added");
    } catch {
      toast.error("Failed to add trigger");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogTitle>Add Trigger</DialogTitle>
        <div className="space-y-4 pt-2">
          <TriggerConfigSection config={config} onChange={setConfig} />
          <div>
            <label className="text-xs font-medium text-muted-foreground">Label (optional)</label>
            <input
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="e.g. Weekday morning"
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <div className="flex justify-end pt-1">
            <Button size="sm" onClick={handleSubmit} disabled={submitting}>
              {submitting ? "Adding..." : "Add trigger"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function AutopilotDetailPage({ autopilotId }: { autopilotId: string }) {
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
  const [duplicateDialogOpen, setDuplicateDialogOpen] = useState(false);
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
        Autopilot not found
      </div>
    );
  }

  const { autopilot, triggers } = data;
  const duplicatePreset = buildAutopilotDuplicatePreset(autopilot, triggers);

  const handleRunNow = async () => {
    try {
      await triggerAutopilot.mutateAsync(autopilotId);
      toast.success("Autopilot triggered");
    } catch (e: any) {
      toast.error(e?.message || "Failed to trigger autopilot");
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteAutopilot.mutateAsync(autopilotId);
      toast.success("Autopilot deleted");
      router.push(wsPaths.autopilots());
    } catch {
      toast.error("Failed to delete autopilot");
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
              aria-label={autopilot.status === "active" ? "Pause autopilot" : "Activate autopilot"}
            />
            <span className={cn(
              "text-xs font-medium capitalize",
              autopilot.status === "active" ? "text-emerald-500" :
              autopilot.status === "paused" ? "text-amber-500" :
              "text-muted-foreground",
            )}>
              {autopilot.status}
            </span>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => setDuplicateDialogOpen(true)}>
            <Copy className="h-3.5 w-3.5 mr-1" />
            Duplicate
          </Button>
          <Button size="sm" variant="outline" onClick={() => setEditDialogOpen(true)}>
            <Pencil className="h-3.5 w-3.5 mr-1" />
            Edit
          </Button>
          <Button size="sm" onClick={handleRunNow} disabled={autopilot.status !== "active" || triggerAutopilot.isPending}>
            <Play className="h-3.5 w-3.5 mr-1" />
            {triggerAutopilot.isPending ? "Running..." : "Run now"}
          </Button>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="max-w-4xl mx-auto p-6 space-y-8">
          {/* Properties */}
          <section className="space-y-4">
            <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">Properties</h2>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <label className="text-xs text-muted-foreground">Agent</label>
                <div className="mt-1 flex items-center gap-2">
                  <ActorAvatar actorType="agent" actorId={autopilot.assignee_id} size={20} enableHoverCard showStatusDot />
                  <span className="cursor-pointer">{getActorName("agent", autopilot.assignee_id)}</span>
                </div>
              </div>
              <div>
                <label className="text-xs text-muted-foreground">Output Mode</label>
                <div className="mt-1">
                  {autopilot.execution_mode === "create_issue" ? "Create Issue" : "Run Only"}
                </div>
              </div>
              {autopilot.description && (
                <div className="col-span-2">
                  <label className="text-xs text-muted-foreground">Prompt</label>
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
              <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">Triggers</h2>
              <Button size="sm" variant="outline" onClick={() => setTriggerDialogOpen(true)}>
                <Plus className="h-3.5 w-3.5 mr-1" />
                Add trigger
              </Button>
            </div>
            {triggers.length === 0 ? (
              <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
                No triggers configured. Add a schedule to run automatically.
              </div>
            ) : (
              <div className="space-y-2">
                {triggers.map((t) => (
                  <TriggerRow key={t.id} trigger={t} autopilotId={autopilotId} />
                ))}
              </div>
            )}
          </section>

          {/* Run History */}
          <section className="space-y-3">
            <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">Run History</h2>
            {runsLoading ? (
              <div className="space-y-1">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : runs.length === 0 ? (
              <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
                No runs yet. Click &quot;Run now&quot; to trigger manually.
              </div>
            ) : (
              <div className="rounded-md border overflow-hidden">
                {runs.map((run) => (
                  <RunRow key={run.id} run={run} />
                ))}
              </div>
            )}
          </section>

          {/* Danger zone */}
          <section className="space-y-3 pt-4 border-t">
            <h2 className="text-sm font-medium text-destructive uppercase tracking-wider">Danger Zone</h2>
            <Button size="sm" variant="destructive" onClick={() => setDeleteConfirmOpen(true)}>
              <Trash2 className="h-3.5 w-3.5 mr-1" />
              Delete autopilot
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
      {duplicateDialogOpen && (
        <AutopilotDialog
          mode="create"
          open={duplicateDialogOpen}
          onOpenChange={setDuplicateDialogOpen}
          initial={duplicatePreset.initial}
          initialTriggerConfig={duplicatePreset.initialTriggerConfig}
          intent="duplicate"
        />
      )}
      <AlertDialog
        open={deleteConfirmOpen}
        onOpenChange={(v) => { if (!v && !deleting) setDeleteConfirmOpen(false); }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete autopilot</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently delete &ldquo;{autopilot.title}&rdquo;, along with its triggers and run history. This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              disabled={deleting}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {deleting ? "Deleting..." : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
