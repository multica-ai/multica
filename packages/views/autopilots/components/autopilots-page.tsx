"use client";

import { useState } from "react";
import { useT } from "@multica/i18n/react";
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

interface AutopilotTemplate {
  titleKey: string;
  promptKey: string;
  summaryKey: string;
  icon: typeof Zap;
  frequency: TriggerFrequency;
  time: string;
}

const TEMPLATES: AutopilotTemplate[] = [
  {
    titleKey: "tpl_daily_news",
    summaryKey: "tpl_daily_news_summary",
    promptKey: "tpl_daily_news_prompt",
    icon: Newspaper,
    frequency: "daily",
    time: "09:00",
  },
  {
    titleKey: "tpl_pr_review",
    summaryKey: "tpl_pr_review_summary",
    promptKey: "tpl_pr_review_prompt",
    icon: GitPullRequest,
    frequency: "weekdays",
    time: "10:00",
  },
  {
    titleKey: "tpl_bug_triage",
    summaryKey: "tpl_bug_triage_summary",
    promptKey: "tpl_bug_triage_prompt",
    icon: Bug,
    frequency: "weekdays",
    time: "09:00",
  },
  {
    titleKey: "tpl_weekly_report",
    summaryKey: "tpl_weekly_report_summary",
    promptKey: "tpl_weekly_report_prompt",
    icon: BarChart3,
    frequency: "weekly",
    time: "17:00",
  },
  {
    titleKey: "tpl_dep_audit",
    summaryKey: "tpl_dep_audit_summary",
    promptKey: "tpl_dep_audit_prompt",
    icon: Shield,
    frequency: "weekly",
    time: "08:00",
  },
  {
    titleKey: "tpl_docs_check",
    summaryKey: "tpl_docs_check_summary",
    promptKey: "tpl_docs_check_prompt",
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

const EXECUTION_MODE_LABELS: Record<string, string> = {
  create_issue: "Create Issue",
  run_only: "Run Only",
};

function AutopilotRow({ autopilot }: { autopilot: Autopilot }) {
  const { getActorName } = useActorName();
  const wsPaths = useWorkspacePaths();
  const statusCfg = (STATUS_CONFIG[autopilot.status] ?? STATUS_CONFIG["active"])!;
  const StatusIcon = statusCfg.icon;

  return (
    <div className="group/row flex flex-col gap-2 border-b px-4 py-3 text-sm transition-colors hover:bg-accent/40 sm:h-11 sm:flex-row sm:items-center sm:gap-2 sm:border-b-0 sm:px-5 sm:py-0">
      <AppLink
        href={wsPaths.autopilotDetail(autopilot.id)}
        className="flex min-w-0 items-center gap-2 sm:flex-1"
      >
        <Zap className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-medium">{autopilot.title}</span>
      </AppLink>

      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 pl-6 text-xs sm:contents sm:pl-0">
        {/* Agent */}
        <span className="flex min-w-0 items-center gap-1.5 text-muted-foreground sm:w-32 sm:shrink-0">
          <ActorAvatar actorType="agent" actorId={autopilot.assignee_id} size={18} enableHoverCard showStatusDot />
          <span className="truncate">
            {getActorName("agent", autopilot.assignee_id)}
          </span>
        </span>

        {/* Mode */}
        <span className="text-muted-foreground sm:w-24 sm:shrink-0 sm:text-center">
          {EXECUTION_MODE_LABELS[autopilot.execution_mode] ?? autopilot.execution_mode}
        </span>

        {/* Status */}
        <span className={cn("flex items-center gap-1 sm:w-20 sm:shrink-0 sm:justify-center", statusCfg.color)}>
          <StatusIcon className="h-3 w-3" />
          {statusCfg.label}
        </span>

        {/* Last run */}
        <span className="text-muted-foreground tabular-nums sm:w-20 sm:shrink-0 sm:text-right">
          {autopilot.last_run_at ? formatRelativeDate(autopilot.last_run_at) : "--"}
        </span>
      </div>
    </div>
  );
}

export function AutopilotsPage() {
  const wsId = useWorkspaceId();
  const t = useT("autopilots");
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
            <div className="sticky top-0 z-[1] hidden h-8 items-center gap-2 border-b bg-muted/30 px-5 sm:flex">
              <span className="shrink-0 w-4" />
              <Skeleton className="h-3 w-12 flex-1 max-w-[48px]" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-10 shrink-0" />
              <Skeleton className="h-3 w-10 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
            </div>
            <div className="space-y-2 p-4 sm:space-y-1 sm:p-5 sm:pt-1">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-[72px] w-full sm:h-11" />
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
              {TEMPLATES.map((template) => {
                const Icon = template.icon;
                return (
                  <button
                    key={template.titleKey}
                    type="button"
                    className="flex items-start gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-accent/40"
                    onClick={() => openCreate(template)}
                  >
                    <Icon className="h-5 w-5 shrink-0 text-muted-foreground mt-0.5" />
                    <div className="min-w-0">
                      <div className="text-sm font-medium">{t(template.titleKey)}</div>
                      <div className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{t(template.summaryKey)}</div>
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
            <div className="sticky top-0 z-[1] hidden h-8 items-center gap-2 border-b bg-muted/30 px-5 text-xs font-medium text-muted-foreground sm:flex">
              <span className="shrink-0 w-4" />
              <span className="min-w-0 flex-1">Name</span>
              <span className="w-32 shrink-0">Agent</span>
              <span className="w-24 text-center shrink-0">Mode</span>
              <span className="w-20 text-center shrink-0">Status</span>
              <span className="w-20 text-right shrink-0">Last run</span>
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
              ? { title: t(selectedTemplate.titleKey), description: t(selectedTemplate.promptKey) }
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
