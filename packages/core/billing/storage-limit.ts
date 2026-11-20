import { queryOptions } from "@tanstack/react-query";
import { api, type ApiError } from "../api";
import type { StorageUsage } from "../types";

/**
 * Attachment storage limit (MUL-9489).
 *
 * A workspace may hold a limited number of attachment bytes, shared by every
 * member. When it is full the server refuses new uploads with
 * `403 storage_limit_reached`; attachments already stored stay readable. This
 * module reads the limit for Billing and turns a refusal into the facts the
 * upload notice shows.
 */
export const STORAGE_LIMIT_REACHED_CODE = "storage_limit_reached";

export const storageUsageKeys = {
  all: (wsId: string) => ["workspace-storage-usage", wsId] as const,
};

/** Refetched on focus: an upgrade happens outside this tab, in Stripe. */
export function storageUsageOptions(wsId: string) {
  return queryOptions({
    queryKey: storageUsageKeys.all(wsId),
    queryFn: () => api.getStorageUsage(wsId),
    enabled: wsId.length > 0,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

/** What a refused upload reports: the workspace's bytes and its limit. */
export interface StorageLimitReached {
  usedBytes: number | null;
  limitBytes: number | null;
}

/**
 * The storage facts of a refused upload, or null when error is anything
 * else. The figures are null when an older or unexpected body omits them.
 */
export function storageLimitReachedFromError(error: unknown): StorageLimitReached | null {
  // Read structurally rather than with `instanceof ApiError`: the error can
  // cross module copies (the upload coordinator, test doubles of the API).
  if (typeof error !== "object" || error === null) return null;
  const { status, body } = error as Partial<Pick<ApiError, "status" | "body">>;
  if (status !== 403) return null;
  if (typeof body !== "object" || body === null) return null;
  const value = body as { code?: unknown; used_bytes?: unknown; limit_bytes?: unknown };
  if (value.code !== STORAGE_LIMIT_REACHED_CODE) return null;
  const bytes = (v: unknown) =>
    typeof v === "number" && Number.isFinite(v) && v >= 0 ? v : null;
  return { usedBytes: bytes(value.used_bytes), limitBytes: bytes(value.limit_bytes) };
}

const STORAGE_UNITS = ["B", "KB", "MB", "GB", "TB"] as const;

/**
 * Storage in decimal units, as plans are sold: 1 GB is 10^9 bytes. Whole
 * numbers drop the decimal ("1 GB"); others keep one ("23.4 GB").
 */
export function formatStorageBytes(bytes: number, locale?: string): string {
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 1000 && unit < STORAGE_UNITS.length - 1) {
    value /= 1000;
    unit++;
  }
  let rounded = Math.round(value * 10) / 10;
  // 999.99 MB rounds to 1000 MB; show it as 1 GB instead.
  if (rounded >= 1000 && unit < STORAGE_UNITS.length - 1) {
    rounded = Math.round(rounded / 100) / 10;
    unit++;
  }
  const formatted = new Intl.NumberFormat(locale, {
    maximumFractionDigits: Number.isInteger(rounded) ? 0 : 1,
  }).format(rounded);
  return `${formatted} ${STORAGE_UNITS[unit]}`;
}

/** The share of the limit in use, 0–100, for a progress bar. */
export function storageUsageProgress(usedBytes: number, limitBytes: number): number {
  if (limitBytes <= 0) return 100;
  return Math.min(100, (usedBytes / limitBytes) * 100);
}

/** The Billing row for attachment storage. */
export type StorageRow =
  | { kind: "metered"; usedBytes: number; limitBytes: number; reached: boolean }
  | { kind: "unavailable"; limitBytes: number | null };

/**
 * What Billing shows for attachment storage, or null when no limit applies.
 *
 * The limit comes from the entitlement snapshot when it has one: it is the
 * number Cloud just computed for the plan, so a page that has just upgraded
 * or added seats shows the new limit at once, while the usage read can still
 * carry a policy cached before the change. Usage that could not be read (a
 * failed request, or a sum the server gave up on) shows as unavailable.
 */
export function resolveStorageRow(
  usage: StorageUsage | undefined,
  usageFailed: boolean,
  entitlementLimit: { mode: string; limit: number | null } | null | undefined,
): StorageRow | null {
  const planLimit = entitlementLimit?.mode === "limited" ? entitlementLimit.limit : null;
  if (usageFailed || !usage) {
    return usageFailed && planLimit !== null ? { kind: "unavailable", limitBytes: planLimit } : null;
  }
  if (usage.action === "off" || usage.limitBytes === null) return null;
  const limitBytes = planLimit ?? usage.limitBytes;
  if (usage.usedBytes === null) return { kind: "unavailable", limitBytes };
  return {
    kind: "metered",
    usedBytes: usage.usedBytes,
    limitBytes,
    // Only an enforced limit is ever reached; observe never refuses.
    reached: usage.action === "enforce" && usage.usedBytes >= limitBytes,
  };
}
