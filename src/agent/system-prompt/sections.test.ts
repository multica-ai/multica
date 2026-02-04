import { describe, expect, it } from "vitest";
import {
  buildConditionalToolSections,
  buildIdentitySection,
  buildMemoryFileSection,
  buildProfileDirSection,
  buildRuntimeSection,
  buildSafetySection,
  buildSkillsSection,
  buildSubagentSection,
  buildToolCallStyleSection,
  buildToolingSummary,
  buildUserSection,
  buildWorkspaceSection,
} from "./sections.js";

describe("buildIdentitySection", () => {
  it("returns soul content in full mode", () => {
    const result = buildIdentitySection({ soul: "You are helpful." }, "full");
    expect(result).toEqual(["You are helpful."]);
  });

  it("returns identity line with name in none mode", () => {
    const result = buildIdentitySection({ config: { name: "Cleo" } }, "none");
    expect(result).toEqual(["You are Cleo, a Super Multica agent."]);
  });

  it("returns generic identity line in none mode without name", () => {
    const result = buildIdentitySection(undefined, "none");
    expect(result).toEqual(["You are a Super Multica agent."]);
  });

  it("returns empty in minimal mode", () => {
    const result = buildIdentitySection({ soul: "data" }, "minimal");
    expect(result).toEqual([]);
  });
});

describe("buildUserSection", () => {
  it("returns user content in full mode", () => {
    const result = buildUserSection({ user: "Name: Bob" }, "full");
    expect(result).toEqual(["Name: Bob"]);
  });

  it("returns empty in minimal mode", () => {
    const result = buildUserSection({ user: "data" }, "minimal");
    expect(result).toEqual([]);
  });

  it("returns empty when no user content", () => {
    const result = buildUserSection({}, "full");
    expect(result).toEqual([]);
  });
});

describe("buildWorkspaceSection", () => {
  it("returns workspace content in full mode", () => {
    const result = buildWorkspaceSection({ workspace: "Rules here" }, "full");
    expect(result).toEqual(["Rules here"]);
  });

  it("returns empty in minimal mode", () => {
    expect(buildWorkspaceSection({ workspace: "data" }, "minimal")).toEqual([]);
  });
});

describe("buildMemoryFileSection", () => {
  it("returns memory content in full mode", () => {
    const result = buildMemoryFileSection({ memory: "Key facts" }, "full");
    expect(result).toEqual(["Key facts"]);
  });

  it("returns empty in minimal mode", () => {
    expect(buildMemoryFileSection({ memory: "data" }, "minimal")).toEqual([]);
  });
});

describe("buildSafetySection", () => {
  it("returns safety text when enabled", () => {
    const result = buildSafetySection(true);
    expect(result.length).toBe(1);
    expect(result[0]).toContain("## Safety");
    expect(result[0]).toContain("no independent goals");
  });

  it("returns empty when disabled", () => {
    expect(buildSafetySection(false)).toEqual([]);
  });
});

describe("buildToolingSummary", () => {
  it("lists core tools with descriptions in order", () => {
    const result = buildToolingSummary(["exec", "read", "write"], "full");
    const text = result.join("\n");
    expect(text).toContain("## Tooling");
    expect(text).toContain("- read: Read file contents");
    expect(text).toContain("- write: Create or overwrite files");
    expect(text).toContain("- exec: Run shell commands");
    // read should appear before exec (order)
    expect(text.indexOf("- read")).toBeLessThan(text.indexOf("- exec"));
  });

  it("appends unknown tools alphabetically", () => {
    const result = buildToolingSummary(["read", "zeta_tool", "alpha_tool"], "full");
    const text = result.join("\n");
    expect(text).toContain("- alpha_tool");
    expect(text).toContain("- zeta_tool");
    expect(text.indexOf("- alpha_tool")).toBeLessThan(text.indexOf("- zeta_tool"));
  });

  it("returns empty for none mode", () => {
    expect(buildToolingSummary(["read"], "none")).toEqual([]);
  });

  it("returns empty for empty tools", () => {
    expect(buildToolingSummary([], "full")).toEqual([]);
  });

  it("works in minimal mode", () => {
    const result = buildToolingSummary(["read"], "minimal");
    expect(result.join("\n")).toContain("## Tooling");
  });

  it("preserves original tool casing", () => {
    const result = buildToolingSummary(["Read", "MyCustomTool", "EXEC"], "full");
    const text = result.join("\n");
    // Core tools: first-seen casing preserved
    expect(text).toContain("- Read: Read file contents");
    expect(text).toContain("- EXEC: Run shell commands");
    // Unknown tools: original casing preserved
    expect(text).toContain("- MyCustomTool");
  });

  it("deduplicates tools by lowercase", () => {
    const result = buildToolingSummary(["read", "Read", "READ"], "full");
    const text = result.join("\n");
    // Should appear only once (first occurrence)
    const matches = text.match(/- read/gi);
    expect(matches).toHaveLength(1);
  });
});

describe("buildToolCallStyleSection", () => {
  it("returns content in full mode", () => {
    const result = buildToolCallStyleSection("full");
    expect(result.join("\n")).toContain("## Tool Call Style");
  });

  it("returns content in minimal mode", () => {
    expect(buildToolCallStyleSection("minimal").length).toBeGreaterThan(0);
  });

  it("returns empty in none mode", () => {
    expect(buildToolCallStyleSection("none")).toEqual([]);
  });
});

describe("buildConditionalToolSections", () => {
  it("includes memory section when memory tools present", () => {
    const result = buildConditionalToolSections(["memory_get", "read"], "full");
    expect(result.join("\n")).toContain("## Memory");
  });

  it("includes sub-agents section when sessions_spawn present in full mode", () => {
    const result = buildConditionalToolSections(["sessions_spawn"], "full");
    expect(result.join("\n")).toContain("## Sub-Agents");
  });

  it("excludes sub-agents section in minimal mode", () => {
    const result = buildConditionalToolSections(["sessions_spawn"], "minimal");
    expect(result.join("\n")).not.toContain("## Sub-Agents");
  });

  it("includes web access section when web tools present", () => {
    const result = buildConditionalToolSections(["web_search"], "full");
    expect(result.join("\n")).toContain("## Web Access");
  });

  it("returns empty when no conditional tools match", () => {
    const result = buildConditionalToolSections(["read", "write"], "full");
    expect(result).toEqual([]);
  });

  it("returns empty in none mode", () => {
    expect(buildConditionalToolSections(["memory_get"], "none")).toEqual([]);
  });
});

describe("buildSkillsSection", () => {
  it("wraps skills prompt in full mode", () => {
    const result = buildSkillsSection("## commit\nDo commits.", "full");
    const text = result.join("\n");
    expect(text).toContain("## Skills (mandatory)");
    expect(text).toContain("## commit");
  });

  it("returns empty in minimal mode", () => {
    expect(buildSkillsSection("skills", "minimal")).toEqual([]);
  });

  it("returns empty when skills prompt is empty", () => {
    expect(buildSkillsSection("", "full")).toEqual([]);
    expect(buildSkillsSection(undefined, "full")).toEqual([]);
  });
});

describe("buildRuntimeSection", () => {
  it("formats runtime info in full mode", () => {
    const result = buildRuntimeSection(
      { agentName: "test", os: "darwin", arch: "arm64", nodeVersion: "v22.0.0", model: "claude", provider: "anthropic" },
      "full",
    );
    const text = result.join("\n");
    expect(text).toContain("## Runtime");
    expect(text).toContain("agent=test");
    expect(text).toContain("os=darwin (arm64)");
    expect(text).toContain("model=anthropic/claude");
  });

  it("returns empty in none mode", () => {
    expect(buildRuntimeSection({ os: "darwin" }, "none")).toEqual([]);
  });

  it("returns empty when no runtime provided", () => {
    expect(buildRuntimeSection(undefined, "full")).toEqual([]);
  });
});

describe("buildProfileDirSection", () => {
  it("includes path in full mode", () => {
    const result = buildProfileDirSection("/path/to/profile", "full");
    expect(result.join("\n")).toContain("/path/to/profile");
  });

  it("returns empty in minimal mode", () => {
    expect(buildProfileDirSection("/path", "minimal")).toEqual([]);
  });
});

describe("buildSubagentSection", () => {
  const ctx = { requesterSessionId: "parent", childSessionId: "child", task: "Find bugs" };

  it("returns rules and task in minimal mode", () => {
    const result = buildSubagentSection(ctx, "minimal");
    const text = result.join("\n");
    expect(text).toContain("## Subagent Rules");
    expect(text).toContain("## Task");
    expect(text).toContain("Find bugs");
  });

  it("includes label when provided", () => {
    const result = buildSubagentSection({ ...ctx, label: "Bug Hunter" }, "minimal");
    expect(result.join("\n")).toContain('Label: "Bug Hunter"');
  });

  it("returns empty in full mode", () => {
    expect(buildSubagentSection(ctx, "full")).toEqual([]);
  });

  it("returns empty when no subagent context", () => {
    expect(buildSubagentSection(undefined, "minimal")).toEqual([]);
  });
});
