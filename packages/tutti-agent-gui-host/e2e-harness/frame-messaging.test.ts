// @vitest-environment node
import { describe, expect, it } from "vitest";
import { agentGuiMessageTargetOrigin, isAgentGuiFrameMessage } from "./frame-messaging";

const source = {} as Window;
const otherSource = {} as Window;

describe("Agent GUI frame message boundaries", () => {
  it.each(["null", "file://"])("accepts a file message serialized as %s only from its owning window", (origin) => {
    const location = { protocol: "file:", origin: "file://" };
    expect(isAgentGuiFrameMessage({ source, origin }, source, location)).toBe(true);
    expect(isAgentGuiFrameMessage({ source: otherSource, origin }, source, location)).toBe(false);
    expect(isAgentGuiFrameMessage({ source: null, origin }, null, location)).toBe(false);
    expect(agentGuiMessageTargetOrigin(location)).toBe("*");
  });

  it.each(["https://untrusted.invalid", "http://localhost:3000", "", "file:///tmp/other.html"])("rejects an unrelated origin %s in a file host", (origin) => {
    expect(isAgentGuiFrameMessage({ source, origin }, source, { protocol: "file:", origin: "file://" })).toBe(false);
  });

  it.each(["http://localhost:3000", "https://multica.example"])("preserves exact origin matching for %s", (origin) => {
    const location = { protocol: new URL(origin).protocol, origin };
    expect(isAgentGuiFrameMessage({ source, origin }, source, location)).toBe(true);
    expect(isAgentGuiFrameMessage({ source: otherSource, origin }, source, location)).toBe(false);
    for (const untrusted of ["null", "file://", "https://untrusted.invalid", `${origin}:9999`]) {
      expect(isAgentGuiFrameMessage({ source, origin: untrusted }, source, location)).toBe(false);
    }
    expect(agentGuiMessageTargetOrigin(location)).toBe(origin);
  });

  it("does not trust arbitrary opaque pages as a local file host", () => {
    expect(isAgentGuiFrameMessage({ source, origin: "null" }, source, { protocol: "data:", origin: "null" })).toBe(false);
  });
});
