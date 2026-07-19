// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { CerebroWorkflow, LoopChainSpec } from "../core/types";
import {
  MachineControlNotice,
  chainFormFromSpec,
  chainSpecFromForm,
} from "./workflow-issue-loop-chain";
import { formFromWorkflow } from "./workflow-issue-loop-form";

const apiChain: LoopChainSpec = {
  version: 2,
  done_status: "done",
  phases: [
    {
      id: "build",
      name: "Build and verify",
      status: "in_progress",
      limits: {
        max_steps: 9,
        max_rounds: 3,
        no_progress_stalls: 2,
        max_wait_seconds: 420,
      },
      blocks: [
        {
          id: "implement",
          type: "session",
          name: "Build",
          agents: [{ agent_id: "agent-a" }, { agent_id: "agent-b" }],
          on_all_busy: "wait",
          skill: "implementation",
          goal: "Build the approved scope.",
          steps: { allowed: true, max: 4 },
        },
        {
          id: "quality",
          type: "eval",
          name: "Quality eval",
          eval_key: "delivery-quality",
        },
      ],
    },
  ],
};

describe("Issue workflow chain editor", () => {
  it("round-trips an API chain without flattening limits or block configuration", () => {
    expect(chainSpecFromForm(chainFormFromSpec(apiChain))).toEqual(apiChain);
  });

  it("shows information when the chain has no machine-controlled block", () => {
    const chain: LoopChainSpec = {
      version: 2,
      phases: [{
        id: "review",
        limits: { max_steps: 2, max_rounds: 1, no_progress_stalls: 1 },
        blocks: [{ id: "human-review", type: "human", prompt: "Approve the result" }],
      }],
    };

    render(<MachineControlNotice chain={chain} />);

    expect(screen.getByText("This chain has no machine-controlled block")).toBeVisible();
    expect(screen.getByText(/informational/i)).toBeVisible();
  }, 15_000);

  it("marks a legacy recipe as read-only instead of replacing it with a blank chain", () => {
    const workflow = {
      id: "legacy",
      workspace_id: "workspace",
      name: "Legacy recipe",
      enabled: true,
      trigger_type: "manual",
      trigger_config: {},
      conditions: [],
      action_type: "noop",
      action_config: {},
      editor_mode: "form",
      created_by_id: "member",
      created_by_type: "member",
      created_at: "2026-07-18T00:00:00Z",
      updated_at: "2026-07-18T00:00:00Z",
      workflow_type: "issue_loop",
      loop_spec: {
        version: 1 as const,
        verification: [],
        caps: { max_iterations: 3, max_revisions: 2, no_progress_stalls: 1 },
        build_agent_id: "",
        build_skill: "build",
      },
    };

    expect(formFromWorkflow(workflow as unknown as CerebroWorkflow).legacy).toBe(true);
  });
});
