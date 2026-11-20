// @vitest-environment node
import { describe, expect, it } from "vitest";

import { ApiError } from "../api";
import { StorageUsageSchema } from "../api/schemas";
import type { StorageUsage } from "../types";
import {
  formatStorageBytes,
  resolveStorageRow,
  storageLimitReachedFromError,
  storageUsageProgress,
} from "./storage-limit";

const OFF: StorageUsage = { action: "off", usedBytes: null, limitBytes: null, reached: null };

describe("StorageUsageSchema", () => {
  it("reads an enforced limit with its figures", () => {
    expect(
      StorageUsageSchema.parse({ action: "enforce", used_bytes: 1_000_000_000, limit_bytes: 1_000_000_000, reached: true }),
    ).toEqual({ action: "enforce", usedBytes: 1_000_000_000, limitBytes: 1_000_000_000, reached: true });
  });

  it("never reports an observed limit as reached", () => {
    expect(StorageUsageSchema.parse({ action: "observe", used_bytes: 5, limit_bytes: 1, reached: true })).toEqual({
      action: "observe",
      usedBytes: 5,
      limitBytes: 1,
      reached: null,
    });
  });

  it("reads off, an unknown action, or a limit without a value as no limit", () => {
    expect(StorageUsageSchema.parse({ action: "off" })).toEqual(OFF);
    expect(StorageUsageSchema.parse({ action: "block", limit_bytes: 5 })).toEqual(OFF);
    expect(StorageUsageSchema.parse({ action: "enforce", limit_bytes: "5" })).toEqual(OFF);
  });

  it("keeps the limit and leaves usage unknown when the server could not read it", () => {
    expect(StorageUsageSchema.parse({ action: "enforce", limit_bytes: 10 })).toEqual({
      action: "enforce",
      usedBytes: null,
      limitBytes: 10,
      reached: null,
    });
  });
});

describe("storageLimitReachedFromError", () => {
  const refusal = (body: unknown, status = 403) => new ApiError("refused", status, "Forbidden", body);

  it("reads the figures of a storage refusal", () => {
    expect(
      storageLimitReachedFromError(refusal({ code: "storage_limit_reached", used_bytes: 900, limit_bytes: 1000 })),
    ).toEqual({ usedBytes: 900, limitBytes: 1000 });
  });

  it("keeps a refusal whose figures are missing or malformed", () => {
    expect(storageLimitReachedFromError(refusal({ code: "storage_limit_reached", used_bytes: "900" }))).toEqual({
      usedBytes: null,
      limitBytes: null,
    });
  });

  it("ignores every other failure", () => {
    expect(storageLimitReachedFromError(refusal({ code: "feature_locked" }))).toBeNull();
    expect(storageLimitReachedFromError(refusal({ code: "storage_limit_reached" }, 413))).toBeNull();
    expect(storageLimitReachedFromError(new Error("storage_limit_reached"))).toBeNull();
    expect(storageLimitReachedFromError(refusal(undefined))).toBeNull();
  });
});

describe("formatStorageBytes", () => {
  it("uses decimal units, as plans are sold", () => {
    expect(formatStorageBytes(1_000_000_000, "en")).toBe("1 GB");
    expect(formatStorageBytes(23_400_000_000, "en")).toBe("23.4 GB");
    expect(formatStorageBytes(120_000_000_000, "en")).toBe("120 GB");
    expect(formatStorageBytes(48_000_000, "en")).toBe("48 MB");
    expect(formatStorageBytes(999, "en")).toBe("999 B");
    expect(formatStorageBytes(2_500_000_000_000, "en")).toBe("2.5 TB");
    // Rounding up to the next unit names that unit.
    expect(formatStorageBytes(999_990_000, "en")).toBe("1 GB");
    expect(formatStorageBytes(999_940_000, "en")).toBe("999.9 MB");
  });
});

describe("storageUsageProgress", () => {
  it("is the share of the limit in use, capped at 100", () => {
    expect(storageUsageProgress(250, 1000)).toBe(25);
    expect(storageUsageProgress(2000, 1000)).toBe(100);
    expect(storageUsageProgress(0, 0)).toBe(100);
  });
});

describe("resolveStorageRow", () => {
  const usage = (over: Partial<StorageUsage>): StorageUsage => ({
    action: "enforce",
    usedBytes: 900,
    limitBytes: 1000,
    reached: false,
    ...over,
  });
  const plan = (limit: number) => ({ mode: "limited", limit });

  it("meters usage against the limit, and only an enforced limit is reached", () => {
    expect(resolveStorageRow(usage({}), false, null)).toEqual({
      kind: "metered",
      usedBytes: 900,
      limitBytes: 1000,
      reached: false,
    });
    expect(resolveStorageRow(usage({ usedBytes: 1000 }), false, null)).toMatchObject({ reached: true });
    expect(resolveStorageRow(usage({ action: "observe", usedBytes: 5000 }), false, null)).toMatchObject({
      reached: false,
    });
  });

  // Right after an upgrade the usage read can still carry the policy cached
  // before it; the entitlement has the new limit.
  it("takes the limit from the plan when the entitlement has one", () => {
    expect(resolveStorageRow(usage({ usedBytes: 1000 }), false, plan(100_000))).toEqual({
      kind: "metered",
      usedBytes: 1000,
      limitBytes: 100_000,
      reached: false,
    });
  });

  it("shows usage the server could not read, or a failed read, as unavailable", () => {
    expect(resolveStorageRow(usage({ usedBytes: null, reached: null }), false, null)).toEqual({
      kind: "unavailable",
      limitBytes: 1000,
    });
    expect(resolveStorageRow(undefined, true, plan(1000))).toEqual({ kind: "unavailable", limitBytes: 1000 });
  });

  it("shows nothing without a limit", () => {
    expect(resolveStorageRow(OFF, false, plan(1000))).toBeNull();
    expect(resolveStorageRow(undefined, false, plan(1000))).toBeNull();
    // A failed read with no plan limit says nothing about whether one applies.
    expect(resolveStorageRow(undefined, true, null)).toBeNull();
  });
});
