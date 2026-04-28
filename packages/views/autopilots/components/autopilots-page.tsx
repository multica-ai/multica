"use client";

import { useState } from "react";
import { Plus, Zap, Play, Pause, AlertCircle, Newspaper, GitPullRequest, Bug, BarChart3, Shield, FileSearch } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { AutopilotDialog } from "./autopilot-dialog";
import type { Autopilot } from "@multica/core/types";
import type { TriggerFrequency } from "./trigger-config";
import { useAutopilotsT, type AutopilotsDict } from "../i18n";

interface AutopilotTemplate {
  title: string;
  prompt: string;
  summary: string;
  icon: typeof Zap;
  frequency: TriggerFrequency;
  time: string;
}

// Prompts are intentionally English — they are read by the agent at runtime,
// not shown as user-facing UI copy. Title/summary are translated via the dict.
const TEMPLATE_PROMPTS = {
  dailyNews: `1. Search the web for news and announcements published today only (strictly today's date)
2. Filter for topics relevant to our team and industry
3. For each item, write a short summary including: title, source, key takeaways
4. Compile everything into a single digest post
5. Post the digest as a comment on this issue and @mention all workspace members`,
  prReview: `1. List all open pull requests in the repository
2. Identify PRs that have been open for more than 24 hours without a review
3. For each stale PR, note the author, age, and a one-line summary of the change
4. Post a comment on this issue listing all stale PRs with links
5. @mention the team to remind them to review`,
  bugTriage: `1. List all issues with status "triage" or "backlog" that have not been prioritized
2. For each issue, read the description and any attached logs or screenshots
3. Assess severity (critical / high / medium / low) based on user impact and scope
4. Set the priority field on the issue accordingly
5. Add a comment explaining your assessment and suggested next steps`,
  weeklyReport: `1. Gather all issues completed (status "done") in the past 7 days
2. Gather all issues currently in progress
3. Identify any blocked issues and their blockers
4. Calculate key metrics: issues closed, issues opened, net change
5. Write a structured weekly report with sections: Completed, In Progress, Blocked, Metrics
6. Post the report as a comment on this issue`,
  dependencyAudit: `1. Run dependency audit tools on the project (npm audit, go vuln check, etc.)
2. Identify any packages with known security vulnerabilities
3. List outdated packages that are more than 2 major versions behind
4. For each finding, note the severity, affected package, and recommended fix
5. Post a summary report as a comment with actionable items`,
  docsCheck: `1. List all code changes merged in the past 7 days (via git log)
2. For each significant change, check if related documentation was updated
3. Identify any new APIs, config options, or features missing documentation
4. Create a list of documentation gaps with file paths and suggested content
5. Post the findings as a comment on this issue`,
} as const;

function buildTemplates(t: AutopilotsDict["templates"]): AutopilotTemplate[] {
  return [
    {
      title: t.dailyNewsTitle,
      summary: t.dailyNewsSummary,
      prompt: TEMPLATE_PROMPTS.dailyNews,
      icon: Newspaper,
      frequency: "daily",
      time: "09:00",
    },
    {
      title: t.prReviewTitle,
      summary: t.prReviewSummary,
      prompt: TEMPLATE_PROMPTS.prReview,
      icon: GitPullRequest,
      frequency: "weekdays",
      time: "10:00",
    },
    {
      title: t.bugTriageTitle,
      summary: t.bugTriageSummary,
      prompt: TEMPLATE_PROMPTS.bugTriage,
      icon: Bug,
      frequency: "weekdays",
      time: "09:00",
    },
    {
      title: t.weeklyReportTitle,
      summary: t.weeklyReportSummary,
      prompt: TEMPLATE_PROMPTS.weeklyReport,
      icon: BarChart3,
      frequency: "weekly",
      time: "17:00",
    },
    {
      title: t.dependencyAuditTitle,
      summary: t.dependencyAuditSummary,
      prompt: TEMPLATE_PROMPTS.dependencyAudit,
      icon: Shield,
      frequency: "weekly",
      time: "08:00",
    },
    {
      title: t.docsCheckTitle,
      summary: t.docsCheckSummary,
      prompt: TEMPLATE_PROMPTS.docsCheck,
      icon: FileSearch,
      frequency: "weekly",
      time: "14:00",
    },
  ];
}

function formatRelativeDate(date: string, t: AutopilotsDict["page"]): string {
  const diff = Date.now() - new Date(date).getTime();
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days < 1) return t.today;
  if (days === 1) return t.dayAgo;
  if (days < 30) return t.daysAgo(days);
  const months = Math.floor(days / 30);
  return t.monthsAgo(months);
}

function getStatusConfig(t: AutopilotsDict["page"]): Record<
  string,
  { label: string; color: string; icon: typeof Zap }
> {
  return {
    active: { label: t.statusActive, color: "text-emerald-500", icon: Play },
    paused: { label: t.statusPaused, color: "text-amber-500", icon: Pause },
    archived: { label: t.statusArchived, color: "text-muted-foreground", icon: AlertCircle },
  };
}

function getExecutionModeLabels(t: AutopilotsDict["page"]): Record<string, string> {
  return {
    create_issue: t.modeCreateIssue,
    run_only: t.modeRunOnly,
  };
}

function AutopilotRow({ autopilot }: { autopilot: Autopilot }) {
  const { getActorName } = useActorName();
  const wsPaths = useWorkspacePaths();
  const t = useAutopilotsT();
  const statusConfig = getStatusConfig(t.page);
  const executionModeLabels = getExecutionModeLabels(t.page);
  const statusCfg = (statusConfig[autopilot.status] ?? statusConfig["active"])!;
  const StatusIcon = statusCfg.icon;

  return (
    <div className="group/row flex h-11 items-center gap-2 px-5 text-sm transition-colors hover:bg-accent/40">
      <AppLink
        href={wsPaths.autopilotDetail(autopilot.id)}
        className="flex min-w-0 flex-1 items-center gap-2"
      >
        <Zap className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-medium">{autopilot.title}</span>
      </AppLink>

      {/* Agent */}
      <span className="flex w-32 items-center gap-1.5 shrink-0">
        <ActorAvatar actorType="agent" actorId={autopilot.assignee_id} size={18} />
        <span className="truncate text-xs text-muted-foreground">
          {getActorName("agent", autopilot.assignee_id)}
        </span>
      </span>

      {/* Mode */}
      <span className="w-24 shrink-0 text-center text-xs text-muted-foreground">
        {executionModeLabels[autopilot.execution_mode] ?? autopilot.execution_mode}
      </span>

      {/* Status */}
      <span className={cn("flex w-20 items-center justify-center gap-1 shrink-0 text-xs", statusCfg.color)}>
        <StatusIcon className="h-3 w-3" />
        {statusCfg.label}
      </span>

      {/* Last run */}
      <span className="w-20 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
        {autopilot.last_run_at ? formatRelativeDate(autopilot.last_run_at, t.page) : "--"}
      </span>
    </div>
  );
}

export function AutopilotsPage() {
  const wsId = useWorkspaceId();
  const t = useAutopilotsT();
  const templates = buildTemplates(t.templates);
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
          <h1 className="text-sm font-medium">{t.page.title}</h1>
          {!isLoading && autopilots.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{autopilots.length}</span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={() => openCreate()}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          {t.page.newAutopilot}
        </Button>
      </PageHeader>

      {/* Table */}
      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <>
            <div className="sticky top-0 z-[1] flex h-8 items-center gap-2 border-b bg-muted/30 px-5">
              <span className="shrink-0 w-4" />
              <Skeleton className="h-3 w-12 flex-1 max-w-[48px]" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-10 shrink-0" />
              <Skeleton className="h-3 w-10 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
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
            <p className="text-sm text-muted-foreground">{t.page.emptyTitle}</p>
            <p className="text-xs text-muted-foreground mt-1 mb-6">
              {t.page.emptyHint}
            </p>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 w-full max-w-3xl">
              {templates.map((tpl) => {
                const Icon = tpl.icon;
                return (
                  <button
                    key={tpl.title}
                    type="button"
                    className="flex items-start gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-accent/40"
                    onClick={() => openCreate(tpl)}
                  >
                    <Icon className="h-5 w-5 shrink-0 text-muted-foreground mt-0.5" />
                    <div className="min-w-0">
                      <div className="text-sm font-medium">{tpl.title}</div>
                      <div className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{tpl.summary}</div>
                    </div>
                  </button>
                );
              })}
            </div>
            <Button size="sm" variant="outline" className="mt-4" onClick={() => openCreate()}>
              <Plus className="h-3.5 w-3.5 mr-1" />
              {t.page.emptyStartFromScratch}
            </Button>
          </div>
        ) : (
          <>
            {/* Column headers */}
            <div className="sticky top-0 z-[1] flex h-8 items-center gap-2 border-b bg-muted/30 px-5 text-xs font-medium text-muted-foreground">
              <span className="shrink-0 w-4" />
              <span className="min-w-0 flex-1">{t.page.columnName}</span>
              <span className="w-32 shrink-0">{t.page.columnAgent}</span>
              <span className="w-24 text-center shrink-0">{t.page.columnMode}</span>
              <span className="w-20 text-center shrink-0">{t.page.columnStatus}</span>
              <span className="w-20 text-right shrink-0">{t.page.columnLastRun}</span>
            </div>
            {autopilots.map((autopilot) => (
              <AutopilotRow key={autopilot.id} autopilot={autopilot} />
            ))}
          </>
        )}
      </div>

      {createOpen && (
        <AutopilotDialog
          mode="create"
          open={createOpen}
          onOpenChange={setCreateOpen}
          initial={
            selectedTemplate
              ? { title: selectedTemplate.title, description: selectedTemplate.prompt }
              : undefined
          }
          initialTriggerConfig={
            selectedTemplate
              ? { frequency: selectedTemplate.frequency, time: selectedTemplate.time }
              : undefined
          }
        />
      )}
    </div>
  );
}
