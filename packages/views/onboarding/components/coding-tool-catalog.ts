import type { CodingToolId } from "@multica/core/onboarding";

export interface CodingToolInfo {
  id: CodingToolId;
  name: string;
  /** Who the member signs in with. */
  account: string;
  /** Runtime `provider` the daemon registers this tool as. */
  provider: string;
  /** What to run when installing from Multica isn't an option. */
  terminalCommand: string;
  /** What to run when signing in from Multica didn't work. */
  loginCommand: string;
  /** Rough install time shown while it runs. */
  minutes: number;
}

/** Ordered by how often a new member already has one. */
export const CODING_TOOLS: readonly CodingToolInfo[] = [
  {
    id: "claude",
    name: "Claude Code",
    account: "Anthropic",
    provider: "claude",
    terminalCommand: "curl -fsSL https://claude.ai/install.sh | bash",
    loginCommand: "claude auth login",
    minutes: 1,
  },
  {
    id: "codex",
    name: "Codex",
    account: "OpenAI",
    provider: "codex",
    terminalCommand: "npm install -g @openai/codex",
    loginCommand: "codex login",
    minutes: 2,
  },
  {
    id: "cursor",
    name: "Cursor CLI",
    account: "Cursor",
    provider: "cursor",
    terminalCommand: "curl https://cursor.com/install -fsS | bash",
    loginCommand: "cursor-agent login",
    minutes: 1,
  },
];

export function codingTool(id: CodingToolId): CodingToolInfo {
  return CODING_TOOLS.find((tool) => tool.id === id)!;
}

export function codingToolForProvider(provider: string): CodingToolInfo | undefined {
  return CODING_TOOLS.find((tool) => tool.provider === provider);
}
