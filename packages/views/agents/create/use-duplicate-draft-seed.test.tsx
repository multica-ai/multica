// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook } from "@testing-library/react";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import type { AgentDraft } from "@multica/core/agents";
import { useDuplicateDraftSeed } from "./use-duplicate-draft-seed";

const SOURCE_RUNTIME = {
  id: "runtime-1",
  name: "Codex laptop",
  provider: "codex",
  status: "online",
  owner_id: "user-1",
  visibility: "private",
} as RuntimeDevice;

const OTHER_RUNTIME = {
  ...SOURCE_RUNTIME,
  id: "runtime-2",
  name: "Spare laptop",
} as RuntimeDevice;

const SOURCE = {
  id: "agent-1",
  runtime_id: "runtime-1",
  name: "Fast Codex",
  description: "",
  instructions: "",
  avatar_url: null,
  custom_args: [],
  permission_mode: "private",
  invocation_targets: [],
  max_concurrent_tasks: 6,
  model: "gpt-5.6-sol",
  thinking_level: "high",
  service_tier: "priority",
  skills: [],
} as unknown as Agent;

function setup(initial: { runtimesSettled: boolean; runtimes: RuntimeDevice[] }) {
  const onSeed = vi.fn<(draft: AgentDraft, runtimeReset: boolean) => void>();
  const view = renderHook(
    (props: { runtimesSettled: boolean; runtimes: RuntimeDevice[] }) =>
      useDuplicateDraftSeed({
        source: SOURCE,
        runtimesSettled: props.runtimesSettled,
        runtimes: props.runtimes,
        currentUserId: "user-1",
        fallbackRuntimeId: props.runtimes[0]?.id ?? "",
        nameSuffix: " copy",
        onSeed,
      }),
    { initialProps: initial },
  );
  return { onSeed, view };
}

// Regression for the cold-start ordering bug: the agent list and the runtime
// list are independent queries, so on a direct ?duplicate=<id> link the agent
// can arrive while runtimes are still pending. Seeding then judged the source
// runtime "unavailable" and permanently cleared model / thinking / speed.
describe("useDuplicateDraftSeed", () => {
  afterEach(cleanup);

  it("waits for the runtime query before seeding", () => {
    const { onSeed, view } = setup({ runtimesSettled: false, runtimes: [] });

    expect(onSeed).not.toHaveBeenCalled();

    // Runtimes resolve late, still holding the source runtime.
    view.rerender({ runtimesSettled: true, runtimes: [SOURCE_RUNTIME] });

    expect(onSeed).toHaveBeenCalledTimes(1);
    const [draft, runtimeReset] = onSeed.mock.calls[0]!;
    expect(draft).toMatchObject({
      runtimeId: "runtime-1",
      model: "gpt-5.6-sol",
      thinkingLevel: "high",
      serviceTier: "priority",
    });
    expect(runtimeReset).toBe(false);
  });

  it("seeds exactly once, so later runtime updates cannot overwrite edits", () => {
    const { onSeed, view } = setup({
      runtimesSettled: true,
      runtimes: [SOURCE_RUNTIME],
    });

    expect(onSeed).toHaveBeenCalledTimes(1);
    view.rerender({
      runtimesSettled: true,
      runtimes: [SOURCE_RUNTIME, OTHER_RUNTIME],
    });

    expect(onSeed).toHaveBeenCalledTimes(1);
  });

  it("reports the reset when the settled list cannot serve the source runtime", () => {
    const { onSeed } = setup({
      runtimesSettled: true,
      runtimes: [OTHER_RUNTIME],
    });

    expect(onSeed).toHaveBeenCalledTimes(1);
    const [draft, runtimeReset] = onSeed.mock.calls[0]!;
    expect(draft).toMatchObject({
      runtimeId: "runtime-2",
      model: "",
      thinkingLevel: "",
      serviceTier: "",
    });
    expect(runtimeReset).toBe(true);
  });

  // A failed runtime query is a decidable answer: the source runtime cannot be
  // confirmed, so falling back is correct — unlike a pending one.
  it("seeds on a failed runtime query rather than hanging", () => {
    const { onSeed } = setup({ runtimesSettled: true, runtimes: [] });

    expect(onSeed).toHaveBeenCalledTimes(1);
    expect(onSeed.mock.calls[0]![1]).toBe(true);
  });
});
