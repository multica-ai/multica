import { describe, expect, it } from "vitest";
import { preprocessMentionShortcodes as preprocessCoreMentionShortcodes } from "@multica/core/markdown";
import { preprocessMentionShortcodes as preprocessUiMentionShortcodes } from "@multica/ui/markdown";

const cases = [
  {
    name: "plain markdown",
    input: "No legacy mentions here.",
    expected: "No legacy mentions here.",
  },
  {
    name: "single legacy mention",
    input: 'Hello [@ id="550e8400-e29b-41d4-a716-446655440000" label="Ada Lovelace"]',
    expected:
      "Hello [@Ada Lovelace](mention://member/550e8400-e29b-41d4-a716-446655440000)",
  },
  {
    name: "attributes in any order",
    input: 'Owner: [@ label="Grace Hopper" id="member-123" extra="ignored"]',
    expected: "Owner: [@Grace Hopper](mention://member/member-123)",
  },
  {
    name: "multiple mentions",
    input: 'Pair [@ id="u1" label="Alice"] and [@ id="u2" label="Bob"]',
    expected:
      "Pair [@Alice](mention://member/u1) and [@Bob](mention://member/u2)",
  },
  {
    name: "missing id remains unchanged",
    input: 'Broken [@ label="Alice"] shortcode',
    expected: 'Broken [@ label="Alice"] shortcode',
  },
  {
    name: "missing label remains unchanged",
    input: 'Broken [@ id="u1"] shortcode',
    expected: 'Broken [@ id="u1"] shortcode',
  },
  {
    name: "modern mention link remains unchanged",
    input: "Already [@Alice](mention://member/u1)",
    expected: "Already [@Alice](mention://member/u1)",
  },
];

describe("mention shortcode preprocessing parity", () => {
  it.each(cases)("$name", ({ input, expected }) => {
    expect(preprocessCoreMentionShortcodes(input)).toBe(expected);
    expect(preprocessUiMentionShortcodes(input)).toBe(expected);
    expect(preprocessUiMentionShortcodes(input)).toBe(
      preprocessCoreMentionShortcodes(input),
    );
  });

  it("stays idempotent in both copies", () => {
    const input =
      'First [@ id="u1" label="Alice"] then [@ id="u2" label="Bob"]';

    const coreOnce = preprocessCoreMentionShortcodes(input);
    const uiOnce = preprocessUiMentionShortcodes(input);

    expect(preprocessCoreMentionShortcodes(coreOnce)).toBe(coreOnce);
    expect(preprocessUiMentionShortcodes(uiOnce)).toBe(uiOnce);
  });
});
