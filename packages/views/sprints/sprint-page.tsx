"use client";

import { useState, useMemo } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Plus, Play, CheckCircle2, Archive, ChevronDown, ChevronRight,
  BarChart2, TrendingDown, Calendar, Target, ArrowLeft, MoreHorizontal, X,
} from "lucide-react";
import {
  BarChart, Bar, LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, ResponsiveContainer, Legend,
} from "recharts";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import {
  sprintListOptions,
  sprintIssuesOptions,
  backlogOptions,
  projectVelocityOptions,
  sprintBurndownOptions,
  useCreateSprint,
  useUpdateSprint,
  useStartSprint,
  useCompleteSprint,
  useAddTicketToSprint,
  useRemoveTicketFromSprint,
} from "@multica/core/sprints";
import type { Sprint, SprintIssue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Separator } from "@multica/ui/components/ui/separator";
import { cn } from "@multica/ui/lib/utils";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { api } from "@multica/core/api";

// ─── Helpers ────────────────────────────────────────────────────────────────

function statusColor(status: string) {
  switch (status) {
    case "done":
    case "cancelled":
      return "text-emerald-500";
    case "in_progress":
    case "in_review":
      return "text-blue-500";
    case "blocked":
      return "text-red-500";
    default:
      return "text-muted-foreground";
  }
}

function statusLabel(status: string) {
  return status.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

function sprintStateBadge(state: Sprint["state"]) {
  switch (state) {
    case "active":
      return <Badge variant="default" className="bg-blue-500/20 text-blue-600 dark:text-blue-400 border-blue-500/30">Active</Badge>;
    case "completed":
      return <Badge variant="default" className="bg-emerald-500/20 text-emerald-600 dark:text-emerald-400 border-emerald-500/30">Completed</Badge>;
    default:
      return <Badge variant="outline" className="text-muted-foreground">Planning</Badge>;
  }
}

// ─── Issue Row ──────────────────────────────────────────────────────────────

function IssueRow({
  issue,
  action,
  actionLabel,
  actionIcon,
}: {
  issue: SprintIssue;
  action?: () => void;
  actionLabel?: string;
  actionIcon?: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-3 py-2 px-3 rounded-md hover:bg-accent/50 transition-colors group">
      <span className={cn("w-2 h-2 rounded-full shrink-0", statusColor(issue.status).replace("text-", "bg-"))} />
      <span className="text-xs text-muted-foreground tabular-nums shrink-0 w-14">
        #{issue.number}
      </span>
      <span className="flex-1 text-sm truncate">{issue.title}</span>
      {issue.estimate != null && (
        <span className="text-xs text-muted-foreground tabular-nums shrink-0 bg-muted rounded px-1.5 py-0.5">
          {issue.estimate}
        </span>
      )}
      <span className={cn("text-xs shrink-0 hidden sm:block", statusColor(issue.status))}>
        {statusLabel(issue.status)}
      </span>
      {action && (
        <button
          type="button"
          onClick={action}
          title={actionLabel}
          className="opacity-0 group-hover:opacity-100 transition-opacity shrink-0 p-1 rounded hover:bg-accent"
        >
          {actionIcon}
        </button>
      )}
    </div>
  );
}

// ─── Create Sprint Dialog ───────────────────────────────────────────────────

function CreateSprintDialog({
  open,
  onOpenChange,
  projectId,
  wsId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectId: string;
  wsId: string;
}) {
  const [name, setName] = useState("");
  const [goal, setGoal] = useState("");
  const [startDate, setStartDate] = useState("");
  const [endDate, setEndDate] = useState("");
  const createSprint = useCreateSprint(wsId, projectId);

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    createSprint.mutate(
      {
        name: name.trim(),
        goal: goal.trim() || null,
        start_date: startDate ? new Date(startDate).toISOString() : null,
        end_date: endDate ? new Date(endDate).toISOString() : null,
      },
      {
        onSuccess: () => {
          toast.success("Sprint created");
          onOpenChange(false);
          setName(""); setGoal(""); setStartDate(""); setEndDate("");
        },
        onError: () => toast.error("Failed to create sprint"),
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create Sprint</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">Name</label>
            <Input
              placeholder="Sprint 1"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              autoFocus
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium text-muted-foreground">Goal <span className="font-normal">(optional)</span></label>
            <Textarea
              placeholder="What do you want to achieve?"
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
              rows={2}
              className="resize-none"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <label className="text-sm font-medium text-muted-foreground">Start date</label>
              <Input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <label className="text-sm font-medium text-muted-foreground">End date</label>
              <Input type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="submit" disabled={!name.trim() || createSprint.isPending}>
              {createSprint.isPending ? "Creating..." : "Create Sprint"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ─── Complete Sprint Dialog ─────────────────────────────────────────────────

function CompleteSprintDialog({
  sprint,
  sprints,
  open,
  onOpenChange,
  wsId,
  projectId,
}: {
  sprint: Sprint;
  sprints: Sprint[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  wsId: string;
  projectId: string;
}) {
  const [carryTo, setCarryTo] = useState("backlog");
  const completeSprint = useCompleteSprint(wsId, projectId);

  function handleComplete() {
    completeSprint.mutate(
      { sprintId: sprint.id, data: { carry_to: carryTo } },
      {
        onSuccess: () => { toast.success("Sprint completed"); onOpenChange(false); },
        onError: () => toast.error("Failed to complete sprint"),
      },
    );
  }

  const planningOrActive = sprints.filter(
    (s) => s.id !== sprint.id && s.state !== "completed",
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Complete Sprint</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Incomplete tickets will be moved to the selected destination.
          </p>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">Move incomplete tickets to</label>
            <Select value={carryTo} onValueChange={setCarryTo}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="backlog">Backlog</SelectItem>
                {planningOrActive.map((s) => (
                  <SelectItem key={s.id} value={s.id}>{s.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button onClick={handleComplete} disabled={completeSprint.isPending}>
            {completeSprint.isPending ? "Completing..." : "Complete Sprint"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── Sprint Card ─────────────────────────────────────────────────────────────

function SprintCard({
  sprint,
  sprints,
  wsId,
  projectId,
}: {
  sprint: Sprint;
  sprints: Sprint[];
  wsId: string;
  projectId: string;
}) {
  const [expanded, setExpanded] = useState(sprint.state === "active");
  const [completeDialogOpen, setCompleteDialogOpen] = useState(false);
  const { data: issues = [], isLoading } = useQuery({
    ...sprintIssuesOptions(wsId, sprint.id),
    enabled: expanded,
  });
  const startSprint = useStartSprint(wsId, projectId);
  const addTicket = useAddTicketToSprint(wsId, projectId);
  const removeTicket = useRemoveTicketFromSprint(wsId, projectId);
  const { data: backlog = [] } = useQuery(backlogOptions(wsId, projectId));

  const doneCount = issues.filter((i) => i.status === "done" || i.status === "cancelled").length;
  const totalCount = issues.length;
  const totalPoints = issues.reduce((sum, i) => sum + (i.estimate ?? 0), 0);

  return (
    <div className={cn(
      "rounded-lg border transition-colors",
      sprint.state === "active" && "border-blue-500/40 bg-blue-500/5 dark:bg-blue-500/10",
      sprint.state === "completed" && "opacity-60 hover:opacity-100",
    )}>
      {/* Header */}
      <div className="flex items-center gap-3 p-4 cursor-pointer" onClick={() => setExpanded(!expanded)}>
        <button type="button" className="text-muted-foreground shrink-0">
          {expanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
        </button>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-medium text-sm">{sprint.name}</span>
            {sprintStateBadge(sprint.state)}
          </div>
          {sprint.goal && (
            <p className="text-xs text-muted-foreground mt-0.5 truncate">{sprint.goal}</p>
          )}
        </div>

        {/* Stats */}
        <div className="hidden sm:flex items-center gap-4 shrink-0 mr-2">
          {totalPoints > 0 && (
            <span className="text-xs text-muted-foreground">{totalPoints} pts</span>
          )}
          {totalCount > 0 && (
            <span className="text-xs text-muted-foreground">
              {doneCount}/{totalCount}
            </span>
          )}
          {sprint.start_date && (
            <span className="text-xs text-muted-foreground hidden md:block">
              {new Date(sprint.start_date).toLocaleDateString()} –{" "}
              {sprint.end_date ? new Date(sprint.end_date).toLocaleDateString() : "?"}
            </span>
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1 shrink-0" onClick={(e) => e.stopPropagation()}>
          {sprint.state === "planning" && (
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              disabled={startSprint.isPending}
              onClick={() =>
                startSprint.mutate(sprint.id, {
                  onSuccess: () => toast.success("Sprint started"),
                  onError: (err) => toast.error(err instanceof Error ? err.message : "Failed to start sprint"),
                })
              }
            >
              <Play className="h-3 w-3 mr-1" />
              Start
            </Button>
          )}
          {sprint.state === "active" && (
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              onClick={() => setCompleteDialogOpen(true)}
            >
              <CheckCircle2 className="h-3 w-3 mr-1" />
              Complete
            </Button>
          )}
        </div>
      </div>

      {/* Progress bar */}
      {totalCount > 0 && (
        <div className="px-4 pb-2">
          <div className="h-1.5 rounded-full bg-muted overflow-hidden">
            <div
              className="h-full bg-emerald-500 transition-all"
              style={{ width: `${Math.round((doneCount / totalCount) * 100)}%` }}
            />
          </div>
        </div>
      )}

      {/* Issues */}
      {expanded && (
        <div className="border-t px-2 pb-2">
          {isLoading ? (
            <div className="py-4 space-y-2">
              {[1, 2, 3].map((n) => <Skeleton key={n} className="h-8 w-full" />)}
            </div>
          ) : issues.length === 0 ? (
            <div className="py-6 text-center text-sm text-muted-foreground">
              No tickets in this sprint.
              {sprint.state !== "completed" && backlog.length > 0 && (
                <p className="mt-1 text-xs">Drag tickets from the backlog below.</p>
              )}
            </div>
          ) : (
            <div className="mt-1 divide-y divide-border/50">
              {issues.map((issue) => (
                <IssueRow
                  key={issue.id}
                  issue={issue}
                  action={sprint.state !== "completed" ? () =>
                    removeTicket.mutate(
                      { sprintId: sprint.id, ticketId: issue.id },
                      { onError: () => toast.error("Failed to remove ticket") },
                    ) : undefined}
                  actionLabel="Remove from sprint"
                  actionIcon={<X className="h-3 w-3 text-muted-foreground" />}
                />
              ))}
            </div>
          )}

          {/* Add from backlog */}
          {sprint.state !== "completed" && backlog.length > 0 && (
            <div className="mt-2">
              <p className="px-3 text-xs text-muted-foreground mb-1">Add from backlog:</p>
              <div className="max-h-36 overflow-y-auto divide-y divide-border/50">
                {backlog.slice(0, 10).map((issue) => (
                  <IssueRow
                    key={issue.id}
                    issue={issue}
                    action={() =>
                      addTicket.mutate(
                        { sprintId: sprint.id, ticketId: issue.id },
                        { onError: () => toast.error("Failed to add ticket") },
                      )
                    }
                    actionLabel="Add to sprint"
                    actionIcon={<Plus className="h-3 w-3 text-muted-foreground" />}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      <CompleteSprintDialog
        sprint={sprint}
        sprints={sprints}
        open={completeDialogOpen}
        onOpenChange={setCompleteDialogOpen}
        wsId={wsId}
        projectId={projectId}
      />
    </div>
  );
}

// ─── Backlog Section ─────────────────────────────────────────────────────────

function BacklogSection({
  wsId,
  projectId,
  activeSprints,
}: {
  wsId: string;
  projectId: string;
  activeSprints: Sprint[];
}) {
  const [expanded, setExpanded] = useState(true);
  const { data: backlog = [], isLoading } = useQuery(backlogOptions(wsId, projectId));
  const addTicket = useAddTicketToSprint(wsId, projectId);

  const hasActiveSprint = activeSprints.some((s) => s.state === "active");
  const activeSprint = activeSprints.find((s) => s.state === "active");

  return (
    <div className="rounded-lg border">
      <div
        className="flex items-center gap-3 p-4 cursor-pointer"
        onClick={() => setExpanded(!expanded)}
      >
        <button type="button" className="text-muted-foreground shrink-0">
          {expanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
        </button>
        <div className="flex-1">
          <span className="font-medium text-sm">Backlog</span>
          <span className="ml-2 text-xs text-muted-foreground">({backlog.length})</span>
        </div>
      </div>

      {expanded && (
        <div className="border-t px-2 pb-2">
          {isLoading ? (
            <div className="py-4 space-y-2">
              {[1, 2, 3].map((n) => <Skeleton key={n} className="h-8 w-full" />)}
            </div>
          ) : backlog.length === 0 ? (
            <div className="py-6 text-center text-sm text-muted-foreground">
              No unsprinted tickets. All tickets are assigned to sprints.
            </div>
          ) : (
            <div className="mt-1 divide-y divide-border/50">
              {backlog.map((issue) => (
                <IssueRow
                  key={issue.id}
                  issue={issue}
                  action={hasActiveSprint && activeSprint ? () =>
                    addTicket.mutate(
                      { sprintId: activeSprint.id, ticketId: issue.id },
                      { onError: () => toast.error("Failed to add ticket to sprint") },
                    ) : undefined}
                  actionLabel="Add to active sprint"
                  actionIcon={<Plus className="h-3 w-3 text-muted-foreground" />}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Velocity Chart ──────────────────────────────────────────────────────────

function VelocityChart({ projectId, wsId }: { projectId: string; wsId: string }) {
  const { data: velocityData = [] } = useQuery(projectVelocityOptions(wsId, projectId));

  const chartData = velocityData.map((v) => ({
    name: v.sprint_name,
    completed: v.completed_points,
    total: v.total_points,
  }));

  if (chartData.length === 0) {
    return (
      <div className="flex items-center justify-center h-48 text-sm text-muted-foreground">
        No completed sprints yet.
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={240}>
      <BarChart data={chartData} margin={{ top: 8, right: 8, left: -24, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
        <XAxis dataKey="name" tick={{ fontSize: 11 }} className="text-muted-foreground" />
        <YAxis tick={{ fontSize: 11 }} className="text-muted-foreground" />
        <Tooltip
          contentStyle={{ fontSize: 12, borderRadius: 8 }}
          labelClassName="font-medium"
        />
        <Legend wrapperStyle={{ fontSize: 12 }} />
        <Bar dataKey="completed" name="Completed pts" fill="hsl(var(--chart-1))" radius={[4, 4, 0, 0]} />
        <Bar dataKey="total" name="Total pts" fill="hsl(var(--chart-2))" radius={[4, 4, 0, 0]} />
      </BarChart>
    </ResponsiveContainer>
  );
}

// ─── Burndown Chart ──────────────────────────────────────────────────────────

function BurndownChart({ sprintId, sprint, wsId }: { sprintId: string; sprint: Sprint; wsId: string }) {
  const { data: burndownIssues = [] } = useQuery(sprintBurndownOptions(wsId, sprintId));

  // Build daily burndown from sprint start to end
  const chartData = useMemo(() => {
    if (!sprint.start_date || !sprint.end_date) return [];

    const start = new Date(sprint.start_date);
    const end = new Date(sprint.end_date);
    const totalDays = Math.max(1, Math.ceil((end.getTime() - start.getTime()) / 86400000));
    const totalPoints = burndownIssues.reduce((sum, i) => sum + (i.estimate ?? 0), 0);

    const days: { date: string; remaining: number; ideal: number }[] = [];

    for (let d = 0; d <= totalDays; d++) {
      const dayDate = new Date(start.getTime() + d * 86400000);
      const dateStr = dayDate.toLocaleDateString(undefined, { month: "short", day: "numeric" });

      // Count remaining = total - done issues updated before this day
      const completedByThisDay = burndownIssues
        .filter(
          (i) =>
            (i.status === "done" || i.status === "cancelled") &&
            new Date(i.updated_at) <= dayDate,
        )
        .reduce((sum, i) => sum + (i.estimate ?? 0), 0);

      days.push({
        date: dateStr,
        remaining: Math.max(0, totalPoints - completedByThisDay),
        ideal: Math.round(totalPoints * (1 - d / totalDays)),
      });
    }
    return days;
  }, [burndownIssues, sprint]);

  if (chartData.length === 0) {
    return (
      <div className="flex items-center justify-center h-48 text-sm text-muted-foreground">
        Set sprint start/end dates to see the burndown.
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={240}>
      <LineChart data={chartData} margin={{ top: 8, right: 8, left: -24, bottom: 0 }}>
        <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
        <XAxis dataKey="date" tick={{ fontSize: 11 }} className="text-muted-foreground" />
        <YAxis tick={{ fontSize: 11 }} className="text-muted-foreground" />
        <Tooltip contentStyle={{ fontSize: 12, borderRadius: 8 }} labelClassName="font-medium" />
        <Legend wrapperStyle={{ fontSize: 12 }} />
        <Line
          type="monotone"
          dataKey="remaining"
          name="Remaining"
          stroke="hsl(var(--chart-1))"
          strokeWidth={2}
          dot={false}
        />
        <Line
          type="monotone"
          dataKey="ideal"
          name="Ideal"
          stroke="hsl(var(--chart-2))"
          strokeWidth={2}
          strokeDasharray="4 4"
          dot={false}
        />
      </LineChart>
    </ResponsiveContainer>
  );
}

// ─── Charts Section ──────────────────────────────────────────────────────────

function ChartsSection({
  wsId,
  projectId,
  activeSprint,
}: {
  wsId: string;
  projectId: string;
  activeSprint: Sprint | undefined;
}) {
  const [activeTab, setActiveTab] = useState<"velocity" | "burndown">("velocity");

  return (
    <div className="rounded-lg border overflow-hidden">
      <div className="flex items-center gap-1 p-3 border-b bg-muted/30">
        <button
          type="button"
          onClick={() => setActiveTab("velocity")}
          className={cn(
            "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm transition-colors",
            activeTab === "velocity"
              ? "bg-background shadow-sm font-medium"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          <BarChart2 className="h-3.5 w-3.5" />
          Velocity
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("burndown")}
          className={cn(
            "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm transition-colors",
            activeTab === "burndown"
              ? "bg-background shadow-sm font-medium"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          <TrendingDown className="h-3.5 w-3.5" />
          Burndown
        </button>
      </div>

      <div className="p-4">
        {activeTab === "velocity" ? (
          <>
            <p className="text-xs text-muted-foreground mb-3">Completed vs. total story points per sprint</p>
            <VelocityChart projectId={projectId} wsId={wsId} />
          </>
        ) : activeSprint ? (
          <>
            <p className="text-xs text-muted-foreground mb-3">Remaining work for active sprint</p>
            <BurndownChart sprintId={activeSprint.id} sprint={activeSprint} wsId={wsId} />
          </>
        ) : (
          <div className="flex items-center justify-center h-48 text-sm text-muted-foreground">
            No active sprint. Start a sprint to see the burndown chart.
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Main Page ───────────────────────────────────────────────────────────────

export function SprintsPage({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const [createDialogOpen, setCreateDialogOpen] = useState(false);

  const { data: sprints = [], isLoading } = useQuery(sprintListOptions(wsId, projectId));

  const activeSprint = sprints.find((s) => s.state === "active");
  const planningSprints = sprints.filter((s) => s.state === "planning");
  const completedSprints = sprints.filter((s) => s.state === "completed");

  if (isLoading) {
    return (
      <div className="flex flex-col h-full">
        <div className="px-6 py-4 border-b">
          <Skeleton className="h-5 w-48" />
        </div>
        <div className="flex-1 overflow-y-auto px-6 py-4 space-y-3">
          {[1, 2, 3].map((n) => <Skeleton key={n} className="h-20 w-full rounded-lg" />)}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      <BreadcrumbHeader
        segments={[{ href: wsPaths.projects(), label: "Projects" }]}
        leaf={<span className="font-medium">Sprints</span>}
        actions={
          <Button size="sm" onClick={() => setCreateDialogOpen(true)}>
            <Plus className="h-3.5 w-3.5 mr-1.5" />
            <span className="hidden sm:inline">New Sprint</span>
          </Button>
        }
      />

      <div className="flex-1 overflow-y-auto">
        <div className="max-w-4xl mx-auto px-4 sm:px-6 py-6 space-y-6">

          {/* Active Sprint */}
          {activeSprint && (
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <span className="h-2 w-2 rounded-full bg-blue-500 inline-block" />
                Active Sprint
              </h2>
              <SprintCard
                sprint={activeSprint}
                sprints={sprints}
                wsId={wsId}
                projectId={projectId}
              />
            </section>
          )}

          {/* Planning Sprints */}
          {planningSprints.length > 0 && (
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
                Planning
              </h2>
              <div className="space-y-3">
                {planningSprints.map((s) => (
                  <SprintCard key={s.id} sprint={s} sprints={sprints} wsId={wsId} projectId={projectId} />
                ))}
              </div>
            </section>
          )}

          {/* Backlog */}
          <section>
            <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
              Backlog
            </h2>
            <BacklogSection wsId={wsId} projectId={projectId} activeSprints={sprints} />
          </section>

          {/* Charts */}
          <section>
            <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
              Analytics
            </h2>
            <ChartsSection wsId={wsId} projectId={projectId} activeSprint={activeSprint} />
          </section>

          {/* Completed Sprints */}
          {completedSprints.length > 0 && (
            <section>
              <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
                <Archive className="h-3.5 w-3.5" />
                Completed ({completedSprints.length})
              </h2>
              <div className="space-y-2">
                {completedSprints.map((s) => (
                  <SprintCard key={s.id} sprint={s} sprints={sprints} wsId={wsId} projectId={projectId} />
                ))}
              </div>
            </section>
          )}

          {/* Empty state */}
          {sprints.length === 0 && (
            <div className="text-center py-20">
              <Target className="h-12 w-12 text-muted-foreground/40 mx-auto mb-4" />
              <h3 className="text-sm font-medium mb-1">No sprints yet</h3>
              <p className="text-sm text-muted-foreground mb-4">
                Create your first sprint to start planning and tracking work.
              </p>
              <Button onClick={() => setCreateDialogOpen(true)}>
                <Plus className="h-4 w-4 mr-2" />
                Create Sprint
              </Button>
            </div>
          )}
        </div>
      </div>

      <CreateSprintDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        projectId={projectId}
        wsId={wsId}
      />
    </div>
  );
}
