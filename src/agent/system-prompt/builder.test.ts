import { describe, expect, it } from "vitest";
import { buildSystemPrompt, buildSystemPromptWithReport } from "./builder.js";
import type { SystemPromptOptions } from "./types.js";

const PROFILE = {
  soul: "# Soul\nYou are a helpful coding assistant.",
  user: "# User\nName: Alice",
  workspace: "# Workspace\nFollow conventional commits.",
  memory: "# Memory\nUser prefers TypeScript.",
  config: { name: "TestAgent" },
};

const TOOLS = ["read", "write", "edit", "glob", "exec", "memory_get", "memory_set", "sessions_spawn", "web_search"];

describe("buildSystemPrompt", () => {
  // ── Full mode ─────────────────────────────────────────────────────────

  it("full mode includes all profile sections", () => {
    const result = buildSystemPrompt({ mode: "full", profile: PROFILE });
    expect(result).toContain("# Soul");
    expect(result).toContain("# User");
    expect(result).toContain("# Workspace");
    expect(result).toContain("# Memory");
  });

  it("full mode includes safety constitution", () => {
    const result = buildSystemPrompt({ mode: "full" });
    expect(result).toContain("## Safety");
    expect(result).toContain("no independent goals");
  });

  it("full mode includes tooling summary when tools provided", () => {
    const result = buildSystemPrompt({ mode: "full", tools: TOOLS });
    expect(result).toContain("## Tooling");
    expect(result).toContain("- read: Read file contents");
    expect(result).toContain("- exec: Run shell commands");
  });

  it("full mode includes tool call style section", () => {
    const result = buildSystemPrompt({ mode: "full", tools: TOOLS });
    expect(result).toContain("## Tool Call Style");
  });

  it("full mode includes memory section when memory tools present", () => {
    const result = buildSystemPrompt({ mode: "full", tools: ["memory_get", "memory_set"] });
    expect(result).toContain("## Memory");
    expect(result).toContain("search memory first");
  });

  it("full mode includes sub-agents section when sessions_spawn present", () => {
    const result = buildSystemPrompt({ mode: "full", tools: ["sessions_spawn"] });
    expect(result).toContain("## Sub-Agents");
  });

  it("full mode includes web access section when web tools present", () => {
    const result = buildSystemPrompt({ mode: "full", tools: ["web_search"] });
    expect(result).toContain("## Web Access");
  });

  it("full mode includes skills section when provided", () => {
    const result = buildSystemPrompt({
      mode: "full",
      skillsPrompt: "## commit\nRun conventional commits.",
    });
    expect(result).toContain("## Skills (mandatory)");
    expect(result).toContain("## commit");
  });

  it("full mode includes runtime info line", () => {
    const result = buildSystemPrompt({
      mode: "full",
      runtime: { agentName: "test", os: "darwin", arch: "arm64", nodeVersion: "v22.0.0" },
    });
    expect(result).toContain("## Runtime");
    expect(result).toContain("agent=test");
    expect(result).toContain("os=darwin (arm64)");
  });

  it("full mode includes profile directory", () => {
    const result = buildSystemPrompt({
      mode: "full",
      profileDir: "/home/user/.super-multica/agent-profiles/test",
    });
    expect(result).toContain("## Profile Directory");
    expect(result).toContain("/home/user/.super-multica/agent-profiles/test");
  });

  it("full mode excludes subagent section", () => {
    const result = buildSystemPrompt({
      mode: "full",
      subagent: { requesterSessionId: "a", childSessionId: "b", task: "test" },
    });
    expect(result).not.toContain("## Subagent Rules");
  });

  // ── Minimal mode ──────────────────────────────────────────────────────

  it("minimal mode excludes profile content", () => {
    const result = buildSystemPrompt({ mode: "minimal", profile: PROFILE });
    expect(result).not.toContain("# Soul");
    expect(result).not.toContain("# User");
    expect(result).not.toContain("# Workspace");
    expect(result).not.toContain("# Memory");
  });

  it("minimal mode includes safety constitution", () => {
    const result = buildSystemPrompt({ mode: "minimal" });
    expect(result).toContain("## Safety");
  });

  it("minimal mode includes tooling summary", () => {
    const result = buildSystemPrompt({ mode: "minimal", tools: ["read", "write"] });
    expect(result).toContain("## Tooling");
  });

  it("minimal mode includes subagent context", () => {
    const result = buildSystemPrompt({
      mode: "minimal",
      subagent: { requesterSessionId: "parent-1", childSessionId: "child-1", task: "Search for bugs" },
    });
    expect(result).toContain("## Subagent Rules");
    expect(result).toContain("Search for bugs");
    expect(result).toContain("parent-1");
  });

  it("minimal mode excludes skills section", () => {
    const result = buildSystemPrompt({
      mode: "minimal",
      skillsPrompt: "## commit\nSome skill.",
    });
    expect(result).not.toContain("## Skills");
  });

  it("minimal mode excludes sub-agents section even with sessions_spawn", () => {
    const result = buildSystemPrompt({ mode: "minimal", tools: ["sessions_spawn"] });
    expect(result).not.toContain("## Sub-Agents");
  });

  // ── None mode ─────────────────────────────────────────────────────────

  it("none mode includes identity line with agent name", () => {
    const result = buildSystemPrompt({
      mode: "none",
      profile: { config: { name: "Multica" } },
    });
    expect(result).toContain("You are Multica, a Super Multica agent.");
  });

  it("none mode includes safety constitution", () => {
    const result = buildSystemPrompt({ mode: "none" });
    expect(result).toContain("## Safety");
  });

  it("none mode excludes tooling and skills", () => {
    const result = buildSystemPrompt({
      mode: "none",
      tools: TOOLS,
      skillsPrompt: "some skills",
    });
    expect(result).not.toContain("## Tooling");
    expect(result).not.toContain("## Skills");
  });

  it("none mode includes subagent context", () => {
    const result = buildSystemPrompt({
      mode: "none",
      subagent: { requesterSessionId: "a", childSessionId: "b", task: "do stuff" },
    });
    expect(result).toContain("## Task");
    expect(result).toContain("do stuff");
  });

  // ── Cross-cutting ─────────────────────────────────────────────────────

  it("safety can be disabled via includeSafety=false", () => {
    const result = buildSystemPrompt({ mode: "full", includeSafety: false });
    expect(result).not.toContain("## Safety");
  });

  it("extra system prompt is appended", () => {
    const result = buildSystemPrompt({
      mode: "full",
      extraSystemPrompt: "Always respond in French.",
    });
    expect(result).toContain("## Additional Context");
    expect(result).toContain("Always respond in French.");
  });

  it("extra system prompt uses subagent header in minimal mode", () => {
    const result = buildSystemPrompt({
      mode: "minimal",
      extraSystemPrompt: "Focus on tests.",
    });
    expect(result).toContain("## Subagent Context");
  });

  it("returns empty-ish prompt when no options", () => {
    const result = buildSystemPrompt({ mode: "none" });
    // Should at least have identity + safety
    expect(result).toContain("Super Multica agent");
    expect(result).toContain("Safety");
  });

  it("handles unknown tools gracefully", () => {
    const result = buildSystemPrompt({ mode: "full", tools: ["custom_tool", "read"] });
    expect(result).toContain("- read: Read file contents");
    expect(result).toContain("- custom_tool");
  });
});

describe("buildSystemPromptWithReport", () => {
  it("report includes accurate section counts", () => {
    const { report } = buildSystemPromptWithReport({
      mode: "full",
      profile: PROFILE,
      tools: TOOLS,
    });
    expect(report.mode).toBe("full");
    expect(report.totalChars).toBeGreaterThan(0);
    expect(report.toolCount).toBe(TOOLS.length);
    expect(report.safetyIncluded).toBe(true);

    const identitySection = report.sections.find((s) => s.name === "identity");
    expect(identitySection?.included).toBe(true);

    const subagentSection = report.sections.find((s) => s.name === "subagent");
    expect(subagentSection?.included).toBe(false);
  });

  it("report reflects skills inclusion", () => {
    const { report: withSkills } = buildSystemPromptWithReport({
      mode: "full",
      skillsPrompt: "some skills",
    });
    expect(withSkills.skillsIncluded).toBe(true);

    const { report: withoutSkills } = buildSystemPromptWithReport({
      mode: "full",
    });
    expect(withoutSkills.skillsIncluded).toBe(false);
  });

  it("report marks excluded sections correctly in minimal mode", () => {
    const { report } = buildSystemPromptWithReport({ mode: "minimal" });
    const identity = report.sections.find((s) => s.name === "identity");
    expect(identity?.included).toBe(false);

    const safety = report.sections.find((s) => s.name === "safety");
    expect(safety?.included).toBe(true);
  });
});
