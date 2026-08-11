import { describe, expect, it } from "vitest";
import type { RuntimeModel } from "@multica/core/types";
import {
  findModelCapabilityEntry,
  modelIdForCapabilityLookup,
} from "./model-capability";

const CLAUDE_MODELS: RuntimeModel[] = [
  { id: "claude-opus-5", label: "Claude Opus 5" },
];

describe("model capability lookup", () => {
  it("inherits a base Claude model for valid context-window tags", () => {
    expect(modelIdForCapabilityLookup("claude", "claude-opus-5[1m]")).toBe(
      "claude-opus-5",
    );
    expect(modelIdForCapabilityLookup("claude", "claude-opus-5[500k]")).toBe(
      "claude-opus-5",
    );
    expect(
      findModelCapabilityEntry(
        CLAUDE_MODELS,
        "claude-opus-5[1m]",
        "claude",
      )?.id,
    ).toBe("claude-opus-5");
  });

  it("keeps malformed Claude tags and other providers exact", () => {
    expect(
      modelIdForCapabilityLookup("claude", "claude-opus-5[weird]"),
    ).toBe("claude-opus-5[weird]");
    expect(modelIdForCapabilityLookup("codex", "gpt-5.6-sol[1m]")).toBe(
      "gpt-5.6-sol[1m]",
    );
    expect(
      findModelCapabilityEntry(
        CLAUDE_MODELS,
        "claude-opus-5[weird]",
        "claude",
      ),
    ).toBeUndefined();
  });
});
