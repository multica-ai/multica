import type { AgentCLIStatus } from "../types";

/**
 * Manual install or upgrade when the server says this row has no upgrade
 * path. `upgradable` is that signal. An up-to-date row is not upgradable
 * either; the card shows that status instead of install copy.
 */
export function isManualInstallOnly(row: AgentCLIStatus): boolean {
  if (row.upgradable) return false;
  return String(row.status ?? "") !== "up_to_date";
}

export function agentCLIDisplayName(cli: string): string {
  switch (cli.toLowerCase()) {
    case "codex":
      return "Codex";
    case "claude":
      return "Claude";
    case "grok":
      return "Grok";
    default:
      return cli;
  }
}

/**
 * Who may start an agent CLI upgrade:
 * - local (and any non-cloud mode) → the runtime owner only
 * - cloud → a workspace admin or owner, or the runtime's owner
 *
 * The UI hides Update and Retry when this is false.
 */
export function canUpgradeAgentCLI(opts: {
  runtimeMode: string | null | undefined;
  isRuntimeOwner: boolean;
  isWorkspaceAdmin: boolean;
}): boolean {
  if (opts.runtimeMode === "cloud") {
    return opts.isWorkspaceAdmin || opts.isRuntimeOwner;
  }
  return opts.isRuntimeOwner;
}
