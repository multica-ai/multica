// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { RuntimeModel } from "@multica/core/types";
import { commonModels, commonThinkingLevels, sharedValue, thinkingLevelsFor } from "./squad-bulk-execution";

const LOW = { value: "low", label: "Low" };
const HIGH = { value: "high", label: "High" };
const MAX = { value: "max", label: "Max" };

const SONNET: RuntimeModel = {
  id: "sonnet",
  label: "Sonnet",
  thinking: { supported_levels: [LOW, HIGH] },
};
const OPUS: RuntimeModel = {
  id: "opus",
  label: "Opus",
  thinking: { supported_levels: [LOW, HIGH, MAX] },
};

describe("sharedValue", () => {
  it("returns the common value when every row agrees", () => {
    expect(sharedValue(["a", "a"])).toBe("a");
  });

  it("returns null (custom) when rows differ", () => {
    expect(sharedValue(["a", "b"])).toBeNull();
  });

  it("returns null when there are no rows", () => {
    expect(sharedValue([])).toBeNull();
  });

  it("treats the empty string as a real shared value (runtime default)", () => {
    expect(sharedValue(["", ""])).toBe("");
  });
});

describe("commonModels", () => {
  it("keeps only models every runtime offers, in the first catalog's order", () => {
    const first = [SONNET, OPUS];
    const second = [OPUS];
    expect(commonModels([first, second]).map((m) => m.id)).toEqual(["opus"]);
  });

  it("is empty while any runtime's catalog is not loaded", () => {
    expect(commonModels([[SONNET], undefined])).toEqual([]);
  });

  it("is empty when no runtime is in use", () => {
    expect(commonModels([])).toEqual([]);
  });
});

describe("thinkingLevelsFor", () => {
  it("returns the selected model's levels", () => {
    expect(thinkingLevelsFor([OPUS], "opus", "claude")).toEqual([LOW, HIGH, MAX]);
  });

  it("returns none for an unknown model or the runtime default", () => {
    expect(thinkingLevelsFor([OPUS], "missing", "claude")).toEqual([]);
    expect(thinkingLevelsFor([OPUS], "", "claude")).toEqual([]);
    expect(thinkingLevelsFor(undefined, "opus", "claude")).toEqual([]);
  });
});

describe("commonThinkingLevels", () => {
  it("keeps only levels every agent's model supports", () => {
    expect(commonThinkingLevels([[LOW, HIGH, MAX], [LOW, HIGH]]).map((l) => l.value)).toEqual(["low", "high"]);
  });

  it("is empty when one agent has no levels", () => {
    expect(commonThinkingLevels([[LOW, HIGH], []])).toEqual([]);
  });
});
