// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { isAgentStuck } from "./team-status";

const detail = (overrides: Partial<AgentPresenceDetail>) =>
  ({ availability: "online", workload: "idle", runningCount: 0, queuedCount: 0, ...overrides }) as AgentPresenceDetail;

describe("isAgentStuck", () => {
  it("flags queued work on a runtime that is not online", () => {
    expect(isAgentStuck(detail({ availability: "offline", queuedCount: 2 }))).toBe(true);
  });

  // On an online runtime a queued task is just waiting to be claimed.
  it("does not flag queued work on an online runtime", () => {
    expect(isAgentStuck(detail({ availability: "online", queuedCount: 2 }))).toBe(false);
  });

  it("does not flag an offline agent with nothing queued", () => {
    expect(isAgentStuck(detail({ availability: "offline", queuedCount: 0 }))).toBe(false);
    expect(isAgentStuck(undefined)).toBe(false);
  });
});
