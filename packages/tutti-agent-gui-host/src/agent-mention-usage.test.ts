// @vitest-environment node
import { expect, it } from "vitest";
import { parseAgentMentionUsage } from "./agent-mention-usage";

it.each([null, "{", "null", '{"version":2,"agents":{}}', '{"version":1,"agents":[]}'])("ignores unsupported or malformed usage: %s", (raw) => {
  expect(parseAgentMentionUsage(raw)).toEqual({});
});

it("keeps valid counts when a local record contains invalid entries", () => {
  expect(parseAgentMentionUsage(JSON.stringify({ version: 1, agents: {
    valid: { count: 3, lastUsedAt: 100 }, negative: { count: -1, lastUsedAt: 100 },
    fractional: { count: 1.5, lastUsedAt: 100 }, badTime: { count: 1, lastUsedAt: "100" }, empty: null,
  } }))).toEqual({ valid: { count: 3, lastUsedAt: 100 } });
});
