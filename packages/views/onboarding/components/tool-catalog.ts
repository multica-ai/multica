/**
 * The coding tools onboarding offers to install when they are missing. The
 * daemon detects many more — anything on this list or not shows up once it is
 * on PATH — but these four are the ones most people already pay for, so they
 * are the ones worth naming on an empty machine.
 *
 * Commands are the vendors' own macOS / Linux one-liners, the same ones
 * `apps/docs/content/docs/install-agent-runtime.mdx` documents; the guide link
 * covers Windows and anything the one-liner does not.
 */
export interface InstallableTool {
  provider: "claude" | "codex" | "cursor" | "copilot";
  /** Who sells the subscription the tool signs in with. */
  vendor: string;
  installCommand: string;
  /** What to run once afterwards so the tool can use the subscription. */
  signInCommand: string;
  guideUrl: string;
}

export const INSTALLABLE_TOOLS: readonly InstallableTool[] = [
  {
    provider: "claude",
    vendor: "Anthropic",
    installCommand: "curl -fsSL https://claude.ai/install.sh | bash",
    signInCommand: "claude",
    guideUrl: "https://code.claude.com/docs/en/quickstart",
  },
  {
    provider: "codex",
    vendor: "OpenAI",
    installCommand: "npm i -g @openai/codex",
    signInCommand: "codex login",
    guideUrl: "https://developers.openai.com/codex/cli/",
  },
  {
    provider: "cursor",
    vendor: "Cursor",
    installCommand: "curl https://cursor.com/install -fsS | bash",
    signInCommand: "cursor-agent login",
    guideUrl: "https://docs.cursor.com/en/cli/installation",
  },
  {
    provider: "copilot",
    vendor: "GitHub",
    installCommand: "npm install -g @github/copilot",
    signInCommand: "copilot",
    guideUrl:
      "https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/install-copilot-cli",
  },
];
