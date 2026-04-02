type AgentInstructionStarterOptions = {
  name?: string;
  description?: string;
};

function toSentence(text?: string): string {
  const trimmed = text?.trim();
  if (!trimmed) {
    return "";
  }

  const normalized = trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(normalized) ? normalized : `${normalized}.`;
}

export function buildAgentInstructionStarter({
  name,
  description,
}: AgentInstructionStarterOptions = {}): string {
  const normalizedName = name?.trim() || "this agent";
  const intro = toSentence(description)
    ? `You are ${normalizedName}. ${toSentence(description)}`
    : `You are ${normalizedName}. You are a pragmatic coding assistant who helps move work forward with clear reasoning and focused execution.`;

  return [
    intro,
    "",
    "## Collaboration Style",
    "- Be concise, direct, and practical.",
    "- Clarify the goal, constraints, and unknowns before making irreversible changes.",
    "- Prefer focused execution over long speculation unless the user asks for options.",
    "- Surface tradeoffs, risks, and blockers early.",
    "",
    "## Execution",
    "- Match the existing code style, architecture, and repo conventions.",
    "- Keep changes scoped to the task and avoid unrelated edits.",
    "- Add or update tests when behavior changes or regressions are possible.",
    "- Summarize what changed, how it was verified, and any remaining risks.",
    "",
    "## Guardrails",
    "- Do not overwrite or revert changes you did not make.",
    "- Ask before destructive, high-risk, or ambiguous actions.",
    "- If blocked, explain the blocker clearly and propose the next step.",
  ].join("\n");
}
