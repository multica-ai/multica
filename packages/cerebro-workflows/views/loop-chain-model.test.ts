import { describe, expect, it } from "vitest";

import type { LoopChainBlock, LoopChainPhase } from "../core/types";
import {
  applyCommandSelection,
  blockSummary,
  nudgeBlock,
  nudgePhase,
  reorderBlocks,
  reorderPhases,
  skillNamesFromPrompt,
  totalSteps,
} from "./loop-chain-model";

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

describe("skillNamesFromPrompt", () => {
  it("resolves multiple references, removes duplicates, and ignores stale ids", () => {
    const prompt = "Use [/plan](slash://skill/a), [/review](slash://skill/b), [/plan](slash://skill/a), and [/missing](slash://skill/x).";
    expect(skillNamesFromPrompt(prompt, [{ id: "a", name: "plan" }, { id: "b", name: "review" }])).toEqual(["plan", "review"]);
  });
});

describe("applyCommandSelection", () => {
  it("snapshots the selected library command as executable argv", () => {
    const selected = applyCommandSelection(block("tests", "command"), { id: "command-1", argv: ["pnpm", "test"] });
    expect(selected).toMatchObject({ command_id: "command-1", check: ["pnpm", "test"], expect: "exit_zero" });
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

describe("reorderPhases", () => {
  const phaseIds = (phases: LoopChainPhase[]) => phases.map((p) => p.id);

  it("moves a phase above the target", () => {
    const phases = [phase("p1", ["a"]), phase("p2", ["b"]), phase("p3", ["c"])];
    expect(phaseIds(reorderPhases(phases, "p3", "p1", true))).toEqual(["p3", "p1", "p2"]);
  });

  it("moves a phase below the target", () => {
    const phases = [phase("p1", ["a"]), phase("p2", ["b"]), phase("p3", ["c"])];
    expect(phaseIds(reorderPhases(phases, "p1", "p3", false))).toEqual(["p2", "p3", "p1"]);
  });

  it("keeps every block with its phase", () => {
    const phases = [phase("p1", ["a", "b"]), phase("p2", ["c"])];
    expect(ids(reorderPhases(phases, "p2", "p1", true))).toEqual([["c"], ["a", "b"]]);
  });

  it("is a no-op when dropping onto itself or an unknown phase", () => {
    const phases = [phase("p1", ["a"]), phase("p2", ["b"])];
    expect(reorderPhases(phases, "p1", "p1", true)).toBe(phases);
    expect(reorderPhases(phases, "p1", "nope", true)).toBe(phases);
  });

  it("does not mutate the input", () => {
    const phases = [phase("p1", ["a"]), phase("p2", ["b"])];
    reorderPhases(phases, "p1", "p2", false);
    expect(phaseIds(phases)).toEqual(["p1", "p2"]);
  });
});

describe("nudgePhase", () => {
  const phaseIds = (phases: LoopChainPhase[]) => phases.map((p) => p.id);

  it("moves a phase up and down", () => {
    const phases = [phase("p1", ["a"]), phase("p2", ["b"]), phase("p3", ["c"])];
    expect(phaseIds(nudgePhase(phases, "p2", -1))).toEqual(["p2", "p1", "p3"]);
    expect(phaseIds(nudgePhase(phases, "p2", 1))).toEqual(["p1", "p3", "p2"]);
  });

  it("clamps at the ends", () => {
    const phases = [phase("p1", ["a"]), phase("p2", ["b"])];
    expect(nudgePhase(phases, "p1", -1)).toBe(phases);
    expect(nudgePhase(phases, "p2", 1)).toBe(phases);
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
