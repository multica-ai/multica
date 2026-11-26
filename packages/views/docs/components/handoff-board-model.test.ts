// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  boardIssuePath,
  boardMarkdown,
  EMPTY_HANDOFF_BOARD,
  isHandoffBoard,
  linkedIssue,
  readBoard,
} from "./handoff-board-model";

describe("Doc board scene", () => {
  it("recognizes the complete versioned board, including a final newline", () => {
    expect(readBoard(EMPTY_HANDOFF_BOARD + "\n")?.elements).toEqual([]);
    expect(isHandoffBoard("A brief\n" + EMPTY_HANDOFF_BOARD)).toBe(false);
    expect(isHandoffBoard(EMPTY_HANDOFF_BOARD + "\nA brief")).toBe(false);
    expect(isHandoffBoard(EMPTY_HANDOFF_BOARD + "\n" + EMPTY_HANDOFF_BOARD)).toBe(false);
  });

  it("does not replace a malformed scene with an editable empty board", () => {
    expect(readBoard(boardMarkdown("not a scene"))).toBeNull();
    expect(readBoard(boardMarkdown('{"elements":[null]}'))).toBeNull();
  });

  it("restores the selected step's issue from a saved scene in its workspace only", () => {
    const issue = { id: "review", identifier: "OPS-12", workspace_id: "workspace-a" };
    const elements = [{ id: "step1", type: "rectangle", customData: { multicaIssue: issue } }];
    const step = readBoard(boardMarkdown(JSON.stringify({ elements })))?.elements?.[0];
    expect(linkedIssue(step?.customData, "workspace-b")).toBeNull();
    const target = linkedIssue(step?.customData, "workspace-a");
    expect(target).toEqual(issue);
    expect(boardIssuePath("release", target!)).toBe("/release/issues/OPS-12");
  });
});
