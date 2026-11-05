// @vitest-environment node
import { expect, it } from "vitest";
import { predicateDefinition } from "../contracts/resources.js";
it("advertises only implemented temporal modes and validates typed predicate inputs", () => {
  const errors = predicateDefinition("diagnostic.errorCount");
  expect(errors.temporal).toEqual(["throughout"]);
  const count = predicateDefinition("queue.itemCount");
  expect(count.temporal).not.toContain("throughout");
  expect(
    count.schema.safeParse({
      kind: "queue.itemCount",
      operator: "eq",
      expected: "0",
    }).success,
  ).toBe(false);
  const turn = predicateDefinition("turn.status");
  expect(
    turn.schema.safeParse({
      kind: "turn.status",
      operator: "eq",
      expected: "completed",
    }).success,
  ).toBe(false);
  expect(
    turn.schema.safeParse({
      kind: "turn.status",
      operator: "eq",
      expected: "completed",
      target: "specific-turn",
    }).success,
  ).toBe(true);
});
