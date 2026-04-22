"use client";

import { useMemo } from "react";
import { cn } from "@multica/ui/lib/utils";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";

export type TriggerFrequency = "hourly" | "daily" | "weekdays" | "weekly" | "custom";

export interface TriggerConfig {
  frequency: TriggerFrequency;
  time: string; // HH:MM
  daysOfWeek: number[]; // 0=Sun … 6=Sat — used when frequency === "weekly"
  cronExpression: string; // only used when frequency === "custom"
  timezone: string; // IANA
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const FREQUENCIES: { value: TriggerFrequency; label: string }[] = [
  { value: "hourly", label: "Hourly" },
  { value: "daily", label: "Daily" },
  { value: "weekdays", label: "Weekdays" },
  { value: "weekly", label: "Days" },
  { value: "custom", label: "Custom" },
];

const DAYS_OF_WEEK = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const COMMON_TIMEZONES = [
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

export function getLocalTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone;
  } catch {
    return "UTC";
  }
}

function getTimezoneOffset(tz: string): string {
  if (tz === "UTC") return "UTC";
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone: tz,
      timeZoneName: "shortOffset",
    }).formatToParts(new Date());
    return parts.find((p) => p.type === "timeZoneName")?.value ?? tz;
  } catch {
    return tz;
  }
}

function getTimezoneLabel(tz: string): string {
  if (tz === "UTC") return "UTC";
  const city = tz.split("/").pop()?.replace(/_/g, " ") ?? tz;
  return `${city} (${getTimezoneOffset(tz)})`;
}

function formatTime12h(time: string): string {
  const [h, m] = time.split(":");
  const hour = parseInt(h ?? "9", 10);
  const min = parseInt(m ?? "0", 10);
  const ampm = hour >= 12 ? "PM" : "AM";
  return `${hour % 12 || 12}:${min.toString().padStart(2, "0")} ${ampm}`;
}

// ---------------------------------------------------------------------------
// Public helpers
// ---------------------------------------------------------------------------

export function getDefaultTriggerConfig(): TriggerConfig {
  return {
    frequency: "daily",
    time: "09:00",
    daysOfWeek: [1],
    cronExpression: "0 9 * * 1-5",
    timezone: getLocalTimezone(),
  };
}

function sortedDays(days: number[]): number[] {
  return [...new Set(days)].sort((a, b) => a - b);
}

function formatDayList(days: number[]): string {
  const sorted = sortedDays(days);
  if (sorted.length === 0) return "—";
  return sorted.map((d) => DAYS_OF_WEEK[d]).join(", ");
}

export function toCronExpression(cfg: TriggerConfig): string {
  const [h, m] = cfg.time.split(":");
  const hour = parseInt(h ?? "9", 10);
  const min = parseInt(m ?? "0", 10);
  switch (cfg.frequency) {
    case "hourly":
      return `${min} * * * *`;
    case "daily":
      return `${min} ${hour} * * *`;
    case "weekdays":
      return `${min} ${hour} * * 1-5`;
    case "weekly": {
      const days = sortedDays(cfg.daysOfWeek);
      const dow = days.length > 0 ? days.join(",") : "1";
      return `${min} ${hour} * * ${dow}`;
    }
    case "custom":
      return cfg.cronExpression;
  }
}

export function describeTrigger(cfg: TriggerConfig): string {
  const offset = getTimezoneOffset(cfg.timezone);
  switch (cfg.frequency) {
    case "hourly": {
      const min = parseInt(cfg.time.split(":")[1] ?? "0", 10);
      return `Runs every hour at :${min.toString().padStart(2, "0")}`;
    }
    case "daily":
      return `Runs daily at ${formatTime12h(cfg.time)} ${offset}`;
    case "weekdays":
      return `Runs weekdays at ${formatTime12h(cfg.time)} ${offset}`;
    case "weekly":
      return `Runs every ${formatDayList(cfg.daysOfWeek)} at ${formatTime12h(cfg.time)} ${offset}`;
    case "custom":
      return `Custom schedule: ${cfg.cronExpression}`;
  }
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function TriggerConfigSection({
  config,
  onChange,
}: {
  config: TriggerConfig;
  onChange: (config: TriggerConfig) => void;
}) {
  const timezones = useMemo(() => {
    const local = getLocalTimezone();
    const set = new Set(COMMON_TIMEZONES);
    return set.has(local) ? COMMON_TIMEZONES : [local, ...COMMON_TIMEZONES];
  }, []);

  return (
    <div className="space-y-3">
      {/* Frequency tabs */}
      <div className="flex flex-wrap gap-1">
        {FREQUENCIES.map((f) => (
          <button
            key={f.value}
            type="button"
            className={cn(
              "rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
              config.frequency === f.value
                ? "bg-foreground text-background"
                : "bg-muted text-muted-foreground hover:text-foreground",
            )}
            onClick={() => onChange({ ...config, frequency: f.value })}
          >
            {f.label}
          </button>
        ))}
      </div>

      {config.frequency === "custom" ? (
        /* Custom cron input */
        <div>
          <label className="text-xs text-muted-foreground">Cron Expression</label>
          <input
            type="text"
            value={config.cronExpression}
            onChange={(e) => onChange({ ...config, cronExpression: e.target.value })}
            placeholder="0 9 * * 1-5"
            className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm font-mono outline-none focus:ring-1 focus:ring-ring"
          />
          <p className="text-xs text-muted-foreground mt-1">
            Standard 5-field cron (min hour dom month dow)
          </p>
        </div>
      ) : (
        <>
          {/* Time + Timezone row */}
          <div className="flex gap-3">
            {config.frequency === "hourly" ? (
              <div className="w-24">
                <label className="text-xs text-muted-foreground">Minute</label>
                <input
                  type="number"
                  min={0}
                  max={59}
                  value={parseInt(config.time.split(":")[1] ?? "0", 10)}
                  onChange={(e) => {
                    const min = Math.max(0, Math.min(59, parseInt(e.target.value) || 0));
                    onChange({ ...config, time: `00:${min.toString().padStart(2, "0")}` });
                  }}
                  className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm font-mono outline-none focus:ring-1 focus:ring-ring"
                />
              </div>
            ) : (
              <>
                <div className="w-28">
                  <label className="text-xs text-muted-foreground">Time</label>
                  <input
                    type="time"
                    value={config.time}
                    onChange={(e) => onChange({ ...config, time: e.target.value || config.time })}
                    className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm font-mono outline-none focus:ring-1 focus:ring-ring"
                  />
                </div>
                <div className="flex-1 min-w-0">
                  <label className="text-xs text-muted-foreground">Timezone</label>
                  <Select
                    value={config.timezone}
                    onValueChange={(v) => v && onChange({ ...config, timezone: v })}
                  >
                    <SelectTrigger className="mt-1 w-full">
                      <SelectValue>
                        {() => getTimezoneLabel(config.timezone)}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {timezones.map((tz) => (
                        <SelectItem key={tz} value={tz}>
                          {getTimezoneLabel(tz)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </>
            )}
          </div>

          {/* Day-of-week multi-selector for weekly */}
          {config.frequency === "weekly" && (
            <div>
              <label className="text-xs text-muted-foreground">Days</label>
              <div className="flex gap-1 mt-1">
                {DAYS_OF_WEEK.map((day, i) => {
                  const selected = config.daysOfWeek.includes(i);
                  return (
                    <button
                      key={day}
                      type="button"
                      aria-pressed={selected}
                      className={cn(
                        "rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
                        selected
                          ? "bg-foreground text-background"
                          : "bg-muted text-muted-foreground hover:text-foreground",
                      )}
                      onClick={() => {
                        const next = selected
                          ? config.daysOfWeek.filter((d) => d !== i)
                          : [...config.daysOfWeek, i];
                        // Keep at least one day selected so the cron stays valid.
                        onChange({
                          ...config,
                          daysOfWeek: next.length > 0 ? next : config.daysOfWeek,
                        });
                      }}
                    >
                      {day}
                    </button>
                  );
                })}
              </div>
            </div>
          )}
        </>
      )}

      {/* Human-readable preview */}
      <p className="text-xs text-muted-foreground">{describeTrigger(config)}</p>
    </div>
  );
}
