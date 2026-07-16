// FIR-3212 — contract tests for the runtime_options section of the
// capabilities-card schema. Per the API Response Compatibility rules, a
// malformed or missing section must downgrade to the honest "unknown" state,
// never throw into the UI or silently read as "supports nothing".

import { describe, it, expect } from "vitest";
import { AgentCapabilitiesSchema } from "./api";

describe("AgentCapabilitiesSchema runtime_options", () => {
  it("parses a well-formed runtime_options section", () => {
    const parsed = AgentCapabilitiesSchema.parse({
      runtime_options: {
        status: "known",
        provider: "claude",
        cli_version: "2.1.209",
        runtime_id: "rt-1",
        exec_options: [
          { field: "model", handling: "honoured", effective: true },
          {
            field: "disallowed_mcp_tools",
            handling: "ignored_silent",
            effective: false,
          },
        ],
        silently_ignored: ["disallowed_mcp_tools"],
        system_prompt: { native: true, modes: ["append", "replace"] },
      },
    });
    expect(parsed.runtime_options.status).toBe("known");
    expect(parsed.runtime_options.exec_options).toHaveLength(2);
    expect(parsed.runtime_options.system_prompt?.native).toBe(true);
    expect(parsed.runtime_options.system_prompt?.modes).toContain("replace");
  });

  it("defaults a missing runtime_options to status=unknown, not empty support", () => {
    const parsed = AgentCapabilitiesSchema.parse({});
    expect(parsed.runtime_options.status).toBe("unknown");
    expect(parsed.runtime_options.exec_options).toEqual([]);
    expect(parsed.runtime_options.system_prompt).toBeUndefined();
  });

  it("rejects malformed fields so parseWithFallback returns the fallback", () => {
    // zod .default() only covers missing keys. Wrong types must fail
    // validation cleanly — parseWithFallback catches that and returns the
    // explicit fallback, so a drifting backend downgrades instead of
    // mis-coercing garbage into the UI.
    const result = AgentCapabilitiesSchema.safeParse({
      runtime_options: {
        status: 42,
        exec_options: [{ field: null, handling: 7, effective: "yes" }],
        silently_ignored: "not-a-list",
        system_prompt: { native: "true", modes: "append" },
      },
    });
    expect(result.success).toBe(false);
  });
});
