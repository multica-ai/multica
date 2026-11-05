import { describe, expect, it } from "vitest";
import { normalizeAgentTitleText } from "./agentTitleText";

describe("normalizeAgentTitleText", () => {
  const fixtures = [
    {
      name: "plain title",
      input: "  Hello\n world  ",
      normalized: "Hello world"
    },
    {
      name: "linked title",
      input: "Read [guide](/docs/guide.md)",
      normalized: "Read guide"
    },
    {
      name: "nested target",
      input: "[guide](/docs/(draft)/guide.md)",
      normalized: "guide"
    },
    {
      name: "incomplete link",
      input: "[guide](/docs",
      normalized: "[guide](/docs"
    },
    { name: "empty title", input: "  ", normalized: "" },
    { name: "escaped label", input: "[a\\]b](/docs)", normalized: "a]b" }
  ];

  it.each(fixtures)("normalizes $name", ({ input, normalized }) => {
    expect(normalizeAgentTitleText(input)).toBe(normalized);
  });
});
