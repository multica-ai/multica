"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Clock,
  FilePlus2,
  Play,
  Rocket,
  Zap,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { TimeInput } from "@multica/ui/components/ui/time-input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { TimezonePicker } from "@multica/views/autopilots/components/pickers/timezone-picker";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { autopilotDetailOptions } from "@multica/core/autopilots/queries";
import {
  useCreateAutopilot,
  useCreateAutopilotTrigger,
  useUpdateAutopilot,
  useUpdateAutopilotTrigger,
} from "@multica/core/autopilots/mutations";
import type {
  AutopilotExecutionMode,
  AutopilotTrigger,
} from "@multica/core/types";
import { TitleEditor, ContentEditor } from "@multica/views/editor";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { AgentPicker } from "@multica/views/autopilots/components/pickers/agent-picker";
import { PageHeader } from "@multica/views/layout/page-header";
import { AppLink, useNavigation } from "@multica/views/navigation";
import { CerebroAutopilotModelSection } from "@multica/views/autopilots/components/cerebro-autopilot-model-section";
import { AutopilotPrivacySection } from "@multica/cerebro-access/views";
import {
  getDefaultTriggerConfig,
  getLocalTimezone,
  parseCronExpression,
  toCronExpression,
  type TriggerConfig,
  type TriggerFrequency,
} from "@multica/views/autopilots/components/trigger-config";
import { useT } from "@multica/views/i18n";

// ---------------------------------------------------------------------------
// Template pre-fill data (mirrors autopilots-page.tsx templates)
// ---------------------------------------------------------------------------

interface TemplateInitial {
  title: string;
  description: string;
  frequency: TriggerFrequency;
  time: string;
}

const TEMPLATE_INITIAL: Record<string, TemplateInitial> = {
  daily_news: {
    title: "Daily News Digest",
    description: `1. Search the web for news and announcements published today only (strictly today's date)
2. Filter for topics relevant to our team and industry
3. For each item, write a short summary including: title, source, key takeaways
4. Compile everything into a single digest post
5. Post the digest as a comment on this issue and @mention all workspace members`,
    frequency: "daily",
    time: "09:00",
  },
  pr_review: {
    title: "PR Review Reminder",
    description: `1. List all open pull requests in the repository
2. Identify PRs that have been open for more than 24 hours without a review
3. For each stale PR, note the author, age, and a one-line summary of the change
4. Post a comment on this issue listing all stale PRs with links
5. @mention the team to remind them to review`,
    frequency: "weekdays",
    time: "10:00",
  },
  bug_triage: {
    title: "Bug Triage",
    description: `1. List all issues with status "triage" or "backlog" that have not been prioritized
2. For each issue, read the description and any attached logs or screenshots
3. Assess severity (critical / high / medium / low) based on user impact and scope
4. Set the priority field on the issue accordingly
5. Add a comment explaining your assessment and suggested next steps`,
    frequency: "weekdays",
    time: "09:00",
  },
  weekly_progress: {
    title: "Weekly Progress Report",
    description: `1. Gather all issues completed (status "done") in the past 7 days
2. Gather all issues currently in progress
3. Identify any blocked issues and their blockers
4. Calculate key metrics: issues closed, issues opened, net change
5. Write a structured weekly report with sections: Completed, In Progress, Blocked, Metrics
6. Post the report as a comment on this issue`,
    frequency: "weekly",
    time: "17:00",
  },
  dependency_audit: {
    title: "Dependency Audit",
    description: `1. Run dependency audit tools on the project (npm audit, go vuln check, etc.)
2. Identify any packages with known security vulnerabilities
3. List outdated packages that are more than 2 major versions behind
4. For each finding, note the severity, affected package, and recommended fix
5. Post a summary report as a comment with actionable items`,
    frequency: "weekly",
    time: "08:00",
  },
  documentation_check: {
    title: "Documentation Check",
    description: `1. List all code changes merged in the past 7 days (via git log)
2. For each significant change, check if related documentation was updated
3. Identify any new APIs, config options, or features missing documentation
4. Create a list of documentation gaps with file paths and suggested content
5. Post the findings as a comment on this issue`,
    frequency: "weekly",
    time: "14:00",
  },
};

// ---------------------------------------------------------------------------
// Static data
// ---------------------------------------------------------------------------

const FREQUENCY_KEYS: TriggerFrequency[] = [
  "hourly",
  "daily",
  "weekdays",
  "weekly",
  "custom",
];

const DAY_KEYS = [
  "sunday",
  "monday",
  "tuesday",
  "wednesday",
  "thursday",
  "friday",
  "saturday",
] as const;

const TIMEZONE_OPTIONS = [
  "UTC",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "America/Sao_Paulo",
  "Europe/London",
  "Europe/Paris",
  "Europe/Berlin",
  "Europe/Moscow",
  "Asia/Dubai",
  "Asia/Kolkata",
  "Asia/Singapore",
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Asia/Seoul",
  "Australia/Sydney",
  "Pacific/Auckland",
];

const OUTPUT_MODE_KEYS: AutopilotExecutionMode[] = ["create_issue", "run_only"];

const OUTPUT_MODE_ICONS: Record<AutopilotExecutionMode, typeof FilePlus2> = {
  create_issue: FilePlus2,
  run_only: Play,
};

// ---------------------------------------------------------------------------
// Next-run helpers
// ---------------------------------------------------------------------------

function computeNextRun(cfg: TriggerConfig, now: Date): Date | null {
  const [hStr, mStr] = cfg.time.split(":");
  const hour = parseInt(hStr ?? "9", 10);
  const minute = parseInt(mStr ?? "0", 10);
  const next = new Date(now);

  switch (cfg.frequency) {
    case "hourly": {
      next.setMinutes(minute, 0, 0);
      if (next <= now) next.setHours(next.getHours() + 1);
      return next;
    }
    case "daily": {
      next.setHours(hour, minute, 0, 0);
      if (next <= now) next.setDate(next.getDate() + 1);
      return next;
    }
    case "weekdays": {
      next.setHours(hour, minute, 0, 0);
      for (let i = 0; i < 8; i++) {
        const dow = next.getDay();
        if (next > now && dow >= 1 && dow <= 5) return next;
        next.setDate(next.getDate() + 1);
        next.setHours(hour, minute, 0, 0);
      }
      return null;
    }
    case "weekly": {
      if (cfg.daysOfWeek.length === 0) return null;
      next.setHours(hour, minute, 0, 0);
      for (let i = 0; i < 8; i++) {
        if (next > now && cfg.daysOfWeek.includes(next.getDay())) return next;
        next.setDate(next.getDate() + 1);
        next.setHours(hour, minute, 0, 0);
      }
      return null;
    }
    case "custom":
      return null;
    default:
      return null;
  }
}

function useFormatCountdown(): (target: Date, now: Date) => string {
  const { t } = useT("autopilots");
  return (target, now) => {
    const diff = Math.max(0, target.getTime() - now.getTime());
    const seconds = Math.floor(diff / 1000);
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    if (days > 0) return t(($) => $.trigger_config.countdown.days_hours, { days, hours });
    if (hours > 0) return t(($) => $.trigger_config.countdown.hours_minutes, { hours, minutes });
    if (minutes > 0) return t(($) => $.trigger_config.countdown.minutes, { minutes });
    return t(($) => $.trigger_config.countdown.less_than_minute);
  };
}

function formatNextRunAbsolute(date: Date, timezone: string): string {
  try {
    return new Intl.DateTimeFormat("en-US", {
      timeZone: timezone,
      weekday: "short",
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
      hour12: true,
    }).format(date);
  } catch {
    return date.toLocaleString();
  }
}

function useNowTicker(intervalMs = 30_000): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

// ---------------------------------------------------------------------------
// Section sub-components
// ---------------------------------------------------------------------------

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase mb-2">
      {children}
    </div>
  );
}

function AgentSection({
  selectedId,
  onChange,
  selectedName,
  selectedDescription,
}: {
  selectedId: string;
  onChange: (id: string) => void;
  selectedName?: string;
  selectedDescription?: string;
}) {
  const { t } = useT("autopilots");
  return (
    <div>
      <SectionLabel>{t(($) => $.dialog.section_agent)}</SectionLabel>
      <AgentPicker
        agentId={selectedId || null}
        onChange={onChange}
        align="start"
        triggerRender={
          <button
            type="button"
            className={cn(
              "w-full flex items-center gap-2.5 rounded-md border bg-background px-3 py-2 text-left",
              "hover:bg-accent/40 transition-colors cursor-pointer",
            )}
          >
            {selectedId ? (
              <ActorAvatar actorType="agent" actorId={selectedId} size={28} showStatusDot />
            ) : (
              <span className="inline-flex size-7 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <Rocket className="size-3.5" />
              </span>
            )}
            <span className="flex-1 min-w-0">
              <span className="block text-sm font-medium truncate">
                {selectedName ?? t(($) => $.dialog.select_agent)}
              </span>
              {selectedDescription && (
                <span className="block text-xs text-muted-foreground truncate">
                  {selectedDescription}
                </span>
              )}
            </span>
            <ChevronDown className="size-3.5 text-muted-foreground shrink-0" />
          </button>
        }
      />
    </div>
  );
}

function OutputModeSection({
  mode,
  onChange,
}: {
  mode: AutopilotExecutionMode;
  onChange: (mode: AutopilotExecutionMode) => void;
}) {
  const { t } = useT("autopilots");
  return (
    <div>
      <SectionLabel>{t(($) => $.dialog.section_output_mode)}</SectionLabel>
      <div className="space-y-1.5">
        {OUTPUT_MODE_KEYS.map((key) => {
          const selected = key === mode;
          const Icon = OUTPUT_MODE_ICONS[key];
          return (
            <button
              key={key}
              type="button"
              onClick={() => onChange(key)}
              className={cn(
                "w-full flex items-start gap-2.5 rounded-md border px-3 py-2 text-left cursor-pointer transition-colors",
                selected
                  ? "border-primary bg-primary/5"
                  : "bg-background hover:bg-accent/40",
              )}
            >
              <span
                className={cn(
                  "mt-0.5 inline-flex size-4 shrink-0 items-center justify-center rounded-full border",
                  selected
                    ? "border-primary bg-primary text-primary-foreground"
                    : "border-muted-foreground/40 bg-background",
                )}
              >
                {selected ? (
                  <Check className="size-2.5" strokeWidth={3} />
                ) : (
                  <Icon className="size-2.5 opacity-0" />
                )}
              </span>
              <span className="flex-1 min-w-0">
                <span className="block text-sm font-medium">
                  {t(($) => $.dialog.output_modes[key].label)}
                </span>
                <span className="block text-xs text-muted-foreground">
                  {t(($) => $.dialog.output_modes[key].description)}
                </span>
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function ScheduleSection({
  config,
  onChange,
  disabled,
  disabledReason,
}: {
  config: TriggerConfig;
  onChange: (c: TriggerConfig) => void;
  disabled?: boolean;
  disabledReason?: string;
}) {
  const { t } = useT("autopilots");
  const formatCountdown = useFormatCountdown();
  const now = useNowTicker();
  const next = useMemo(() => computeNextRun(config, now), [config, now]);
  const timezones = useMemo(() => {
    const local = getLocalTimezone();
    if (TIMEZONE_OPTIONS.includes(local)) return TIMEZONE_OPTIONS;
    return [local, ...TIMEZONE_OPTIONS];
  }, []);

  const selectedDay = config.daysOfWeek[0] ?? 1;

  const freqLabels: Record<TriggerFrequency, string> = {
    hourly: t(($) => $.dialog.frequency_long.hourly),
    daily: t(($) => $.dialog.frequency_long.daily),
    weekdays: t(($) => $.dialog.frequency_long.weekdays),
    weekly: t(($) => $.dialog.frequency_long.weekly),
    custom: t(($) => $.dialog.frequency_long.custom),
  };

  return (
    <div>
      <SectionLabel>{t(($) => $.dialog.section_schedule)}</SectionLabel>
      <div
        className={cn(
          "space-y-2",
          disabled && "opacity-60 pointer-events-none",
        )}
      >
        <div className="grid grid-cols-2 gap-2">
          <Select
            value={config.frequency}
            onValueChange={(v) =>
              v && onChange({ ...config, frequency: v as TriggerFrequency })
            }
          >
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {FREQUENCY_KEYS.map((freq) => (
                <SelectItem key={freq} value={freq}>
                  {freqLabels[freq]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {config.frequency === "weekly" ? (
            <Select
              value={String(selectedDay)}
              onValueChange={(v) =>
                v && onChange({ ...config, daysOfWeek: [parseInt(v, 10)] })
              }
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DAY_KEYS.map((dayKey, i) => (
                  <SelectItem key={dayKey} value={String(i)}>
                    {t(($) => $.dialog.days[dayKey])}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <div />
          )}
        </div>

        {config.frequency === "custom" ? (
          <input
            type="text"
            value={config.cronExpression}
            onChange={(e) =>
              onChange({ ...config, cronExpression: e.target.value })
            }
            placeholder="0 9 * * 1-5"
            className="w-full rounded-lg border border-input bg-transparent px-2.5 py-1 h-8 text-sm font-mono outline-none transition-colors focus:border-ring focus:ring-3 focus:ring-ring/50 dark:bg-input/30"
          />
        ) : config.frequency === "hourly" ? (
          <TimeInput
            minuteOnly
            value={config.time}
            onChange={(v) => onChange({ ...config, time: v })}
          />
        ) : (
          <div className="grid grid-cols-2 gap-2">
            <TimeInput
              value={config.time}
              onChange={(v) => onChange({ ...config, time: v })}
            />
            <TimezonePicker
              value={config.timezone}
              onChange={(tz: string) => onChange({ ...config, timezone: tz })}
              options={timezones}
            />
          </div>
        )}

        {next && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground pt-1">
            <Clock className="size-3 shrink-0" />
            <span className="truncate">
              {t(($) => $.dialog.next_run_label)}{" "}
              <span className="text-foreground">
                {formatNextRunAbsolute(next, config.timezone)}
              </span>
            </span>
            <span className="ml-auto rounded-sm bg-muted px-1.5 py-0.5 text-[10px] font-medium text-foreground shrink-0">
              {formatCountdown(next, now)}
            </span>
          </div>
        )}
      </div>
      {disabled && disabledReason && (
        <p className="mt-2 text-[11px] text-muted-foreground">
          {disabledReason}
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Shared form body
// ---------------------------------------------------------------------------

interface FormBodyProps {
  mode: "create" | "edit";
  autopilotId?: string;
  initialTitle: string;
  initialDescription: string;
  initialAssigneeId: string;
  initialExecutionMode: AutopilotExecutionMode;
  initialModel: string | null;
  initialIsPrivate: boolean;
  initialTriggerConfig: TriggerConfig;
  triggers?: AutopilotTrigger[];
  onCancel: () => void;
}

function AutopilotFormBody({
  mode,
  autopilotId,
  initialTitle,
  initialDescription,
  initialAssigneeId,
  initialExecutionMode,
  initialModel,
  initialIsPrivate,
  initialTriggerConfig,
  triggers = [],
  onCancel,
}: FormBodyProps) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const workspaceName = useCurrentWorkspace()?.name;
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const isCreate = mode === "create";

  const [title, setTitle] = useState(initialTitle);
  const [description, setDescription] = useState(initialDescription);
  const [assigneeId, setAssigneeId] = useState<string>(initialAssigneeId);
  const [executionMode, setExecutionMode] = useState<AutopilotExecutionMode>(initialExecutionMode);
  const [modelOverride, setModelOverride] = useState<string | null>(initialModel);
  const [isPrivate, setIsPrivate] = useState<boolean>(initialIsPrivate);
  const [triggerConfig, setTriggerConfig] = useState<TriggerConfig>(initialTriggerConfig);

  const initialCronRef = useRef(toCronExpression(initialTriggerConfig));
  const initialTimezoneRef = useRef(initialTriggerConfig.timezone);
  const scheduleDirty =
    toCronExpression(triggerConfig) !== initialCronRef.current ||
    triggerConfig.timezone !== initialTimezoneRef.current;

  const firstTriggerIdRef = useRef(
    !isCreate && triggers[0] ? triggers[0].id : null,
  );

  const triggerCount = isCreate ? 0 : triggers.length;
  const schedulePillDisabled = !isCreate && triggerCount >= 2;

  const selectedAgent = useMemo(
    () => agents.find((a) => a.id === assigneeId) ?? null,
    [agents, assigneeId],
  );

  const createAutopilot = useCreateAutopilot();
  const createTrigger = useCreateAutopilotTrigger();
  const updateAutopilot = useUpdateAutopilot();
  const updateTrigger = useUpdateAutopilotTrigger();
  const [submitting, setSubmitting] = useState(false);

  const canSubmit = title.trim().length > 0 && assigneeId.length > 0 && !submitting;

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      if (isCreate) {
        const autopilot = await createAutopilot.mutateAsync({
          title: title.trim(),
          description: description.trim() || undefined,
          assignee_id: assigneeId,
          execution_mode: executionMode,
          ...(modelOverride ? { model: modelOverride } : {}),
          is_private: isPrivate,
        });
        let scheduleOk = true;
        try {
          await createTrigger.mutateAsync({
            autopilotId: autopilot.id,
            kind: "schedule",
            cron_expression: toCronExpression(triggerConfig),
            timezone: triggerConfig.timezone,
          });
        } catch {
          scheduleOk = false;
        }
        if (scheduleOk) toast.success(t(($) => $.dialog.toast_created));
        else toast.error(t(($) => $.dialog.toast_create_partial));
        router.push(wsPaths.autopilotDetail(autopilot.id));
      } else {
        await updateAutopilot.mutateAsync({
          id: autopilotId!,
          title: title.trim(),
          description: description.trim() || null,
          assignee_id: assigneeId,
          execution_mode: executionMode,
          model: modelOverride,
          is_private: isPrivate,
        });
        let scheduleOk = true;
        if (scheduleDirty && !schedulePillDisabled) {
          const snapshottedTriggerId = firstTriggerIdRef.current;
          try {
            if (snapshottedTriggerId) {
              await updateTrigger.mutateAsync({
                autopilotId: autopilotId!,
                triggerId: snapshottedTriggerId,
                cron_expression: toCronExpression(triggerConfig),
                timezone: triggerConfig.timezone,
              });
            } else {
              await createTrigger.mutateAsync({
                autopilotId: autopilotId!,
                kind: "schedule",
                cron_expression: toCronExpression(triggerConfig),
                timezone: triggerConfig.timezone,
              });
            }
          } catch {
            scheduleOk = false;
          }
        }
        if (scheduleOk) toast.success(t(($) => $.dialog.toast_updated));
        else toast.error(t(($) => $.dialog.toast_update_partial));
        router.push(wsPaths.autopilotDetail(autopilotId!));
      }
    } catch {
      toast.error(
        isCreate
          ? t(($) => $.dialog.toast_create_failed)
          : t(($) => $.dialog.toast_update_failed),
      );
      setSubmitting(false);
    }
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2 text-xs min-w-0">
          <AppLink
            href={isCreate ? wsPaths.autopilots() : wsPaths.autopilotDetail(autopilotId!)}
            className="text-muted-foreground hover:text-foreground transition-colors shrink-0"
          >
            <Zap className="h-4 w-4" />
          </AppLink>
          <span className="text-muted-foreground shrink-0">/</span>
          <span className="text-muted-foreground truncate">
            {isCreate
              ? t(($) => $.dialog.header_create)
              : t(($) => $.dialog.header_edit)}
          </span>
          {workspaceName && (
            <>
              <ChevronRight className="size-3 text-muted-foreground/40 shrink-0" />
              <span className="text-muted-foreground truncate">{workspaceName}</span>
            </>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Button size="sm" variant="outline" onClick={onCancel} disabled={submitting}>
            {t(($) => $.dialog.cancel)}
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={!canSubmit}>
            {submitting
              ? isCreate
                ? t(($) => $.dialog.creating)
                : t(($) => $.dialog.saving)
              : isCreate
              ? t(($) => $.dialog.create)
              : t(($) => $.dialog.save)}
          </Button>
        </div>
      </PageHeader>

      {/* Body: two columns */}
      <div className="flex-1 min-h-0 flex flex-col lg:flex-row overflow-hidden">
        {/* Left: Runbook */}
        <div className="flex-1 min-h-0 flex flex-col border-b lg:border-b-0 lg:border-r">
          <div className="px-6 pt-5 pb-3 shrink-0">
            <TitleEditor
              autoFocus={isCreate}
              defaultValue={initialTitle}
              placeholder={t(($) => $.dialog.title_placeholder)}
              className="text-2xl font-semibold tracking-tight"
              onChange={setTitle}
              onSubmit={handleSubmit}
            />
          </div>

          <div className="px-6 pb-2 shrink-0 flex items-baseline gap-2">
            <span className="text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase">
              {t(($) => $.dialog.runbook_label)}
            </span>
            <span className="text-xs text-muted-foreground/80">
              {t(($) => $.dialog.runbook_hint)}
            </span>
          </div>

          <div className="flex-1 min-h-0 px-6 pb-6 flex flex-col">
            <div className="h-full overflow-y-auto rounded-lg border border-border bg-background transition-colors focus-within:border-input px-4 py-3">
              <ContentEditor
                defaultValue={initialDescription}
                placeholder={t(($) => $.dialog.description_placeholder)}
                onUpdate={setDescription}
                debounceMs={300}
                showBubbleMenu={false}
              />
            </div>
          </div>
        </div>

        {/* Right: Configuration */}
        <aside className="w-full lg:w-[340px] shrink-0 overflow-y-auto px-5 py-5 space-y-5 bg-muted/30">
          <AgentSection
            selectedId={assigneeId}
            onChange={setAssigneeId}
            selectedName={selectedAgent?.name}
            selectedDescription={selectedAgent?.description}
          />

          <OutputModeSection mode={executionMode} onChange={setExecutionMode} />

          <CerebroAutopilotModelSection value={modelOverride} onChange={setModelOverride} />

          <AutopilotPrivacySection value={isPrivate} onChange={setIsPrivate} initialIsPrivate={initialIsPrivate} />

          <ScheduleSection
            config={triggerConfig}
            onChange={setTriggerConfig}
            disabled={schedulePillDisabled}
            disabledReason={
              schedulePillDisabled
                ? t(($) => $.dialog.schedule_disabled_reason)
                : undefined
            }
          />

          <div className="flex items-center gap-1.5 text-xs text-muted-foreground pt-2">
            <Zap className="size-3.5 text-amber-500 shrink-0" />
            <span className="truncate">{t(($) => $.dialog.auto_run_hint)}</span>
          </div>
        </aside>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Public page components
// ---------------------------------------------------------------------------

export function AutopilotCreatePage({ templateId }: { templateId?: string }) {
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();

  const tpl = templateId ? TEMPLATE_INITIAL[templateId] : undefined;

  const initialTriggerConfig: TriggerConfig = tpl
    ? { ...getDefaultTriggerConfig(), frequency: tpl.frequency, time: tpl.time }
    : getDefaultTriggerConfig();

  return (
    <AutopilotFormBody
      mode="create"
      initialTitle={tpl?.title ?? ""}
      initialDescription={tpl?.description ?? ""}
      initialAssigneeId=""
      initialExecutionMode="create_issue"
      initialModel={null}
      initialIsPrivate={false}
      initialTriggerConfig={initialTriggerConfig}
      onCancel={() => router.push(wsPaths.autopilots())}
    />
  );
}

export function AutopilotEditPage({ autopilotId }: { autopilotId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();

  const { data, isLoading } = useQuery(autopilotDetailOptions(wsId, autopilotId));

  if (isLoading) {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b px-5">
          <Skeleton className="h-4 w-4" />
          <span className="text-muted-foreground">/</span>
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="flex flex-1 min-h-0">
          <div className="flex-1 p-6 space-y-4">
            <Skeleton className="h-8 w-64" />
            <Skeleton className="h-4 w-32" />
            <Skeleton className="flex-1 h-48 w-full" />
          </div>
          <div className="w-[340px] p-5 space-y-4 bg-muted/30 border-l">
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-20 w-full" />
            <Skeleton className="h-10 w-full" />
          </div>
        </div>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        Not found
      </div>
    );
  }

  const { autopilot, triggers } = data;

  const initialTriggerConfig: TriggerConfig = (() => {
    const first = triggers[0];
    if (first?.cron_expression) {
      return parseCronExpression(first.cron_expression, first.timezone ?? "UTC");
    }
    return getDefaultTriggerConfig();
  })();

  return (
    <AutopilotFormBody
      mode="edit"
      autopilotId={autopilotId}
      initialTitle={autopilot.title}
      initialDescription={autopilot.description ?? ""}
      initialAssigneeId={autopilot.assignee_id}
      initialExecutionMode={autopilot.execution_mode as AutopilotExecutionMode}
      initialModel={(autopilot as any).model ?? null}
      initialIsPrivate={(autopilot as any).is_private ?? false}
      initialTriggerConfig={initialTriggerConfig}
      triggers={triggers}
      onCancel={() => router.push(wsPaths.autopilotDetail(autopilotId))}
    />
  );
}
