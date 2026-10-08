import { describe, expect, it } from "vitest";

import { runFailureBadgeLabel } from "@/lib/run-failure-badge";
import { AgentTaskSchema } from "./schemas";

function parseFailedTask(failure_reason: unknown) {
  return AgentTaskSchema.parse({
    id: "task-1",
    status: "failed",
    failure_reason,
  });
}

describe("AgentTaskSchema failure_reason (#7913 regression)", () => {
  // The field was a closed six-value enum with `.catch("")`, so every refined
  // reason the backend has written since MUL-1949 was erased to undefined
  // before any surface could read it. The badge map was widened in MUL-5370
  // and has been unreachable ever since — the parser, not the map, was the
  // reason a failed run said only "Failed".
  it("keeps a refined reason the backend classifies today", () => {
    expect(parseFailedTask("agent_error.provider_network").failure_reason).toBe(
      "agent_error.provider_network",
    );
    expect(parseFailedTask("environment_prepare_failed").failure_reason).toBe(
      "environment_prepare_failed",
    );
  });

  it("keeps a reason newer than this build", () => {
    // The taxonomy grows on the backend's cadence and installed builds lag it.
    // Dropping an unknown value here would re-create the dead-map bug the next
    // time a reason lands, which is the whole point of parsing it open.
    expect(parseFailedTask("some_future_reason").failure_reason).toBe(
      "some_future_reason",
    );
  });

  it("still normalizes the not-failed sentinel to undefined", () => {
    // Go's `omitempty` sends "" on a task that did not fail. Downstream truthy
    // checks depend on that becoming undefined rather than an empty string.
    expect(parseFailedTask("").failure_reason).toBeUndefined();
    expect(parseFailedTask(undefined).failure_reason).toBeUndefined();
    expect(parseFailedTask(42).failure_reason).toBeUndefined();
  });

  it("hands the run row a value its badge map can name", () => {
    // The end-to-end shape of the bug: parse, then look up. Either half alone
    // passed while the run row still rendered a bare "Failed".
    const task = parseFailedTask("environment_prepare_failed");
    expect(runFailureBadgeLabel(task.failure_reason)).toBe(
      "Environment setup failed",
    );
  });
});
