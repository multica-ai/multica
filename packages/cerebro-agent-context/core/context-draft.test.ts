import { describe, expect, it } from "vitest";
import {
  bumpPatch,
  isContextDraftDirty,
  canSubmitContextDraft,
  readBriefLayerMode,
  readSystemPromptMode,
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
    model: "gpt-5",
    thinkingLevel: "high",
    personaSandbox: "strict",
    workspaceBriefMode: "",
    toolsBriefMode: "",
    systemPromptMode: "",
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

  it("requires both a change and a title before submission", () => {
    expect(canSubmitContextDraft("Reason", true)).toBe(true);
    expect(canSubmitContextDraft("  ", true)).toBe(false);
    expect(canSubmitContextDraft("Reason", false)).toBe(false);
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
