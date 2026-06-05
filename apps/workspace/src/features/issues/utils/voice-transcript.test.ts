import { describe, expect, it } from "vitest";
import { applyVoiceTranscriptToDraft, extractVoiceTitleCandidate } from "./voice-transcript";

describe("voice transcript mapping", () => {
  it("extracts the first sentence as a title candidate", () => {
    expect(extractVoiceTitleCandidate("Fix login bug. It happens on Safari.")).toBe("Fix login bug.");
  });

  it("fills an empty title and description", () => {
    const result = applyVoiceTranscriptToDraft({
      title: "",
      description: "",
      transcript: "Fix login bug. It happens on Safari.",
    });

    expect(result.title).toBe("Fix login bug.");
    expect(result.description).toBe("Fix login bug. It happens on Safari.");
    expect(result.titleNeedsManualConfirmation).toBe(false);
  });

  it("preserves an existing title", () => {
    const result = applyVoiceTranscriptToDraft({
      title: "Existing issue",
      description: "",
      transcript: "Add details from the meeting.",
    });

    expect(result.title).toBe("Existing issue");
    expect(result.description).toBe("Add details from the meeting.");
  });

  it("appends transcript after existing description", () => {
    const result = applyVoiceTranscriptToDraft({
      title: "Existing issue",
      description: "Current description",
      transcript: "Voice details",
    });

    expect(result.description).toBe("Current description\n\nVoice details");
  });

  it("keeps title empty when transcript is too short", () => {
    const result = applyVoiceTranscriptToDraft({
      title: "",
      description: "",
      transcript: "go",
    });

    expect(result.title).toBe("");
    expect(result.description).toBe("go");
    expect(result.titleNeedsManualConfirmation).toBe(true);
  });
});
