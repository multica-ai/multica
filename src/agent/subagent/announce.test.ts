import { describe, it, expect } from "vitest";
import { buildSubagentSystemPrompt, formatAnnouncementMessage } from "./announce.js";
import type { FormatAnnouncementParams } from "./announce.js";

describe("buildSubagentSystemPrompt", () => {
  it("includes task and session context", () => {
    const prompt = buildSubagentSystemPrompt({
      requesterSessionId: "parent-123",
      childSessionId: "child-456",
      task: "Analyze the auth module for security issues",
    });

    expect(prompt).toContain("## Subagent Rules");
    expect(prompt).toContain("Analyze the auth module for security issues");
    expect(prompt).toContain("parent-123");
    expect(prompt).toContain("child-456");
    expect(prompt).toContain("Do NOT spawn nested subagents");
    expect(prompt).toContain("## Safety");
  });

  it("includes label when provided", () => {
    const prompt = buildSubagentSystemPrompt({
      requesterSessionId: "parent-123",
      childSessionId: "child-456",
      label: "Security Audit",
      task: "Check for vulnerabilities",
    });

    expect(prompt).toContain('Label: "Security Audit"');
  });

  it("omits label line when not provided", () => {
    const prompt = buildSubagentSystemPrompt({
      requesterSessionId: "parent-123",
      childSessionId: "child-456",
      task: "Do something",
    });

    expect(prompt).not.toContain("Label:");
  });
});

describe("formatAnnouncementMessage", () => {
  const baseParams: FormatAnnouncementParams = {
    runId: "run-1",
    childSessionId: "child-456",
    requesterSessionId: "parent-123",
    task: "Analyze code",
    label: "Code Analysis",
    cleanup: "delete",
    outcome: { status: "ok" },
    startedAt: 1000000,
    endedAt: 1030000,
  };

  it("formats successful completion", () => {
    const msg = formatAnnouncementMessage({
      ...baseParams,
      findings: "Found 3 issues in the auth module.",
    });

    expect(msg).toContain('"Code Analysis" just completed successfully');
    expect(msg).toContain("Found 3 issues in the auth module.");
    expect(msg).toContain("runtime 30s");
    expect(msg).toContain("session child-456");
  });

  it("formats error outcome", () => {
    const msg = formatAnnouncementMessage({
      ...baseParams,
      outcome: { status: "error", error: "API key expired" },
    });

    expect(msg).toContain("failed: API key expired");
  });

  it("formats timeout outcome", () => {
    const msg = formatAnnouncementMessage({
      ...baseParams,
      outcome: { status: "timeout" },
    });

    expect(msg).toContain("timed out");
  });

  it("shows (no output) when findings is not provided", () => {
    const msg = formatAnnouncementMessage(baseParams);

    expect(msg).toContain("(no output)");
  });

  it("uses task text when label is not provided", () => {
    const paramsNoLabel: FormatAnnouncementParams = {
      ...baseParams,
      label: undefined,
    };
    const msg = formatAnnouncementMessage(paramsNoLabel);

    expect(msg).toContain('"Analyze code"');
  });

  it("formats runtime for minutes", () => {
    const msg = formatAnnouncementMessage({
      ...baseParams,
      startedAt: 1000000,
      endedAt: 1150000, // 150 seconds = 2m30s
    });

    expect(msg).toContain("runtime 2m30s");
  });

  it("formats runtime for hours", () => {
    const msg = formatAnnouncementMessage({
      ...baseParams,
      startedAt: 1000000,
      endedAt: 4600000, // 3600 seconds = 1h
    });

    expect(msg).toContain("runtime 1h");
  });

  it("includes summarization instruction", () => {
    const msg = formatAnnouncementMessage(baseParams);

    expect(msg).toContain("Summarize this naturally for the user");
    expect(msg).toContain("NO_REPLY");
  });
});
