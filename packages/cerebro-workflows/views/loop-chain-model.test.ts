import { describe, expect, it } from "vitest";

import type { LoopChainBlock, LoopChainPhase } from "../core/types";
import { blockSummary, nudgeBlock, reorderBlocks, totalSteps } from "./loop-chain-model";

function block(id: string, type: LoopChainBlock["type"] = "session"): LoopChainBlock {
  return { id, type, name: id };
}

function phase(id: string, blockIds: string[]): LoopChainPhase {
  return {
    id,
    name: id,
    blocks: blockIds.map((bid) => block(bid)),
    limits: { max_steps: 8, max_rounds: 3, no_progress_stalls: 2 },
  };
}

const ids = (phases: LoopChainPhase[]) => phases.map((p) => p.blocks.map((b) => b.id));

describe("reorderBlocks", () => {
  it("moves a block within a phase, dropping above the target", () => {
    const phases = [phase("p1", ["a", "b", "c"])];
    expect(ids(reorderBlocks(phases, "c", "a", true))).toEqual([["c", "a", "b"]]);
  });

  it("moves a block within a phase, dropping below the target", () => {
    const phases = [phase("p1", ["a", "b", "c"])];
    expect(ids(reorderBlocks(phases, "a", "c", false))).toEqual([["b", "c", "a"]]);
  });

  it("moves a block across phases", () => {
    const phases = [phase("p1", ["a", "b"]), phase("p2", ["c"])];
    expect(ids(reorderBlocks(phases, "a", "c", true))).toEqual([["b"], ["a", "c"]]);
  });

  it("never empties a phase — moving the only block out is a no-op", () => {
    const phases = [phase("p1", ["a"]), phase("p2", ["b", "c"])];
    expect(ids(reorderBlocks(phases, "a", "b", true))).toEqual([["a"], ["b", "c"]]);
  });

  it("is a no-op when dropping onto itself", () => {
    const phases = [phase("p1", ["a", "b"])];
    expect(reorderBlocks(phases, "a", "a", true)).toBe(phases);
  });

  it("does not mutate the input", () => {
    const phases = [phase("p1", ["a", "b", "c"])];
    reorderBlocks(phases, "a", "c", false);
    expect(ids(phases)).toEqual([["a", "b", "c"]]);
  });
});

describe("nudgeBlock", () => {
  it("moves a block up within its phase", () => {
    const phases = [phase("p1", ["a", "b", "c"])];
    expect(ids(nudgeBlock(phases, "b", -1))).toEqual([["b", "a", "c"]]);
  });

  it("moves a block down within its phase", () => {
    const phases = [phase("p1", ["a", "b", "c"])];
    expect(ids(nudgeBlock(phases, "b", 1))).toEqual([["a", "c", "b"]]);
  });

  it("clamps at the ends", () => {
    const phases = [phase("p1", ["a", "b"])];
    expect(nudgeBlock(phases, "a", -1)).toBe(phases);
    expect(nudgeBlock(phases, "b", 1)).toBe(phases);
  });
});

describe("blockSummary", () => {
  it("summarises a session with skill and agents in plain language", () => {
    expect(blockSummary({ id: "s", type: "session", skill: "delivery", agents: [{ agent_id: "x" }, { agent_id: "y" }] }))
      .toBe("Skill delivery · 2 agents");
  });

  it("falls back to a description when a session has no skill", () => {
    expect(blockSummary({ id: "s", type: "session" })).toBe("An agent runs a skill on the issue");
  });

  it("summarises a command", () => {
    expect(blockSummary({ id: "c", type: "command", check: ["make", "test"] })).toBe("make test · must pass");
  });

  it("summarises human and eval steps", () => {
    expect(blockSummary({ id: "h", type: "human" })).toBe("Waits for a person to approve");
    expect(blockSummary({ id: "e", type: "eval", eval_key: "delivery-quality" })).toBe("Server scores delivery-quality");
  });
});

describe("totalSteps", () => {
  it("counts blocks across every phase", () => {
    expect(totalSteps({ version: 2, phases: [phase("p1", ["a", "b"]), phase("p2", ["c"])] })).toBe(3);
  });
});
