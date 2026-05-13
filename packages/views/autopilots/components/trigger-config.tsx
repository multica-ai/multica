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
import { useT } from "../../i18n";

export type TriggerFrequency = "hourly" | "daily" | "weekdays" | "weekly" | "custom";

export interface TriggerConfig {
  frequency: TriggerFrequency;
  time: string; // HH:MM
  daysOfWeek: number[]; // 0=Sun … 6=Sat — used when frequency === "weekly"
  cronExpression: string; // only used when frequency === "custom"
  timezone: string; // IANA
}

// ---------------------------------------------------------------------------
// Constants — schema-level (not user-visible)
// ---------------------------------------------------------------------------

const FREQUENCY_KEYS: TriggerFrequency[] = [
  "hourly",
  "daily",
  "weekdays",
  "weekly",
  "custom",
];

const DAY_SHORT_KEYS = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"] as const;

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

export function parseCronExpression(cron: string, timezone: string): TriggerConfig {
  const base: TriggerConfig = {
    ...getDefaultTriggerConfig(),
    timezone,
    cronExpression: cron,
  };
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return { ...base, frequency: "custom" };
  const minStr = parts[0] ?? "";
  const hourStr = parts[1] ?? "";
  const dom = parts[2] ?? "";
  const mon = parts[3] ?? "";
  const dow = parts[4] ?? "";
  if (dom !== "*" || mon !== "*") return { ...base, frequency: "custom" };
  const min = parseInt(minStr, 10);
  if (Number.isNaN(min) || min < 0 || min > 59) return { ...base, frequency: "custom" };

  if (hourStr === "*" && dow === "*") {
    const time = `00:${String(min).padStart(2, "0")}`;
    return { ...base, frequency: "hourly", time };
  }
  const hour = parseInt(hourStr, 10);
  if (Number.isNaN(hour) || hour < 0 || hour > 23) return { ...base, frequency: "custom" };
  const time = `${String(hour).padStart(2, "0")}:${String(min).padStart(2, "0")}`;

  if (dow === "*") return { ...base, frequency: "daily", time };
  if (dow === "1-5") return { ...base, frequency: "weekdays", time, daysOfWeek: [1, 2, 3, 4, 5] };
  if (/^[0-6](,[0-6])*$/.test(dow)) {
    const days = dow.split(",").map((n) => parseInt(n, 10));
    return { ...base, frequency: "weekly", time, daysOfWeek: days };
  }
  return { ...base, frequency: "custom" };
}

// ---------------------------------------------------------------------------
// Hooks (i18n-aware)
// ---------------------------------------------------------------------------

// Hook returning a function that produces the compact "Hourly · :15 / Daily 09:00"
// summary. Was a pure module-level function before i18n; converted to a hook so
// the strings can flow through useT.
export function useSummarizeTrigger(): (cfg: TriggerConfig) => string {
  const { t } = useT("autopilots");
  return (cfg) => {
    switch (cfg.frequency) {
      case "hourly": {
        const min = cfg.time.split(":")[1] ?? "00";
        return t(($) => $.trigger_config.summary.hourly, { min });
      }
      case "daily":
        return t(($) => $.trigger_config.summary.daily, { time: cfg.time });
      case "weekdays":
        return t(($) => $.trigger_config.summary.weekdays, { time: cfg.time });
      case "weekly":
        return t(($) => $.trigger_config.summary.weekly, {
          days: formatDayList(cfg.daysOfWeek, t),
          time: cfg.time,
        });
      case "custom":
        return t(($) => $.trigger_config.summary.custom);
    }
  };
}

// Hook returning a function that produces the longer "Runs daily at 9:00 AM PDT"
// description. Same rationale as useSummarizeTrigger.
export function useDescribeTrigger(): (cfg: TriggerConfig) => string {
  const { t } = useT("autopilots");
  return (cfg) => {
    const offset = getTimezoneOffset(cfg.timezone);
    switch (cfg.frequency) {
      case "hourly": {
        const min = parseInt(cfg.time.split(":")[1] ?? "0", 10).toString().padStart(2, "0");
        return t(($) => $.trigger_config.describe.hourly, { min });
      }
      case "daily":
        return t(($) => $.trigger_config.describe.daily, {
          time: formatTime12h(cfg.time),
          offset,
        });
      case "weekdays":
        return t(($) => $.trigger_config.describe.weekdays, {
          time: formatTime12h(cfg.time),
          offset,
        });
      case "weekly":
        return t(($) => $.trigger_config.describe.weekly, {
          days: formatDayList(cfg.daysOfWeek, t),
          time: formatTime12h(cfg.time),
          offset,
        });
      case "custom":
        return t(($) => $.trigger_config.describe.custom, {
          cron: cfg.cronExpression,
        });
    }
  };
}

// Helper that resolves day-short labels through t() — extracted so the two
// hooks above can share the join logic without each grabbing its own t.
type AutopilotsT = ReturnType<typeof useT<"autopilots">>["t"];
function formatDayList(days: number[], t: AutopilotsT): string {
  const sorted = sortedDays(days);
  if (sorted.length === 0) return t(($) => $.trigger_config.summary.no_days);
  return sorted
    .map((d) => t(($) => $.trigger_config.days_short[DAY_SHORT_KEYS[d]!]))
    .join(", ");
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
  const { t } = useT("autopilots");
  const describeTrigger = useDescribeTrigger();
  const timezones = useMemo(() => {
    const local = getLocalTimezone();
    const set = new Set(COMMON_TIMEZONES);
    return set.has(local) ? COMMON_TIMEZONES : [local, ...COMMON_TIMEZONES];
  }, []);

  return (
    <div className="space-y-3">
      {/* Frequency tabs */}
      <div className="flex flex-wrap gap-1">
        {FREQUENCY_KEYS.map((freq) => (
          <button
            key={freq}
            type="button"
            className={cn(
              "rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
              config.frequency === freq
                ? "bg-foreground text-background"
                : "bg-muted text-muted-foreground hover:text-foreground",
            )}
            onClick={() => onChange({ ...config, frequency: freq })}
          >
            {t(($) => $.trigger_config.frequencies[freq])}
          </button>
        ))}
      </div>

      {config.frequency === "custom" ? (
        /* Custom cron input */
        <div>
          <label className="text-xs text-muted-foreground">
            {t(($) => $.trigger_config.cron_label)}
          </label>
          <input
            type="text"
            value={config.cronExpression}
            onChange={(e) => onChange({ ...config, cronExpression: e.target.value })}
            placeholder="0 9 * * 1-5"
            className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm font-mono outline-none focus:ring-1 focus:ring-ring"
          />
          <p className="text-xs text-muted-foreground mt-1">
            {t(($) => $.trigger_config.cron_hint)}
          </p>
        </div>
      ) : (
        <>
          {/* Time + Timezone row */}
          <div className="flex gap-3">
            {config.frequency === "hourly" ? (
              <div className="w-24">
                <label className="text-xs text-muted-foreground">
                  {t(($) => $.trigger_config.minute_label)}
                </label>
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
                  <label className="text-xs text-muted-foreground">
                    {t(($) => $.trigger_config.time_label)}
                  </label>
                  <input
                    type="time"
                    value={config.time}
                    onChange={(e) => onChange({ ...config, time: e.target.value || config.time })}
                    className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm font-mono outline-none focus:ring-1 focus:ring-ring"
                  />
                </div>
                <div className="flex-1 min-w-0">
                  <label className="text-xs text-muted-foreground">
                    {t(($) => $.trigger_config.timezone_label)}
                  </label>
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
              <label className="text-xs text-muted-foreground">
                {t(($) => $.trigger_config.days_label)}
              </label>
              <div className="flex gap-1 mt-1">
                {DAY_SHORT_KEYS.map((dayKey, i) => {
                  const selected = config.daysOfWeek.includes(i);
                  return (
                    <button
                      key={dayKey}
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
                      {t(($) => $.trigger_config.days_short[dayKey])}
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
