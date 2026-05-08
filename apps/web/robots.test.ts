import { describe, expect, it } from "vitest";
import robots from "./app/robots";

describe("robots", () => {
  it("disallows realtime transport endpoints", () => {
    const config = robots();
    const rules = Array.isArray(config.rules) ? config.rules[0] : config.rules;
    expect(rules?.disallow).toEqual(expect.arrayContaining(["/ws", "/sse"]));
  });
});
