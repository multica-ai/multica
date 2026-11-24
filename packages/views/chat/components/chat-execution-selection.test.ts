// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { RuntimeModel } from "@multica/core/types";
import { nextChatModelSelection } from "./chat-execution-selection";

const models: RuntimeModel[] = [
  { id: "a", label: "A", default: true, thinking: { supported_levels: [{ value: "low", label: "Low" }] } },
  { id: "b", label: "B", thinking: { supported_levels: [{ value: "high", label: "High" }], default_level: "high" } },
];

describe("chat model changes", () => {
  it("keeps inherited reasoning when the new model supports it", () => {
    expect(nextChatModelSelection("a", null, "low", models, "claude")).toEqual({
      model: "a", thinkingLevel: null,
    });
  });
  it("uses runtime default when inherited reasoning is unsupported", () => {
    expect(nextChatModelSelection("b", null, "low", models, "claude")).toEqual({
      model: "b", thinkingLevel: "",
    });
  });
  it("sets ZCode's required default on a reasoning model", () => {
    expect(nextChatModelSelection("b", null, "", models, "zcode")).toEqual({
      model: "b", thinkingLevel: "high",
    });
  });
  it("recognizes Claude context window variants without changing the saved model", () => {
    expect(nextChatModelSelection("a[1m]", null, "low", models, "claude")).toEqual({
      model: "a[1m]", thinkingLevel: null,
    });
  });
});
