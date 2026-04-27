"use client";

import { useState } from "react";
import { Plus, Zap, Play, Pause, AlertCircle, Newspaper, GitPullRequest, Bug, BarChart3, Shield, FileSearch, Loader2, CheckCircle2, XCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { projectListOpenOptions } from "@multica/core/projects/queries";
import { useCreateAutopilot, useCreateAutopilotTrigger, useTriggerAutopilot } from "@multica/core/autopilots/mutations";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
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
import type { Autopilot } from "@multica/core/types";
import type { TriggerFrequency } from "./trigger-config";

interface AutopilotTemplate {
  title: string;
  prompt: string;
  summary: string;
  icon: typeof Zap;
  frequency: TriggerFrequency;
  time: string;
}

const TEMPLATES: AutopilotTemplate[] = [
  {
    title: "Daily news digest",
    summary: "Search and summarize today's news for the team",
    prompt: `1. Search the web for news and announcements published today only (strictly today's date)
2. Filter for topics relevant to our team and industry
3. For each item, write a short summary including: title, source, key takeaways
4. Compile everything into a single digest post
5. Post the digest as a comment on this issue and @mention all workspace members`,
    icon: Newspaper,
    frequency: "daily",
    time: "09:00",
  },
  {
    title: "PR review reminder",
    summary: "Flag stale pull requests that need review",
    prompt: `1. List all open pull requests in the repository
2. Identify PRs that have been open for more than 24 hours without a review
3. For each stale PR, note the author, age, and a one-line summary of the change
4. Post a comment on this issue listing all stale PRs with links
5. @mention the team to remind them to review`,
    icon: GitPullRequest,
    frequency: "weekdays",
    time: "10:00",
  },
  {
    title: "Bug triage",
    summary: "Assess and prioritize new bug reports",
    prompt: `1. List all issues with status "triage" or "backlog" that have not been prioritized
2. For each issue, read the description and any attached logs or screenshots
3. Assess severity (critical / high / medium / low) based on user impact and scope
4. Set the priority field on the issue accordingly
5. Add a comment explaining your assessment and suggested next steps`,
    icon: Bug,
    frequency: "weekdays",
    time: "09:00",
  },
  {
    title: "Weekly progress report",
    summary: "Compile a weekly summary of team progress",
    prompt: `1. Gather all issues completed (status "done") in the past 7 days
2. Gather all issues currently in progress
3. Identify any blocked issues and their blockers
4. Calculate key metrics: issues closed, issues opened, net change
5. Write a structured weekly report with sections: Completed, In Progress, Blocked, Metrics
6. Post the report as a comment on this issue`,
    icon: BarChart3,
    frequency: "weekly",
    time: "17:00",
  },
  {
    title: "Dependency audit",
    summary: "Scan for security vulnerabilities and outdated packages",
    prompt: `1. Run dependency audit tools on the project (npm audit, go vuln check, etc.)
2. Identify any packages with known security vulnerabilities
3. List outdated packages that are more than 2 major versions behind
4. For each finding, note the severity, affected package, and recommended fix
5. Post a summary report as a comment with actionable items`,
    icon: Shield,
    frequency: "weekly",
    time: "08:00",
  },
  {
    title: "Documentation check",
    summary: "Review recent changes for documentation gaps",
    prompt: `1. List all code changes merged in the past 7 days (via git log)
2. For each significant change, check if related documentation was updated
3. Identify any new APIs, config options, or features missing documentation
4. Create a list of documentation gaps with file paths and suggested content
5. Post the findings as a comment on this issue`,
    icon: FileSearch,
    frequency: "weekly",
    time: "14:00",
  },
];

function formatRelativeDate(date: string): string {
  const diff = Date.now() - new Date(date).getTime();
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days < 1) return "Today";
  if (days === 1) return "1d ago";
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}

const STATUS_CONFIG: Record<string, { label: string; color: string; icon: typeof Zap }> = {
  active: { label: "Active", color: "text-emerald-500", icon: Play },
  paused: { label: "Paused", color: "text-amber-500", icon: Pause },
  archived: { label: "Archived", color: "text-muted-foreground", icon: AlertCircle },
};

const LAST_RUN_STATUS_CONFIG: Record<string, { icon: typeof CheckCircle2; color: string }> = {
  completed: { icon: CheckCircle2, color: "text-emerald-500" },
  failed: { icon: XCircle, color: "text-destructive" },
};

const EXECUTION_MODE_LABELS: Record<string, string> = {
  create_issue: "Create Issue",
  run_only: "Run Only",
};

function AutopilotRow({ autopilot }: { autopilot: Autopilot }) {
  const { getActorName } = useActorName();
  const wsPaths = useWorkspacePaths();
  const triggerAutopilot = useTriggerAutopilot();
  const statusCfg = (STATUS_CONFIG[autopilot.status] ?? STATUS_CONFIG["active"])!;
  const StatusIcon = statusCfg.icon;
  const isRunning = autopilot.has_running_run ?? false;
  const lastRunStatusCfg = autopilot.last_run_status ? LAST_RUN_STATUS_CONFIG[autopilot.last_run_status] : null;
  const LastRunIcon = lastRunStatusCfg?.icon ?? null;

  const handleRunNow = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    try {
      await triggerAutopilot.mutateAsync(autopilot.id);
      toast.success("Autopilot triggered");
    } catch (err: any) {
      toast.error(err?.message || "Failed to trigger autopilot");
    }
  };

  return (
    <div className="group/row flex h-11 items-center gap-2 px-3 sm:px-5 text-sm transition-colors hover:bg-accent/40">
      <AppLink
        href={wsPaths.autopilotDetail(autopilot.id)}
        className="flex min-w-0 flex-1 items-center gap-2"
      >
        <Zap className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-medium">{autopilot.title}</span>
      </AppLink>

      {/* Agent — hidden on mobile */}
      <span className="hidden sm:flex w-32 items-center gap-1.5 shrink-0">
        <ActorAvatar actorType="agent" actorId={autopilot.assignee_id} size={18} />
        <span className="truncate text-xs text-muted-foreground">
          {getActorName("agent", autopilot.assignee_id)}
        </span>
      </span>

      {/* Mode — hidden below md */}
      <span className="hidden md:block w-24 shrink-0 text-center text-xs text-muted-foreground">
        {EXECUTION_MODE_LABELS[autopilot.execution_mode] ?? autopilot.execution_mode}
      </span>

      {/* Status */}
      <span className={cn("flex w-20 items-center justify-center gap-1 shrink-0 text-xs", statusCfg.color)}>
        <StatusIcon className="h-3 w-3" />
        {statusCfg.label}
      </span>

      {/* Last run — icon always visible, time on sm+ */}
      <span className="flex items-center justify-end gap-1 shrink-0 w-6 sm:w-20 text-xs text-muted-foreground">
        {autopilot.last_run_at ? (
          <>
            {LastRunIcon && (
              <LastRunIcon className={cn("h-3 w-3 shrink-0", lastRunStatusCfg?.color)} />
            )}
            <span className="hidden sm:inline tabular-nums">
              {formatRelativeDate(autopilot.last_run_at)}
            </span>
          </>
        ) : (
          <span className="hidden sm:inline">--</span>
        )}
      </span>

      {/* Run now — visible on row hover; icon-only on mobile */}
      <Button
        size="sm"
        variant="outline"
        className="h-7 opacity-0 group-hover/row:opacity-100 [@media(hover:none)]:opacity-100 transition-opacity shrink-0"
        onClick={handleRunNow}
        disabled={autopilot.status !== "active" || isRunning || triggerAutopilot.isPending}
      >
        {isRunning ? (
          <Loader2 className="h-3 w-3 sm:mr-1 animate-spin" />
        ) : (
          <Play className="h-3 w-3 sm:mr-1" />
        )}
        <span className="hidden sm:inline">{isRunning ? "Running..." : "Run now"}</span>
      </Button>
    </div>
  );
}

function CreateAutopilotDialog({
  open,
  onOpenChange,
  template,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  template?: AutopilotTemplate | null;
}) {
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOpenOptions(wsId));
  const createAutopilot = useCreateAutopilot();
  const createTrigger = useCreateAutopilotTrigger();

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [assigneeId, setAssigneeId] = useState("");
  const [projectId, setProjectId] = useState<string | null>(null);
  const [triggerConfig, setTriggerConfig] = useState<TriggerConfig>(getDefaultTriggerConfig);
  const [submitting, setSubmitting] = useState(false);

  // Apply template when it changes
  const [appliedTemplate, setAppliedTemplate] = useState<AutopilotTemplate | null | undefined>(null);
  if (template !== appliedTemplate && open) {
    setAppliedTemplate(template);
    if (template) {
      setTitle(template.title);
      setDescription(template.prompt);
      setTriggerConfig({
        ...getDefaultTriggerConfig(),
        frequency: template.frequency,
        time: template.time,
      });
    }
  }

  const activeAgents = agents.filter((a) => !a.archived_at);

  const handleSubmit = async () => {
    if (!title.trim() || !assigneeId || submitting) return;
    setSubmitting(true);
    try {
      const autopilot = await createAutopilot.mutateAsync({
        title: title.trim(),
        description: description.trim() || undefined,
        assignee_id: assigneeId,
        project_id: projectId || undefined,
        execution_mode: "create_issue",
      });

      // Attach schedule trigger
      try {
        await createTrigger.mutateAsync({
          autopilotId: autopilot.id,
          kind: "schedule",
          cron_expression: toCronExpression(triggerConfig),
          timezone: triggerConfig.timezone,
        });
      } catch {
        toast.error("Autopilot created, but trigger failed to save");
      }

      onOpenChange(false);
      setTitle("");
      setDescription("");
      setAssigneeId("");
      setProjectId(null);
      setTriggerConfig(getDefaultTriggerConfig());
      toast.success("Autopilot created");
    } catch {
      toast.error("Failed to create autopilot");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogTitle>New Autopilot</DialogTitle>
        <div className="space-y-5 pt-2">
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

          {/* Agent */}
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
                  <SelectItem key={a.id} value={a.id}>
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Project */}
          <div>
            <label className="text-xs font-medium text-muted-foreground">Project (Optional)</label>
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
                  <SelectItem key={p.id} value={p.id}>
                    {p.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Schedule */}
          <div>
            <label className="text-xs font-medium text-muted-foreground">Schedule</label>
            <div className="mt-2">
              <TriggerConfigSection config={triggerConfig} onChange={setTriggerConfig} />
            </div>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-1">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button size="sm" onClick={handleSubmit} disabled={!title.trim() || !assigneeId || submitting}>
              {submitting ? "Creating..." : "Create"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function AutopilotsPage() {
  const wsId = useWorkspaceId();
  const { data: autopilots = [], isLoading } = useQuery(autopilotListOptions(wsId));
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedTemplate, setSelectedTemplate] = useState<AutopilotTemplate | null>(null);

  const openCreate = (template?: AutopilotTemplate) => {
    setSelectedTemplate(template ?? null);
    setCreateOpen(true);
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Zap className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Autopilot</h1>
          {!isLoading && autopilots.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{autopilots.length}</span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={() => openCreate()}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          New autopilot
        </Button>
      </PageHeader>

      {/* Table */}
      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <>
            <div className="sticky top-0 z-[1] flex h-8 items-center gap-2 border-b bg-muted/30 px-3 sm:px-5">
              <span className="shrink-0 w-4" />
              <Skeleton className="h-3 w-12 flex-1 max-w-[48px]" />
              <Skeleton className="hidden sm:block h-3 w-12 shrink-0" />
              <Skeleton className="hidden md:block h-3 w-10 shrink-0" />
              <Skeleton className="h-3 w-10 shrink-0" />
              <Skeleton className="hidden md:block h-3 w-12 shrink-0" />
            </div>
            <div className="p-5 pt-1 space-y-1">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-11 w-full" />
              ))}
            </div>
          </>
        ) : autopilots.length === 0 ? (
          <div className="flex flex-col items-center py-16 px-5">
            <Zap className="h-10 w-10 mb-3 text-muted-foreground opacity-30" />
            <p className="text-sm text-muted-foreground">No autopilots yet</p>
            <p className="text-xs text-muted-foreground mt-1 mb-6">
              Schedule recurring tasks for your AI agents. Pick a template or start from scratch.
            </p>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 w-full max-w-3xl">
              {TEMPLATES.map((t) => {
                const Icon = t.icon;
                return (
                  <button
                    key={t.title}
                    type="button"
                    className="flex items-start gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-accent/40"
                    onClick={() => openCreate(t)}
                  >
                    <Icon className="h-5 w-5 shrink-0 text-muted-foreground mt-0.5" />
                    <div className="min-w-0">
                      <div className="text-sm font-medium">{t.title}</div>
                      <div className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{t.summary}</div>
                    </div>
                  </button>
                );
              })}
            </div>
            <Button size="sm" variant="outline" className="mt-4" onClick={() => openCreate()}>
              <Plus className="h-3.5 w-3.5 mr-1" />
              Start from scratch
            </Button>
          </div>
        ) : (
          <>
            {/* Column headers */}
            <div className="sticky top-0 z-[1] flex h-8 items-center gap-2 border-b bg-muted/30 px-3 sm:px-5 text-xs font-medium text-muted-foreground">
              <span className="shrink-0 w-4" />
              <span className="min-w-0 flex-1">Name</span>
              <span className="hidden sm:block w-32 shrink-0">Agent</span>
              <span className="hidden md:block w-24 text-center shrink-0">Mode</span>
              <span className="w-20 text-center shrink-0">Status</span>
              <span className="w-6 sm:w-20 text-right shrink-0">Last run</span>
            </div>
            {autopilots.map((autopilot) => (
              <AutopilotRow key={autopilot.id} autopilot={autopilot} />
            ))}
          </>
        )}
      </div>

      <CreateAutopilotDialog open={createOpen} onOpenChange={setCreateOpen} template={selectedTemplate} />
    </div>
  );
}
