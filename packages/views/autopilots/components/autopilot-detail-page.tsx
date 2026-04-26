"use client";

import { useState, useEffect, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Zap, Play, Clock, Plus, Trash2, CheckCircle2, XCircle, Loader2, Pencil, FileText, Square } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { autopilotDetailOptions, autopilotRunsOptions, autopilotKeys } from "@multica/core/autopilots/queries";
import { projectListOptions, isOpenProject } from "@multica/core/projects/queries";
import {
  useUpdateAutopilot,
  useDeleteAutopilot,
  useTriggerAutopilot,
  useCreateAutopilotTrigger,
  useDeleteAutopilotTrigger,
} from "@multica/core/autopilots/mutations";
import { agentListOptions } from "@multica/core/workspace/queries";
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
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import {
  TriggerConfigSection,
  getDefaultTriggerConfig,
  toCronExpression,
} from "./trigger-config";
import type { TriggerConfig } from "./trigger-config";
import type { AutopilotRun, AutopilotTrigger } from "@multica/core/types";
import { api } from "@multica/core/api";
import type { TaskMessagePayload } from "@multica/core/types/events";
import type { AgentTask } from "@multica/core/types/agent";
import { AgentTranscriptDialog } from "../../issues/components/agent-transcript-dialog";

interface TimelineItem {
  seq: number;
  type: "tool_use" | "tool_result" | "thinking" | "text" | "error";
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
}

function buildTimeline(msgs: TaskMessagePayload[]): TimelineItem[] {
  return msgs
    .map((m) => ({ seq: m.seq, type: m.type, tool: m.tool, content: m.content, input: m.input, output: m.output }))
    .sort((a, b) => a.seq - b.seq);
}

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

function RunRow({ run, agentId, autopilotId }: { run: AutopilotRun; agentId: string; autopilotId: string }) {
  const wsPaths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const queryClient = useQueryClient();
  const cfg = (RUN_STATUS_CONFIG[run.status] ?? RUN_STATUS_CONFIG["issue_created"])!;
  const StatusIcon = cfg.icon;
  const [logOpen, setLogOpen] = useState(false);
  const [stopping, setStopping] = useState(false);

  const { data: rawMessages, isFetching: logLoading } = useQuery({
    queryKey: ["task-messages", run.task_id ?? ""],
    queryFn: () => api.listTaskMessages(run.task_id!),
    enabled: logOpen && !!run.task_id,
    staleTime: Infinity,
  });
  const logItems = rawMessages ? buildTimeline(rawMessages) : null;

  const isLive = run.status === "running";

  const handleStop = useCallback(async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!run.task_id || stopping) return;
    setStopping(true);
    try {
      await api.cancelTaskById(run.task_id);
      await queryClient.invalidateQueries({ queryKey: autopilotKeys.runs(wsId, autopilotId) });
      toast.success("Run cancelled");
    } catch {
      toast.error("Failed to cancel run");
    } finally {
      setStopping(false);
    }
  }, [run.task_id, stopping, queryClient, wsId, autopilotId]);

  const handleViewLog = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!run.task_id) return;
    setLogOpen(true);
  }, [run.task_id]);

  const taskStatus: AgentTask["status"] =
    run.status === "completed" ? "completed" :
    run.status === "failed" ? "failed" :
    run.status === "running" ? "running" : "completed";

  const syntheticTask: AgentTask = {
    id: run.task_id ?? "",
    agent_id: agentId,
    runtime_id: "",
    issue_id: "",
    status: taskStatus,
    priority: 0,
    dispatched_at: run.triggered_at,
    started_at: run.triggered_at,
    completed_at: run.completed_at,
    result: run.result,
    error: run.failure_reason,
    created_at: run.triggered_at,
  };

  const actions = (
    <div className="flex items-center gap-1 shrink-0">
      {run.task_id && run.status === "running" && (
        <button
          onClick={handleStop}
          disabled={stopping}
          className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-destructive hover:text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
          title="Stop this run"
        >
          {stopping ? <Loader2 className="h-3 w-3 animate-spin" /> : <Square className="h-3 w-3" />}
          <span>Stop</span>
        </button>
      )}
      {run.task_id && (
        <button
          onClick={handleViewLog}
          disabled={logLoading && !logItems}
          className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors disabled:opacity-50"
          title="View execution log"
        >
          {logLoading && !logItems ? <Loader2 className="h-3 w-3 animate-spin" /> : <FileText className="h-3 w-3" />}
          <span>Log</span>
        </button>
      )}
    </div>
  );

  const content = (
    <div className="flex flex-col gap-0.5 min-w-0 flex-1 sm:flex-row sm:items-center sm:gap-3">
      {/* Top row (always visible): status icon + label + actions */}
      <div className="flex items-center gap-2 min-w-0">
        <StatusIcon className={cn("h-4 w-4 shrink-0", cfg.color, cfg.spin && "animate-spin")} />
        <span className={cn("shrink-0 text-xs font-medium", cfg.color)}>{cfg.label}</span>
        <span className="hidden sm:inline shrink-0 text-xs text-muted-foreground capitalize">{run.source}</span>
        <span className="flex-1 min-w-0 text-xs text-muted-foreground truncate">
          {run.issue_id ? (
            "Issue linked"
          ) : run.failure_reason ? (
            <span className="text-destructive truncate">{run.failure_reason}</span>
          ) : null}
        </span>
        <div className="sm:hidden">{actions}</div>
      </div>
      {/* Bottom row on mobile: source + date */}
      <div className="flex items-center justify-between gap-2 sm:contents">
        <span className="sm:hidden text-xs text-muted-foreground capitalize">{run.source}</span>
        <span className="sm:ml-auto shrink-0 text-xs text-muted-foreground tabular-nums">
          {formatDate(run.triggered_at || run.created_at)}
        </span>
        <div className="hidden sm:flex">{actions}</div>
      </div>
    </div>
  );

  const rowClass = "flex items-start gap-3 px-4 py-2.5 text-sm hover:bg-accent/30 transition-colors";

  return (
    <>
      {run.issue_id ? (
        <AppLink href={wsPaths.issueDetail(run.issue_id)} className={cn(rowClass, "cursor-pointer")}>
          {content}
        </AppLink>
      ) : (
        <div className={rowClass}>{content}</div>
      )}
      {logOpen && logItems !== null && (
        <AgentTranscriptDialog
          open={logOpen}
          onOpenChange={setLogOpen}
          task={syntheticTask}
          items={logItems}
          agentName={getActorName("agent", agentId)}
          isLive={isLive}
        />
      )}
    </>
  );
}

function TriggerRow({ trigger, autopilotId }: { trigger: AutopilotTrigger; autopilotId: string }) {
  const deleteTrigger = useDeleteAutopilotTrigger();

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
        onClick={() => {
          deleteTrigger.mutate({ autopilotId, triggerId: trigger.id });
          toast.success("Trigger deleted");
        }}
      >
        <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
      </Button>
    </div>
  );
}

const PRIORITY_OPTIONS = [
  { value: "urgent", label: "Urgent" },
  { value: "high", label: "High" },
  { value: "medium", label: "Medium" },
  { value: "low", label: "Low" },
  { value: "none", label: "None" },
];

const EXECUTION_MODE_OPTIONS = [
  { value: "create_issue", label: "Create Issue" },
  { value: "run_only", label: "Run Only" },
];

function EditAutopilotDialog({
  open,
  onOpenChange,
  autopilot,
  agents,
  projects,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  autopilot: { id: string; title: string; description?: string | null; assignee_id: string; project_id?: string | null; priority: string; execution_mode: string; issue_title_template?: string | null };
  agents: { id: string; name: string; archived_at?: string | null }[];
  projects: { id: string; title: string }[];
}) {
  const updateAutopilot = useUpdateAutopilot();
  const [title, setTitle] = useState(autopilot.title);
  const [description, setDescription] = useState(autopilot.description ?? "");
  const [assigneeId, setAssigneeId] = useState(autopilot.assignee_id);
  const [projectId, setProjectId] = useState(autopilot.project_id ?? null);
  const [priority, setPriority] = useState(autopilot.priority);
  const [executionMode, setExecutionMode] = useState(autopilot.execution_mode);
  const [submitting, setSubmitting] = useState(false);

  const activeAgents = agents.filter((a) => !a.archived_at);

  // Sync form when autopilot data changes (e.g. after optimistic update)
  useEffect(() => {
    setTitle(autopilot.title);
    setDescription(autopilot.description ?? "");
    setAssigneeId(autopilot.assignee_id);
    setProjectId(autopilot.project_id ?? null);
    setPriority(autopilot.priority);
    setExecutionMode(autopilot.execution_mode);
  }, [autopilot]);

  const handleSubmit = async () => {
    if (!title.trim() || !assigneeId || submitting) return;
    setSubmitting(true);
    try {
      await updateAutopilot.mutateAsync({
        id: autopilot.id,
        title: title.trim(),
        description: description.trim() || null,
        assignee_id: assigneeId,
        project_id: projectId || null,
        priority,
        execution_mode: executionMode as "create_issue" | "run_only",
      });
      onOpenChange(false);
      toast.success("Autopilot updated");
    } catch {
      toast.error("Failed to update autopilot");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogTitle>Edit Autopilot</DialogTitle>
        <div className="space-y-4 pt-2">
          {/* Name */}
          <div>
            <label className="text-xs font-medium text-muted-foreground">Name</label>
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="e.g. Daily code review"
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
              autoFocus
            />
          </div>

          {/* Prompt */}
          <div>
            <label className="text-xs font-medium text-muted-foreground">Prompt</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Step-by-step instructions for the agent..."
              rows={6}
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring resize-y"
            />
          </div>

          {/* Agent + Priority */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">Agent</label>
              <Select value={assigneeId} onValueChange={(v) => v && setAssigneeId(v)}>
                <SelectTrigger className="mt-1 w-full">
                  <SelectValue>
                    {(value: string | null) => {
                      if (!value) return "Select agent...";
                      const agent = activeAgents.find((a) => a.id === value);
                      return agent?.name ?? "Unknown Agent";
                    }}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {activeAgents.map((a) => (
                    <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">Priority</label>
              <Select value={priority} onValueChange={(v) => v && setPriority(v)}>
                <SelectTrigger className="mt-1 w-full">
                  <SelectValue>
                    {(value: string | null) => PRIORITY_OPTIONS.find((o) => o.value === value)?.label ?? "Medium"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {PRIORITY_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          {/* Execution Mode + Project */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">Execution Mode</label>
              <Select value={executionMode} onValueChange={(v) => v && setExecutionMode(v)}>
                <SelectTrigger className="mt-1 w-full">
                  <SelectValue>
                    {(value: string | null) => EXECUTION_MODE_OPTIONS.find((o) => o.value === value)?.label ?? "Create Issue"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {EXECUTION_MODE_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">Project</label>
              <Select value={projectId || "none"} onValueChange={(v) => setProjectId(v === "none" ? null : v)}>
                <SelectTrigger className="mt-1 w-full">
                  <SelectValue>
                    {(value: string | null) => {
                      if (!value || value === "none") return "No project";
                      const project = projects.find((p) => p.id === value);
                      return project?.title ?? "Unknown Project";
                    }}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">No project</SelectItem>
                  {projects.map((p) => (
                    <SelectItem key={p.id} value={p.id}>{p.title}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-1">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button size="sm" onClick={handleSubmit} disabled={!title.trim() || !assigneeId || submitting}>
              {submitting ? "Saving..." : "Save"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
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
  const { data: runs = [], isLoading: runsLoading } = useQuery({
    ...autopilotRunsOptions(wsId, autopilotId),
    refetchInterval: (query): number | false => {
      const raw = query.state.data as { runs?: AutopilotRun[] } | undefined;
      return raw?.runs?.some((r) => r.status === "running") ? 5000 : false;
    },
  });
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const updateAutopilot = useUpdateAutopilot();
  const deleteAutopilot = useDeleteAutopilot();
  const triggerAutopilot = useTriggerAutopilot();

  const [triggerDialogOpen, setTriggerDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);

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

  const handleRunNow = async () => {
    try {
      await triggerAutopilot.mutateAsync(autopilotId);
      toast.success("Autopilot triggered");
    } catch (e: any) {
      toast.error(e?.message || "Failed to trigger autopilot");
    }
  };

  const handleDelete = async () => {
    try {
      await deleteAutopilot.mutateAsync(autopilotId);
      toast.success("Autopilot deleted");
      router.push(wsPaths.autopilots());
    } catch {
      toast.error("Failed to delete autopilot");
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
                  <ActorAvatar actorType="agent" actorId={autopilot.assignee_id} size={20} />
                  <span>{getActorName("agent", autopilot.assignee_id)}</span>
                </div>
              </div>
              <div>
                <label className="text-xs text-muted-foreground">Priority</label>
                <div className="mt-1 capitalize">{autopilot.priority}</div>
              </div>
              <div>
                <label className="text-xs text-muted-foreground">Execution Mode</label>
                <div className="mt-1">
                  {autopilot.execution_mode === "create_issue" ? "Create Issue" : "Run Only"}
                </div>
              </div>
              <div>
                <label className="text-xs text-muted-foreground">Project</label>
                <div className="mt-1">
                  {autopilot.project_id ? projects.find((p) => p.id === autopilot.project_id)?.title ?? "Unknown Project" : "None"}
                </div>
              </div>
              {autopilot.description && (
                <div className="col-span-2">
                  <label className="text-xs text-muted-foreground">Prompt</label>
                  <div className="mt-1 whitespace-pre-wrap text-sm">{autopilot.description}</div>
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
                  <RunRow key={run.id} run={run} agentId={autopilot.assignee_id} autopilotId={autopilotId} />
                ))}
              </div>
            )}
          </section>

          {/* Danger zone */}
          <section className="space-y-3 pt-4 border-t">
            <h2 className="text-sm font-medium text-destructive uppercase tracking-wider">Danger Zone</h2>
            <Button size="sm" variant="destructive" onClick={handleDelete}>
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
      <EditAutopilotDialog
        open={editDialogOpen}
        onOpenChange={setEditDialogOpen}
        autopilot={autopilot}
        agents={agents}
        projects={projects.filter(isOpenProject)}
      />
    </div>
  );
}
