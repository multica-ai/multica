import { z } from "zod";
import type { AgentRuntime } from "../types";

export const USAGE_LIMIT_WARNING_PERCENT = 80;
// The daemon checks every 15 minutes while idle; past this, a check was missed.
export const USAGE_LIMIT_STALE_AFTER_MS = 20 * 60 * 1000;

const isoDate = z.string().refine((value) => !Number.isNaN(Date.parse(value)));

const UsageLimitWindowSchema = z.object({
  id: z.string().min(1),
  label: z.string().catch(""),
  window_minutes: z.number().nonnegative().catch(0),
  used_percent: z.number().min(0).catch(0),
  resets_at: isoDate.nullable().catch(null),
});

export const RuntimeUsageLimitsSchema = z.object({
  provider: z.string().min(1),
  account_id: z.string().catch(""),
  plan: z.string().catch(""),
  device: z.string().catch(""),
  windows: z
    .array(z.unknown())
    .catch([])
    .transform((items) =>
      items.flatMap((item) => {
        const parsed = UsageLimitWindowSchema.safeParse(item);
        return parsed.success ? [parsed.data] : [];
      }),
    ),
  updated_at: isoDate.nullable().catch(null),
  checked_at: isoDate.nullable().catch(null),
  source: z.string().catch(""),
  error: z.string().catch(""),
});

export type RuntimeUsageLimits = z.infer<typeof RuntimeUsageLimitsSchema>;
export type UsageLimitWindow = RuntimeUsageLimits["windows"][number];

export function readRuntimeUsageLimits(runtime: Pick<AgentRuntime, "metadata">): RuntimeUsageLimits | null {
  const raw = runtime.metadata?.usage_limits;
  if (raw === undefined || raw === null) return null;
  const parsed = RuntimeUsageLimitsSchema.safeParse(raw);
  return parsed.success ? parsed.data : null;
}

export interface UsageLimitAccount {
  key: string;
  provider: string;
  plan: string;
  devices: string[];
  /** Tightest window first. */
  windows: UsageLimitWindow[];
  updatedAt: string | null;
  checkedAt: string | null;
  error: string;
  stale: boolean;
  /** One online runtime per daemon, enough to ask every machine to check now. */
  refreshRuntimeIds: string[];
}

const PROVIDER_ORDER = ["claude", "codex"];

function time(value: string | null): number {
  return value ? Date.parse(value) : 0;
}

// A window past its reset time has started over, so its stored use is gone.
function currentWindows(windows: UsageLimitWindow[], now: number): UsageLimitWindow[] {
  return windows
    .map((window) =>
      window.resets_at && time(window.resets_at) <= now ? { ...window, used_percent: 0, resets_at: null } : window,
    )
    .sort(
    (a, b) => b.used_percent - a.used_percent || a.window_minutes - b.window_minutes || a.id.localeCompare(b.id),
  );
}

/**
 * Groups runtime snapshots by provider login. One login can sit behind many
 * runtimes (several machines, several workspaces), so the newest data wins
 * and the account is stale only when that data is old or its last check failed.
 */
export function groupUsageLimitAccounts(runtimes: AgentRuntime[], now: number = Date.now()): UsageLimitAccount[] {
  const groups = new Map<string, { snapshots: RuntimeUsageLimits[]; runtimes: AgentRuntime[] }>();
  for (const runtime of runtimes) {
    const snapshot = readRuntimeUsageLimits(runtime);
    if (!snapshot || snapshot.provider !== runtime.provider) continue;
    const key = `${snapshot.provider}:${snapshot.account_id || `device:${snapshot.device || runtime.daemon_id || runtime.id}`}`;
    const group = groups.get(key) ?? { snapshots: [], runtimes: [] };
    group.snapshots.push(snapshot);
    group.runtimes.push(runtime);
    groups.set(key, group);
  }

  const accounts: UsageLimitAccount[] = [];
  for (const [key, group] of groups) {
    const best = [...group.snapshots].sort(
      (a, b) => time(b.updated_at) - time(a.updated_at) || time(b.checked_at) - time(a.checked_at),
    )[0]!;
    const refreshRuntimeIds = new Map<string, string>();
    for (const runtime of group.runtimes) {
      if (runtime.status !== "online") continue;
      const daemonKey = runtime.daemon_id ?? runtime.id;
      if (!refreshRuntimeIds.has(daemonKey)) refreshRuntimeIds.set(daemonKey, runtime.id);
    }
    const devices = [...new Set(group.snapshots.map((snapshot) => snapshot.device).filter(Boolean))].sort();
    accounts.push({
      key,
      provider: best.provider,
      plan: best.plan,
      devices,
      windows: currentWindows(best.windows, now),
      updatedAt: best.updated_at,
      checkedAt: best.checked_at,
      error: best.error,
      stale: !best.updated_at || now - time(best.updated_at) > USAGE_LIMIT_STALE_AFTER_MS || best.error !== "",
      refreshRuntimeIds: [...refreshRuntimeIds.values()],
    });
  }

  const rank = (provider: string) => {
    const index = PROVIDER_ORDER.indexOf(provider);
    return index === -1 ? PROVIDER_ORDER.length : index;
  };
  return accounts.sort(
    (a, b) =>
      rank(a.provider) - rank(b.provider) ||
      a.provider.localeCompare(b.provider) ||
      (a.devices[0] ?? "").localeCompare(b.devices[0] ?? "") ||
      a.key.localeCompare(b.key),
  );
}

export function isUsageLimitWarning(window: Pick<UsageLimitWindow, "used_percent">): boolean {
  return window.used_percent >= USAGE_LIMIT_WARNING_PERCENT;
}

// Early in a window a few requests extrapolate to a misleading forecast.
const PACE_MIN_ELAPSED_RATIO = 0.1;

export interface UsageLimitPace {
  /** Share of the window that has passed, 0–100. */
  elapsedPercent: number;
  /** When the limit is hit at the current rate, if that comes before the reset. */
  runsOutAt: number | null;
}

export function usageLimitPace(window: UsageLimitWindow, now: number = Date.now()): UsageLimitPace | null {
  if (window.window_minutes <= 0 || !window.resets_at) return null;
  const windowMs = window.window_minutes * 60_000;
  const resetsAt = time(window.resets_at);
  const elapsedMs = windowMs - (resetsAt - now);
  if (elapsedMs <= 0 || elapsedMs > windowMs) return null;

  let runsOutAt: number | null = null;
  const used = window.used_percent;
  if (used > 0 && used < 100 && elapsedMs >= windowMs * PACE_MIN_ELAPSED_RATIO) {
    const outAt = now + ((100 - used) * elapsedMs) / used;
    if (outAt < resetsAt) runsOutAt = outAt;
  }
  return { elapsedPercent: (elapsedMs / windowMs) * 100, runsOutAt };
}

/** The shortest window and the tightest of the rest, shortest first. */
export function usageLimitBarWindows(windows: UsageLimitWindow[]): UsageLimitWindow[] {
  if (windows.length <= 2) return [...windows].sort((a, b) => a.window_minutes - b.window_minutes);
  const shortest = windows.reduce((best, window) => (window.window_minutes < best.window_minutes ? window : best));
  const tightest = windows
    .filter((window) => window !== shortest)
    .reduce((best, window) => (window.used_percent > best.used_percent ? window : best));
  return [shortest, tightest].sort((a, b) => a.window_minutes - b.window_minutes);
}
