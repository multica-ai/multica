/**
 * System Prompt Section Builders
 *
 * Each function returns string[] (lines to include) or [] to skip.
 */

import { SAFETY_CONSTITUTION } from "./constitution.js";
import { formatRuntimeLine } from "./runtime-info.js";
import type { ProfileContent, RuntimeInfo, SubagentContext, SystemPromptMode } from "./types.js";

// ─── Core tool summaries ────────────────────────────────────────────────────

/** Brief descriptions of Super Multica's built-in tools */
const CORE_TOOL_SUMMARIES: Record<string, string> = {
  read: "Read file contents",
  write: "Create or overwrite files",
  edit: "Make precise edits to files",
  glob: "Find files by glob pattern",
  exec: "Run shell commands",
  process: "Manage background exec sessions",
  web_search: "Search the web",
  web_fetch: "Fetch and extract readable content from a URL",
  memory_get: "Read from agent memory",
  memory_set: "Write to agent memory",
  memory_list: "List memory entries",
  memory_delete: "Delete memory entries",
  sessions_spawn: "Spawn a sub-agent session",
};

/** Preferred display order for tools */
const TOOL_ORDER = [
  "read",
  "write",
  "edit",
  "glob",
  "exec",
  "process",
  "web_search",
  "web_fetch",
  "memory_get",
  "memory_set",
  "memory_list",
  "memory_delete",
  "sessions_spawn",
];

// ─── Section builders ───────────────────────────────────────────────────────

/**
 * Identity section — soul.md in full mode, single line in none mode, nothing in minimal.
 */
export function buildIdentitySection(
  profile: ProfileContent | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode === "none") {
    const name = profile?.config?.name;
    return name
      ? [`You are ${name}, a Super Multica agent.`]
      : ["You are a Super Multica agent."];
  }
  if (mode === "minimal") {
    return [];
  }
  // full mode
  if (profile?.soul) {
    return [profile.soul];
  }
  return [];
}

/**
 * User section — user.md content (full mode only).
 */
export function buildUserSection(
  profile: ProfileContent | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode !== "full" || !profile?.user) return [];
  return [profile.user];
}

/**
 * Workspace section — workspace.md content (full mode only).
 */
export function buildWorkspaceSection(
  profile: ProfileContent | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode !== "full" || !profile?.workspace) return [];
  return [profile.workspace];
}

/**
 * Memory section — memory.md content (full mode only).
 */
export function buildMemoryFileSection(
  profile: ProfileContent | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode !== "full" || !profile?.memory) return [];
  return [profile.memory];
}

/**
 * Safety constitution — always included.
 */
export function buildSafetySection(includeSafety: boolean): string[] {
  if (!includeSafety) return [];
  return [SAFETY_CONSTITUTION];
}

/**
 * Tooling summary — lists active tools with descriptions.
 * Included in full and minimal modes.
 * Preserves original tool casing while deduplicating by lowercase.
 */
export function buildToolingSummary(
  tools: string[] | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode === "none" || !tools || tools.length === 0) return [];

  // Preserve original casing: first occurrence wins per normalized name
  const canonicalByNormalized = new Map<string, string>();
  for (const name of tools) {
    const normalized = name.toLowerCase();
    if (!canonicalByNormalized.has(normalized)) {
      canonicalByNormalized.set(normalized, name);
    }
  }
  const resolveToolName = (normalized: string) =>
    canonicalByNormalized.get(normalized) ?? normalized;

  const normalizedTools = new Set(canonicalByNormalized.keys());

  // Build ordered tool lines
  const toolLines: string[] = [];
  const seen = new Set<string>();

  // Core tools in preferred order
  for (const tool of TOOL_ORDER) {
    if (normalizedTools.has(tool) && !seen.has(tool)) {
      seen.add(tool);
      const displayName = resolveToolName(tool);
      const summary = CORE_TOOL_SUMMARIES[tool];
      toolLines.push(summary ? `- ${displayName}: ${summary}` : `- ${displayName}`);
    }
  }

  // External/unknown tools alphabetically
  const extraTools = [...normalizedTools].filter((t) => !seen.has(t)).sort();
  for (const tool of extraTools) {
    const displayName = resolveToolName(tool);
    toolLines.push(`- ${displayName}`);
  }

  return [
    "## Tooling",
    "Tool availability (filtered by policy):",
    "Tool names are case-sensitive. Call tools exactly as listed.",
    toolLines.join("\n"),
    "",
  ];
}

/**
 * Tool call style guidance — full and minimal modes.
 */
export function buildToolCallStyleSection(mode: SystemPromptMode): string[] {
  if (mode === "none") return [];
  return [
    "## Tool Call Style",
    "Default: do not narrate routine, low-risk tool calls (just call the tool).",
    "Narrate only when it helps: multi-step work, complex problems, sensitive actions (e.g., deletions), or when the user explicitly asks.",
    "Keep narration brief and value-dense; avoid repeating obvious steps.",
    "",
  ];
}

/**
 * Conditional tool sections — inject usage hints based on which tools are active.
 */
export function buildConditionalToolSections(
  tools: string[] | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode === "none" || !tools || tools.length === 0) return [];

  const toolSet = new Set(tools.map((t) => t.toLowerCase()));
  const lines: string[] = [];

  // Memory tools
  const hasMemory =
    toolSet.has("memory_get") ||
    toolSet.has("memory_set") ||
    toolSet.has("memory_list") ||
    toolSet.has("memory_delete");
  if (hasMemory) {
    lines.push(
      "## Memory",
      "Before answering anything about prior work, decisions, dates, people, preferences, or todos: search memory first, then pull only the needed entries.",
      "Update memory when the user shares important information, decisions, or preferences.",
      "",
    );
  }

  // Subagent tools (full mode only — minimal agents cannot spawn)
  if (mode === "full" && toolSet.has("sessions_spawn")) {
    lines.push(
      "## Sub-Agents",
      "If a task is complex or long-running, spawn a sub-agent. It will do the work and report back when done.",
      "You can check on running sub-agents at any time.",
      "Sub-agents cannot spawn nested sub-agents.",
      "",
    );
  }

  // Web tools
  if (toolSet.has("web_search") || toolSet.has("web_fetch")) {
    lines.push(
      "## Web Access",
      "You have web access. Use it when the user asks about current events, needs up-to-date information, or requests content from URLs.",
      "Prefer web_search for discovery and web_fetch for specific URLs.",
      "",
    );
  }

  return lines;
}

/**
 * Skills section — wraps SkillManager output with mandatory scan instructions.
 * Full mode only.
 */
export function buildSkillsSection(
  skillsPrompt: string | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode !== "full") return [];
  const trimmed = skillsPrompt?.trim();
  if (!trimmed) return [];

  return [
    "## Skills (mandatory)",
    "Before replying: scan the available skills below.",
    "- If exactly one skill clearly applies: follow its instructions.",
    "- If multiple could apply: choose the most specific one.",
    "- If none clearly apply: skip skill invocation.",
    "",
    trimmed,
    "",
  ];
}

/**
 * Runtime info line — full and minimal modes.
 */
export function buildRuntimeSection(
  runtime: RuntimeInfo | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode === "none" || !runtime) return [];
  return ["## Runtime", formatRuntimeLine(runtime)];
}

/**
 * Profile directory section — tells agent where its files live.
 * Full mode only.
 */
export function buildProfileDirSection(
  profileDir: string | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode !== "full" || !profileDir) return [];
  return [
    "## Profile Directory",
    "",
    `Your profile files are located at: \`${profileDir}\``,
    "",
    "Use `edit` or `write` tools to update these files when needed.",
  ];
}

/**
 * Subagent context — rules and task for child agents.
 * Minimal and none modes only.
 */
export function buildSubagentSection(
  subagent: SubagentContext | undefined,
  mode: SystemPromptMode,
): string[] {
  if (mode === "full" || !subagent) return [];

  const lines: string[] = [
    "## Subagent Rules",
    "- Stay focused on the assigned task below.",
    "- Complete the task thoroughly and report your findings.",
    "- Do NOT initiate side actions unrelated to the task.",
    "- Do NOT attempt to communicate with the user directly.",
    "- Do NOT spawn nested subagents.",
    "- Your session is ephemeral and will be cleaned up after completion.",
    "",
    "## Context",
    `Requester session: ${subagent.requesterSessionId}`,
    `Child session: ${subagent.childSessionId}`,
  ];

  if (subagent.label) {
    lines.push(`Label: "${subagent.label}"`);
  }

  lines.push("", "## Task", subagent.task);

  return lines;
}

/**
 * Extra system prompt — appended at the end if provided.
 */
export function buildExtraPromptSection(
  extraSystemPrompt: string | undefined,
  mode: SystemPromptMode,
): string[] {
  const trimmed = extraSystemPrompt?.trim();
  if (!trimmed) return [];

  const header = mode === "minimal" ? "## Subagent Context" : "## Additional Context";
  return [header, trimmed];
}
