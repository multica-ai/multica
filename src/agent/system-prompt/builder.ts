/**
 * System Prompt Builder
 *
 * Core assembly logic: collects sections based on mode, filters, and joins.
 */

import type {
  PromptSection,
  SystemPromptOptions,
  SystemPromptReport,
} from "./types.js";
import {
  buildConditionalToolSections,
  buildExtraPromptSection,
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

/**
 * Build a system prompt from structured options.
 */
export function buildSystemPrompt(options: SystemPromptOptions): string {
  const { prompt } = buildSystemPromptWithReport(options);
  return prompt;
}

/**
 * Build a system prompt and return a diagnostic report alongside it.
 */
export function buildSystemPromptWithReport(options: SystemPromptOptions): {
  prompt: string;
  report: SystemPromptReport;
} {
  const {
    mode,
    profile,
    profileDir,
    tools,
    skillsPrompt,
    runtime,
    subagent,
    extraSystemPrompt,
    includeSafety = true,
  } = options;

  // Collect all candidate sections in order
  const candidates: Array<{ name: string; lines: string[] }> = [
    { name: "identity", lines: buildIdentitySection(profile, mode) },
    { name: "user", lines: buildUserSection(profile, mode) },
    { name: "workspace", lines: buildWorkspaceSection(profile, mode) },
    { name: "memory", lines: buildMemoryFileSection(profile, mode) },
    { name: "safety", lines: buildSafetySection(includeSafety) },
    { name: "tooling", lines: buildToolingSummary(tools, mode) },
    { name: "tool-call-style", lines: buildToolCallStyleSection(mode) },
    { name: "conditional-tools", lines: buildConditionalToolSections(tools, mode) },
    { name: "skills", lines: buildSkillsSection(skillsPrompt, mode) },
    { name: "runtime", lines: buildRuntimeSection(runtime, mode) },
    { name: "profile-dir", lines: buildProfileDirSection(profileDir, mode) },
    { name: "subagent", lines: buildSubagentSection(subagent, mode) },
    { name: "extra", lines: buildExtraPromptSection(extraSystemPrompt, mode) },
  ];

  // Build included sections
  const sections: PromptSection[] = [];
  const reportSections: SystemPromptReport["sections"] = [];

  for (const { name, lines } of candidates) {
    const included = lines.length > 0;
    const content = lines.join("\n");
    reportSections.push({
      name,
      chars: content.length,
      lines: included ? content.split("\n").length : 0,
      included,
    });
    if (included) {
      sections.push({ name, content });
    }
  }

  // Join sections with double newline separators
  const prompt = sections.map((s) => s.content).join("\n\n");

  const report: SystemPromptReport = {
    mode,
    totalChars: prompt.length,
    totalLines: prompt.split("\n").length,
    sections: reportSections,
    toolCount: tools?.length ?? 0,
    skillsIncluded: (skillsPrompt?.trim()?.length ?? 0) > 0 && mode === "full",
    safetyIncluded: includeSafety,
  };

  return { prompt, report };
}
