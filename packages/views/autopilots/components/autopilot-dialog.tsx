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
  Maximize2,
  Minimize2,
  Play,
  Rocket,
  X as XIcon,
  Zap,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { TimeInput } from "@multica/ui/components/ui/time-input";
import { TimezonePicker } from "./pickers/timezone-picker";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
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
import { TitleEditor, ContentEditor } from "../../editor";
import { ActorAvatar } from "../../common/actor-avatar";
import { AgentPicker } from "./pickers/agent-picker";
import {
  getDefaultTriggerConfig,
  getLocalTimezone,
  parseCronExpression,
  toCronExpression,
  type TriggerConfig,
  type TriggerFrequency,
} from "./trigger-config";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface AutopilotInitial {
  title: string;
  description: string;
  assignee_id: string;
  execution_mode: AutopilotExecutionMode;
}

export type AutopilotDialogProps =
  | {
      mode: "create";
      open: boolean;
      onOpenChange: (v: boolean) => void;
      initial?: Partial<AutopilotInitial>;
      initialTriggerConfig?: Partial<TriggerConfig>;
      intent?: "create" | "duplicate";
    }
  | {
      mode: "edit";
      open: boolean;
      onOpenChange: (v: boolean) => void;
      autopilotId: string;
      initial: AutopilotInitial;
      triggers: AutopilotTrigger[];
    };

// ---------------------------------------------------------------------------
// Static data
// ---------------------------------------------------------------------------

const FREQUENCY_OPTIONS: { value: TriggerFrequency; label: string }[] = [
  { value: "hourly", label: "Every hour" },
  { value: "daily", label: "Every day" },
  { value: "weekdays", label: "Every weekday" },
  { value: "weekly", label: "Every week" },
  { value: "custom", label: "Custom cron" },
];

const DAY_OPTIONS: { value: number; label: string; short: string }[] = [
  { value: 0, label: "Sunday", short: "Sun" },
  { value: 1, label: "Monday", short: "Mon" },
  { value: 2, label: "Tuesday", short: "Tue" },
  { value: 3, label: "Wednesday", short: "Wed" },
  { value: 4, label: "Thursday", short: "Thu" },
  { value: 5, label: "Friday", short: "Fri" },
  { value: 6, label: "Saturday", short: "Sat" },
];

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

const OUTPUT_MODES: {
  value: AutopilotExecutionMode;
  label: string;
  description: string;
  Icon: typeof FilePlus2;
}[] = [
  {
    value: "create_issue",
    label: "Create issue",
    description: "Each run creates a tracked issue",
    Icon: FilePlus2,
  },
  {
    value: "run_only",
    label: "Run only",
    description: "Silent run, no issue created",
    Icon: Play,
  },
];

// ---------------------------------------------------------------------------
// Next-run computation (local approximation — server stores the authoritative value)
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
  }
}

function formatCountdown(target: Date, now: Date): string {
  const diff = Math.max(0, target.getTime() - now.getTime());
  const seconds = Math.floor(diff / 1000);
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m`;
  return "<1m";
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

// ---------------------------------------------------------------------------
// Live "now" ticker for countdown
// ---------------------------------------------------------------------------

function useNowTicker(intervalMs = 30_000): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

// ---------------------------------------------------------------------------
// AutopilotDialog
// ---------------------------------------------------------------------------

export function AutopilotDialog(props: AutopilotDialogProps) {
  const { open, onOpenChange } = props;
  const workspaceName = useCurrentWorkspace()?.name;
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const [isExpanded, setIsExpanded] = useState(false);

  const isCreate = props.mode === "create";
  const isDuplicate = isCreate && props.intent === "duplicate";
  const initial: Partial<AutopilotInitial> = isCreate
    ? props.initial ?? {}
    : props.initial;

  const [title, setTitle] = useState(initial.title ?? "");
  const [description, setDescription] = useState(initial.description ?? "");
  const [assigneeId, setAssigneeId] = useState<string>(initial.assignee_id ?? "");
  const [executionMode, setExecutionMode] = useState<AutopilotExecutionMode>(
    initial.execution_mode ?? "create_issue",
  );

  const initialCfg: TriggerConfig = (() => {
    if (isCreate) {
      const tpl = props.initialTriggerConfig;
      return tpl ? { ...getDefaultTriggerConfig(), ...tpl } : getDefaultTriggerConfig();
    }
    const first = props.triggers[0];
    if (first?.cron_expression) {
      return parseCronExpression(first.cron_expression, first.timezone ?? "UTC");
    }
    return getDefaultTriggerConfig();
  })();
  const [triggerConfig, setTriggerConfig] = useState<TriggerConfig>(initialCfg);

  const initialCronRef = useRef(toCronExpression(initialCfg));
  const initialTimezoneRef = useRef(initialCfg.timezone);
  const scheduleDirty =
    toCronExpression(triggerConfig) !== initialCronRef.current ||
    triggerConfig.timezone !== initialTimezoneRef.current;

  const firstTriggerIdRef = useRef(
    !isCreate && props.triggers[0] ? props.triggers[0].id : null,
  );

  const triggerCount = isCreate ? 0 : props.triggers.length;
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

  const canSubmit =
    title.trim().length > 0 && assigneeId.length > 0 && !submitting;

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
        onOpenChange(false);
        if (scheduleOk) toast.success(isDuplicate ? "Autopilot copied" : "Autopilot created");
        else toast.error("Autopilot created, but schedule failed to save");
      } else {
        await updateAutopilot.mutateAsync({
          id: props.autopilotId,
          title: title.trim(),
          description: description.trim() || null,
          assignee_id: assigneeId,
          execution_mode: executionMode,
        });
        let scheduleOk = true;
        if (scheduleDirty && !schedulePillDisabled) {
          const snapshottedTriggerId = firstTriggerIdRef.current;
          try {
            if (snapshottedTriggerId) {
              await updateTrigger.mutateAsync({
                autopilotId: props.autopilotId,
                triggerId: snapshottedTriggerId,
                cron_expression: toCronExpression(triggerConfig),
                timezone: triggerConfig.timezone,
              });
            } else {
              await createTrigger.mutateAsync({
                autopilotId: props.autopilotId,
                kind: "schedule",
                cron_expression: toCronExpression(triggerConfig),
                timezone: triggerConfig.timezone,
              });
            }
          } catch {
            scheduleOk = false;
          }
        }
        onOpenChange(false);
        if (scheduleOk) toast.success("Autopilot updated");
        else toast.error("Autopilot updated, but schedule failed to save");
      }
    } catch {
      toast.error(isCreate ? "Failed to create autopilot" : "Failed to update autopilot");
    } finally {
      setSubmitting(false);
    }
  };

  const contentKey = isCreate ? "create" : props.autopilotId;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className={cn(
          "p-0 gap-0 flex flex-col overflow-hidden",
          "!transition-all !duration-300 !ease-out !-translate-y-1/2",
          "!w-[calc(100vw-2rem)]",
          isExpanded
            ? "!max-w-6xl !h-[calc(100vh-4rem)]"
            : "!max-w-5xl !h-[min(720px,calc(100vh-4rem))]",
        )}
      >
        <DialogTitle className="sr-only">
          {isCreate ? (isDuplicate ? "Duplicate Autopilot" : "New Autopilot") : "Edit Autopilot"}
        </DialogTitle>

        {/* Header */}
        <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0 border-b">
          <div className="flex items-center gap-2 text-xs">
            <div className="flex items-center gap-1.5">
              <span className="inline-flex size-5 items-center justify-center rounded-md bg-primary/15 text-primary">
                <Rocket className="size-3" />
              </span>
              <span className="font-medium text-foreground">
                {isCreate ? (isDuplicate ? "Duplicate autopilot" : "New autopilot") : "Edit autopilot"}
              </span>
            </div>
            <span className="text-muted-foreground/60">·</span>
            <span className="text-muted-foreground">
              {isDuplicate ? "Review copied settings before saving" : "A recurring AI task"}
            </span>
            {workspaceName && (
              <>
                <ChevronRight className="size-3 text-muted-foreground/40" />
                <span className="text-muted-foreground">{workspaceName}</span>
              </>
            )}
          </div>
          <div className="flex items-center gap-1">
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    onClick={() => setIsExpanded((v) => !v)}
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                  </button>
                }
              />
              <TooltipContent side="bottom">{isExpanded ? "Collapse" : "Expand"}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    onClick={() => onOpenChange(false)}
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    <XIcon className="size-4" />
                  </button>
                }
              />
              <TooltipContent side="bottom">Close</TooltipContent>
            </Tooltip>
          </div>
        </div>

        {/* Body: two columns (stacks on narrow screens via flex-wrap at container level) */}
        <div
          key={contentKey}
          className="flex-1 min-h-0 flex flex-col lg:flex-row overflow-hidden"
        >
          {/* Left: Runbook */}
          <div className="flex-1 min-h-0 flex flex-col border-b lg:border-b-0 lg:border-r">
            <div className="px-6 pt-5 pb-3 shrink-0">
              <TitleEditor
                autoFocus={isCreate}
                defaultValue={initial.title ?? ""}
                placeholder="Autopilot name"
                className="text-2xl font-semibold tracking-tight"
                onChange={setTitle}
                onSubmit={handleSubmit}
              />
            </div>

            <div className="px-6 pb-2 shrink-0 flex items-baseline gap-2">
              <span className="text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase">
                Runbook
              </span>
              <span className="text-xs text-muted-foreground/80">
                Read by the agent on every run
              </span>
            </div>

            <div className="flex-1 min-h-0 px-6 pb-6 flex flex-col">
              <div className="h-full overflow-y-auto rounded-lg border border-border bg-background transition-colors focus-within:border-input px-4 py-3">
                <ContentEditor
                  defaultValue={initial.description ?? ""}
                  placeholder={`# Goal\nWhat should the agent accomplish?\n\n# Context\nWho is this for? Any constraints?\n\n# Steps\n1. …\n2. …`}
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

            <ScheduleSection
              config={triggerConfig}
              onChange={setTriggerConfig}
              disabled={schedulePillDisabled}
              disabledReason={
                schedulePillDisabled
                  ? "This autopilot has multiple schedules — edit them in the detail page."
                  : undefined
              }
            />
          </aside>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between gap-3 px-5 py-3 border-t shrink-0 bg-background">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground min-w-0">
            <Zap className="size-3.5 text-amber-500 shrink-0" />
            <span className="truncate">
              Once saved, runs automatically until paused.
            </span>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button size="sm" onClick={handleSubmit} disabled={!canSubmit}>
              {submitting
                ? isCreate
                  ? isDuplicate
                    ? "Copying..."
                    : "Creating..."
                  : "Saving..."
                : isCreate
                ? isDuplicate
                  ? "Create copy"
                  : "Create autopilot"
                : "Save"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Right column sections
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
  return (
    <div>
      <SectionLabel>Agent</SectionLabel>
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
              <ActorAvatar
                actorType="agent"
                actorId={selectedId}
                size={28}
                showStatusDot
              />
            ) : (
              <span className="inline-flex size-7 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <Rocket className="size-3.5" />
              </span>
            )}
            <span className="flex-1 min-w-0">
              <span className="block text-sm font-medium truncate">
                {selectedName ?? "Select agent"}
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
  return (
    <div>
      <SectionLabel>Output mode</SectionLabel>
      <div className="space-y-1.5">
        {OUTPUT_MODES.map((o) => {
          const selected = o.value === mode;
          return (
            <button
              key={o.value}
              type="button"
              onClick={() => onChange(o.value)}
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
                {selected && <Check className="size-2.5" strokeWidth={3} />}
              </span>
              <span className="flex-1 min-w-0">
                <span className="block text-sm font-medium">{o.label}</span>
                <span className="block text-xs text-muted-foreground">
                  {o.description}
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
  const now = useNowTicker();
  const next = useMemo(() => computeNextRun(config, now), [config, now]);
  const timezones = useMemo(() => {
    const local = getLocalTimezone();
    if (TIMEZONE_OPTIONS.includes(local)) return TIMEZONE_OPTIONS;
    return [local, ...TIMEZONE_OPTIONS];
  }, []);

  const selectedDay = config.daysOfWeek[0] ?? 1;

  return (
    <div>
      <SectionLabel>Schedule</SectionLabel>
      <div
        className={cn(
          "space-y-2",
          disabled && "opacity-60 pointer-events-none",
        )}
      >
        {/* Row 1: Frequency + (Day when weekly) */}
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
              {FREQUENCY_OPTIONS.map((f) => (
                <SelectItem key={f.value} value={f.value}>
                  {f.label}
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
                {DAY_OPTIONS.map((d) => (
                  <SelectItem key={d.value} value={String(d.value)}>
                    {d.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <div />
          )}
        </div>

        {/* Row 2: Time + Timezone (hidden for hourly / custom) */}
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
              onChange={(tz) => onChange({ ...config, timezone: tz })}
              options={timezones}
            />
          </div>
        )}

        {/* Next run preview */}
        {next && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground pt-1">
            <Clock className="size-3 shrink-0" />
            <span className="truncate">
              Next run:{" "}
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
