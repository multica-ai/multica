// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { RuntimeUpdate } from "../types/agent";
import {
  UPDATE_FAST_POLL_MS,
  UPDATE_SCHEDULED_POLL_MS,
  knownRuntimeUpdateStatus,
  updatePollInterval,
} from "./updates";

function update(status: string): RuntimeUpdate {
  return {
    id: "upd-1",
    runtime_id: "rt-1",
    status: status as RuntimeUpdate["status"],
    target_version: "v0.9.2",
    created_at: "",
    updated_at: "",
  };
}

describe("updatePollInterval", () => {
  it.each([
    ["pending", UPDATE_FAST_POLL_MS],
    ["running", UPDATE_FAST_POLL_MS],
    ["scheduled", UPDATE_SCHEDULED_POLL_MS],
  ])("polls a %s update", (status, interval) => {
    expect(updatePollInterval(update(status))).toBe(interval);
  });

  it.each(["completed", "failed", "timeout", "cancelled", "expired", "draining"])(
    "stops polling a %s update",
    (status) => {
      expect(updatePollInterval(update(status))).toBe(false);
    },
  );

  it("does not poll without an update", () => {
    expect(updatePollInterval(null)).toBe(false);
    expect(updatePollInterval(undefined)).toBe(false);
  });
});

describe("knownRuntimeUpdateStatus", () => {
  it("returns null for a status from a newer server", () => {
    expect(knownRuntimeUpdateStatus("draining")).toBeNull();
    expect(knownRuntimeUpdateStatus(undefined)).toBeNull();
    expect(knownRuntimeUpdateStatus("scheduled")).toBe("scheduled");
  });
});
