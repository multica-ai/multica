// @vitest-environment node
import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { buildQuickActionBody } from "./body";

const cases = JSON.parse(readFileSync(
  new URL("../../../server/internal/handler/testdata/quick_action_bodies.json", import.meta.url),
  "utf8",
)) as { name: string; action: Omit<Parameters<typeof buildQuickActionBody>[0], "target_missing">; expected: string }[];

describe("buildQuickActionBody", () => {
  // Also consumed by the server's body builder contract test.
  it.each(cases)("matches the server body: $name", ({ action, expected }) => {
    expect(buildQuickActionBody({ ...action, target_missing: false })).toBe(expected);
  });

  const action = {
    assignee_type: "agent",
    assignee_id: "agent-id",
    target_name: "Reviewer",
    target_missing: false,
    prompt: "Review this",
  };

  it.each([
    { target_missing: true },
    { target_name: undefined },
    { target_name: "" },
    { assignee_id: "" },
    { assignee_type: "unknown" },
  ])("refuses an unresolved target: %o", (overrides) => {
    expect(buildQuickActionBody({ ...action, ...overrides })).toBeNull();
  });
});
