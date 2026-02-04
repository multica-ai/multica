/**
 * System Prompt Report — telemetry and diagnostics
 */

import type { SystemPromptReport } from "./types.js";

/**
 * Format a prompt report as a human-readable summary for debugging.
 */
export function formatPromptReport(report: SystemPromptReport): string {
  const lines: string[] = [
    `System Prompt Report (mode: ${report.mode})`,
    `  Total: ${report.totalChars} chars, ${report.totalLines} lines`,
    `  Tools: ${report.toolCount}`,
    `  Skills: ${report.skillsIncluded ? "yes" : "no"}`,
    `  Safety: ${report.safetyIncluded ? "yes" : "no"}`,
    "",
    "  Sections:",
  ];

  for (const section of report.sections) {
    const status = section.included ? "✓" : "—";
    lines.push(
      `    ${status} ${section.name}: ${section.chars} chars, ${section.lines} lines`,
    );
  }

  return lines.join("\n");
}
