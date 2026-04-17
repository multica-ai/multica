"use client";

import { useState, useEffect } from "react";
import { Zap, Play, Clock, Plus, Trash2, CheckCircle2, XCircle, Loader2, Pencil } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { autopilotDetailOptions, autopilotRunsOptions } from "@multica/core/autopilots/queries";
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

function formatDate(date: string): string {
  return new Date(date).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

const RUN_STATUS_CONFIG: Record<string, { label: string; color: string; icon: typeof CheckCircle2 }> = {
  issue_created: { label: "任务已创建", color: "text-blue-500", icon: Clock },
  running: { label: "运行中", color: "text-blue-500", icon: Loader2 },
  completed: { label: "已完成", color: "text-emerald-500", icon: CheckCircle2 },
  failed: { label: "失败", color: "text-destructive", icon: XCircle },
};

function RunRow({ run }: { run: AutopilotRun }) {
  const wsPaths = useWorkspacePaths();
  const cfg = (RUN_STATUS_CONFIG[run.status] ?? RUN_STATUS_CONFIG["issue_created"])!;
  const StatusIcon = cfg.icon;

  const content = (
    <>
      <StatusIcon className={cn("h-4 w-4 shrink-0", cfg.color)} />
      <span className={cn("w-24 shrink-0 text-xs font-medium", cfg.color)}>{cfg.label}</span>
      <span className="w-16 shrink-0 text-xs text-muted-foreground capitalize">{run.source}</span>
      <span className="flex-1 min-w-0 text-xs text-muted-foreground truncate">
        {run.issue_id ? (
          "已关联任务"
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
            <span className="text-xs bg-muted px-1.5 py-0.5 rounded">已禁用</span>
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
            下次运行: {formatDate(trigger.next_run_at)}
          </div>
        )}
      </div>
      <Button
        size="icon"
        variant="ghost"
        className="h-7 w-7 shrink-0"
        onClick={() => {
          deleteTrigger.mutate({ autopilotId, triggerId: trigger.id });
          toast.success("触发器已删除");
        }}
      >
        <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
      </Button>
    </div>
  );
}

const PRIORITY_OPTIONS = [
  { value: "urgent", label: "紧急" },
  { value: "high", label: "高" },
  { value: "medium", label: "中" },
  { value: "low", label: "低" },
  { value: "none", label: "无" },
];

const EXECUTION_MODE_OPTIONS = [
  { value: "create_issue", label: "创建任务" },
  { value: "run_only", label: "仅运行" },
];

function EditAutopilotDialog({
  open,
  onOpenChange,
  autopilot,
  agents,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  autopilot: { id: string; title: string; description?: string | null; assignee_id: string; priority: string; execution_mode: string; issue_title_template?: string | null };
  agents: { id: string; name: string; archived_at?: string | null }[];
}) {
  const updateAutopilot = useUpdateAutopilot();
  const [title, setTitle] = useState(autopilot.title);
  const [description, setDescription] = useState(autopilot.description ?? "");
  const [assigneeId, setAssigneeId] = useState(autopilot.assignee_id);
  const [priority, setPriority] = useState(autopilot.priority);
  const [executionMode, setExecutionMode] = useState(autopilot.execution_mode);
  const [submitting, setSubmitting] = useState(false);

  const activeAgents = agents.filter((a) => !a.archived_at);

  // Sync form when autopilot data changes (e.g. after optimistic update)
  useEffect(() => {
    setTitle(autopilot.title);
    setDescription(autopilot.description ?? "");
    setAssigneeId(autopilot.assignee_id);
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
        priority,
        execution_mode: executionMode as "create_issue" | "run_only",
      });
      onOpenChange(false);
      toast.success("自动驾驶已更新");
    } catch {
      toast.error("更新自动驾驶失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogTitle>编辑自动驾驶</DialogTitle>
        <div className="space-y-4 pt-2">
          {/* Name */}
          <div>
            <label className="text-xs font-medium text-muted-foreground">名称</label>
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="例如：每日代码审查"
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
              autoFocus
            />
          </div>

          {/* Prompt */}
          <div>
            <label className="text-xs font-medium text-muted-foreground">提示词</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="为智能体提供逐步操作指引..."
              rows={6}
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring resize-y"
            />
          </div>

          {/* Agent + Priority */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">智能体</label>
              <Select value={assigneeId} onValueChange={(v) => v && setAssigneeId(v)}>
                <SelectTrigger className="mt-1 w-full">
                  <SelectValue>
                    {(value: string | null) => {
                      if (!value) return "选择智能体...";
                      const agent = activeAgents.find((a) => a.id === value);
                      return agent?.name ?? "未知智能体";
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
              <label className="text-xs font-medium text-muted-foreground">优先级</label>
              <Select value={priority} onValueChange={(v) => v && setPriority(v)}>
                <SelectTrigger className="mt-1 w-full">
                  <SelectValue>
                    {(value: string | null) => PRIORITY_OPTIONS.find((o) => o.value === value)?.label ?? "中"}
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

          {/* Execution Mode */}
          <div>
            <label className="text-xs font-medium text-muted-foreground">执行模式</label>
            <Select value={executionMode} onValueChange={(v) => v && setExecutionMode(v)}>
              <SelectTrigger className="mt-1 w-full">
                <SelectValue>
                  {(value: string | null) => EXECUTION_MODE_OPTIONS.find((o) => o.value === value)?.label ?? "创建任务"}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {EXECUTION_MODE_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-1">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button size="sm" onClick={handleSubmit} disabled={!title.trim() || !assigneeId || submitting}>
              {submitting ? "保存中..." : "保存"}
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
      toast.success("触发器已添加");
    } catch {
      toast.error("添加触发器失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogTitle>添加触发器</DialogTitle>
        <div className="space-y-4 pt-2">
          <TriggerConfigSection config={config} onChange={setConfig} />
          <div>
            <label className="text-xs font-medium text-muted-foreground">标签（可选）</label>
            <input
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="例如：工作日早晨"
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <div className="flex justify-end pt-1">
            <Button size="sm" onClick={handleSubmit} disabled={submitting}>
              {submitting ? "添加中..." : "添加触发器"}
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
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
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
        未找到自动驾驶
      </div>
    );
  }

  const { autopilot, triggers } = data;

  const handleRunNow = async () => {
    try {
      await triggerAutopilot.mutateAsync(autopilotId);
      toast.success("自动驾驶已触发");
    } catch (e: any) {
      toast.error(e?.message || "触发自动驾驶失败");
    }
  };

  const handleDelete = async () => {
    try {
      await deleteAutopilot.mutateAsync(autopilotId);
      toast.success("自动驾驶已删除");
      router.push(wsPaths.autopilots());
    } catch {
      toast.error("删除自动驾驶失败");
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
              aria-label={autopilot.status === "active" ? "暂停自动驾驶" : "激活自动驾驶"}
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
            编辑
          </Button>
          <Button size="sm" onClick={handleRunNow} disabled={autopilot.status !== "active" || triggerAutopilot.isPending}>
            <Play className="h-3.5 w-3.5 mr-1" />
            {triggerAutopilot.isPending ? "运行中..." : "立即运行"}
          </Button>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="max-w-4xl mx-auto p-6 space-y-8">
          {/* Properties */}
          <section className="space-y-4">
            <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">属性</h2>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <label className="text-xs text-muted-foreground">智能体</label>
                <div className="mt-1 flex items-center gap-2">
                  <ActorAvatar actorType="agent" actorId={autopilot.assignee_id} size={20} />
                  <span>{getActorName("agent", autopilot.assignee_id)}</span>
                </div>
              </div>
              <div>
                <label className="text-xs text-muted-foreground">优先级</label>
                <div className="mt-1 capitalize">{autopilot.priority}</div>
              </div>
              <div>
                <label className="text-xs text-muted-foreground">执行模式</label>
                <div className="mt-1">
                  {autopilot.execution_mode === "create_issue" ? "创建任务" : "仅运行"}
                </div>
              </div>
              {autopilot.description && (
                <div className="col-span-2">
                  <label className="text-xs text-muted-foreground">提示词</label>
                  <div className="mt-1 whitespace-pre-wrap text-sm">{autopilot.description}</div>
                </div>
              )}
            </div>
          </section>

          {/* Triggers */}
          <section className="space-y-3">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">触发器</h2>
              <Button size="sm" variant="outline" onClick={() => setTriggerDialogOpen(true)}>
                <Plus className="h-3.5 w-3.5 mr-1" />
                添加触发器
              </Button>
            </div>
            {triggers.length === 0 ? (
              <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
                尚未配置触发器。添加计划以自动运行。
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
            <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">运行历史</h2>
            {runsLoading ? (
              <div className="space-y-1">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : runs.length === 0 ? (
              <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
                暂无运行记录。点击「立即运行」手动触发。
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
            <h2 className="text-sm font-medium text-destructive uppercase tracking-wider">危险区域</h2>
            <Button size="sm" variant="destructive" onClick={handleDelete}>
              <Trash2 className="h-3.5 w-3.5 mr-1" />
              删除自动驾驶
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
      />
    </div>
  );
}
