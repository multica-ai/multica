import { describe, expect, it } from "vitest";
import {
  bumpPatch,
  isContextDraftDirty,
  canSubmitContextDraft,
  readBriefLayerMode,
  readSystemPromptMode,
  readSpeedMode,
  readPositiveIntegerSetting,
} from "./context-draft";

describe("bumpPatch", () => {
  it("increments a strict semantic patch version", () => {
    expect(bumpPatch("2.4.9")).toBe("2.4.10");
  });

  it("falls back for an invalid version", () => {
    expect(bumpPatch("draft")).toBe("1.0.1");
  });
});

describe("context draft state", () => {
  const current = {
    instructions: "Current",
    runtimeId: "runtime-1",
    model: "gpt-5",
    thinkingLevel: "high",
    workspaceBriefMode: "",
    toolsBriefMode: "",
    systemPromptMode: "",
    speedMode: "",
    maxTurns: "",
    timeoutMinutes: "",
  };

  it("detects a changed instruction", () => {
    expect(isContextDraftDirty(current, { ...current, instructions: "Changed" })).toBe(true);
  });

  it("does not mark an identical draft dirty", () => {
    expect(isContextDraftDirty(current, current)).toBe(false);
  });

  it("detects a changed workspace brief mode (FIR-3212)", () => {
    expect(isContextDraftDirty(current, { ...current, workspaceBriefMode: "off" })).toBe(true);
  });

  it("detects a changed tools brief mode (FIR-3212)", () => {
    expect(isContextDraftDirty(current, { ...current, toolsBriefMode: "summary" })).toBe(true);
  });

  it("detects a changed system prompt mode (FIR-3212)", () => {
    expect(isContextDraftDirty(current, { ...current, systemPromptMode: "replace" })).toBe(true);
  });

  it("detects changed runtime controls (FIR-4000)", () => {
    expect(isContextDraftDirty(current, { ...current, runtimeId: "runtime-2" })).toBe(true);
    expect(isContextDraftDirty(current, { ...current, speedMode: "fast" })).toBe(true);
    expect(isContextDraftDirty(current, { ...current, maxTurns: "18" })).toBe(true);
    expect(isContextDraftDirty(current, { ...current, timeoutMinutes: "42" })).toBe(true);
  });

  it("requires both a change and a title before submission", () => {
    expect(canSubmitContextDraft("Reason", true)).toBe(true);
    expect(canSubmitContextDraft("  ", true)).toBe(false);
    expect(canSubmitContextDraft("Reason", false)).toBe(false);
  });
});

describe("versioned runtime setting readers (FIR-4000)", () => {
  it("normalises speed and positive integer overrides", () => {
    const config = { speed_mode: "fast", max_turns: 18, timeout_minutes: 42 };
    expect(readSpeedMode(config)).toBe("fast");
    expect(readPositiveIntegerSetting(config, "max_turns")).toBe("18");
    expect(readPositiveIntegerSetting(config, "timeout_minutes")).toBe("42");
  });

  it("treats defaults and malformed values as inherited", () => {
    expect(readSpeedMode({ speed_mode: "standard" })).toBe("");
    expect(readPositiveIntegerSetting({ max_turns: 0 }, "max_turns")).toBe("");
    expect(readPositiveIntegerSetting({ timeout_minutes: "42" }, "timeout_minutes")).toBe("");
  });
});

describe("readBriefLayerMode (FIR-3212)", () => {
  it("reads a set mode out of runtime_config", () => {
    expect(readBriefLayerMode({ workspace_brief_mode: "off" }, "workspace_brief_mode")).toBe("off");
    expect(readBriefLayerMode({ tools_brief_mode: "summary" }, "tools_brief_mode")).toBe("summary");
  });

  it("treats a missing key, null config, or non-string value as the default", () => {
    expect(readBriefLayerMode(undefined, "workspace_brief_mode")).toBe("");
    expect(readBriefLayerMode(null, "workspace_brief_mode")).toBe("");
    expect(readBriefLayerMode({}, "tools_brief_mode")).toBe("");
    expect(readBriefLayerMode({ workspace_brief_mode: 3 }, "workspace_brief_mode")).toBe("");
  });

  it("normalises the explicit 'full' spelling back to the default", () => {
    expect(readBriefLayerMode({ workspace_brief_mode: "full" }, "workspace_brief_mode")).toBe("");
    expect(readBriefLayerMode({ tools_brief_mode: "full" }, "tools_brief_mode")).toBe("");
  });
});

describe("readSystemPromptMode (FIR-3212)", () => {
  it("reads each mode the server accepts", () => {
    expect(readSystemPromptMode({ system_prompt_mode: "replace" })).toBe("replace");
    expect(readSystemPromptMode({ system_prompt_mode: "append" })).toBe("append");
    expect(readSystemPromptMode({ system_prompt_mode: "prepend" })).toBe("prepend");
  });

  it("treats a missing key, null config, or non-string value as the engine default", () => {
    expect(readSystemPromptMode(undefined)).toBe("");
    expect(readSystemPromptMode(null)).toBe("");
    expect(readSystemPromptMode({})).toBe("");
    expect(readSystemPromptMode({ system_prompt_mode: 7 })).toBe("");
  });

  // The daemon reads an unrecognised value as the default, so the dialog must
  // show the default too — otherwise the control claims a behaviour no run has.
  it("treats a mode the daemon would ignore as the engine default", () => {
    expect(readSystemPromptMode({ system_prompt_mode: "replace-all" })).toBe("");
    expect(readSystemPromptMode({ system_prompt_mode: "full" })).toBe("");
  });

  // runtime_config is shared with the brief-layer knobs and openclaw's mode.
  it("reads its own key out of a shared runtime_config", () => {
    expect(
      readSystemPromptMode({ workspace_brief_mode: "off", system_prompt_mode: "replace" }),
    ).toBe("replace");
  });
});
