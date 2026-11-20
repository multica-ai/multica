// @vitest-environment node
import { describe, expect, it } from "vitest";

import { ApiError } from "../api";
import { StorageUsageSchema } from "../api/schemas";
import type { StorageUsage } from "../types";
import {
  formatStorageBytes,
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

  it("treats missing usage of an active limit as zero and a missing reached as false", () => {
    expect(StorageUsageSchema.parse({ action: "enforce", limit_bytes: 10 })).toEqual({
      action: "enforce",
      usedBytes: 0,
      limitBytes: 10,
      reached: false,
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
  });
});

describe("storageUsageProgress", () => {
  it("is the share of the limit in use, capped at 100", () => {
    expect(storageUsageProgress({ action: "observe", usedBytes: 250, limitBytes: 1000, reached: null })).toBe(25);
    expect(storageUsageProgress({ action: "observe", usedBytes: 2000, limitBytes: 1000, reached: null })).toBe(100);
    expect(storageUsageProgress(OFF)).toBe(0);
  });
});
