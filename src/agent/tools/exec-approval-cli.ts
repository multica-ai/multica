/**
 * CLI Terminal Approval — readline-based approval for CLI mode (no Hub/Gateway)
 */

import readline from "readline";
import type {
  ExecApprovalCallback,
  ExecApprovalConfig,
  ApprovalDecision,
  ApprovalResult,
} from "./exec-approval-types.js";
import { DEFAULT_APPROVAL_TIMEOUT_MS } from "./exec-approval-types.js";
import { evaluateCommandSafety, requiresApproval } from "./exec-safety.js";
import { matchAllowlist, addAllowlistEntry, recordAllowlistUse } from "./exec-allowlist.js";

/** ANSI color helpers */
const red = (s: string) => `\x1b[31m${s}\x1b[0m`;
const yellow = (s: string) => `\x1b[33m${s}\x1b[0m`;
const green = (s: string) => `\x1b[32m${s}\x1b[0m`;
const bold = (s: string) => `\x1b[1m${s}\x1b[0m`;
const dim = (s: string) => `\x1b[2m${s}\x1b[0m`;

/** Risk level color mapping */
function colorRisk(level: string): string {
  switch (level) {
    case "dangerous": return red(level);
    case "needs-review": return yellow(level);
    case "safe": return green(level);
    default: return level;
  }
}

/**
 * Callback for persisting allowlist changes.
 * The Hub mode uses ProfileManager; CLI callers provide their own persistence.
 */
export type AllowlistPersister = (updatedConfig: ExecApprovalConfig) => void;

/**
 * Create a CLI-based approval callback that prompts the user in the terminal.
 *
 * @param config - Exec approval configuration (security, ask, allowlist, etc.)
 * @param onConfigUpdate - Optional callback to persist config changes (e.g., allowlist updates)
 */
export function createCliApprovalCallback(
  config: ExecApprovalConfig,
  onConfigUpdate?: AllowlistPersister,
): ExecApprovalCallback {
  // Mutable copy of config for runtime allowlist updates
  const runtimeConfig = { ...config, allowlist: [...(config.allowlist ?? [])] };

  return async (command: string, cwd: string | undefined): Promise<ApprovalResult> => {
    const security = runtimeConfig.security ?? "allowlist";
    const ask = runtimeConfig.ask ?? "on-miss";
    const timeoutMs = runtimeConfig.timeoutMs ?? DEFAULT_APPROVAL_TIMEOUT_MS;

    // Security: deny blocks everything
    if (security === "deny") {
      return { approved: false, decision: "deny" };
    }

    // Security: full allows everything
    if (security === "full") {
      return { approved: true, decision: "allow-once" };
    }

    // Evaluate safety
    const evaluation = evaluateCommandSafety(command, runtimeConfig);

    // Check if approval is needed
    const needsApproval = requiresApproval({
      ask,
      security,
      analysisOk: evaluation.analysisOk,
      allowlistSatisfied: evaluation.allowlistSatisfied,
    });

    if (!needsApproval) {
      // Auto-approved: record allowlist usage if it was an allowlist match
      if (evaluation.allowlistSatisfied) {
        const match = matchAllowlist(runtimeConfig.allowlist ?? [], command);
        if (match) {
          runtimeConfig.allowlist = recordAllowlistUse(runtimeConfig.allowlist ?? [], match, command);
          onConfigUpdate?.(runtimeConfig);
        }
      }
      return { approved: true, decision: "allow-once" };
    }

    // Prompt user in terminal
    const decision = await promptTerminal(command, cwd, evaluation.riskLevel, evaluation.reasons, timeoutMs);

    if (decision === "allow-always") {
      // Extract binary or full command as allowlist pattern
      const pattern = extractAllowlistPattern(command);
      runtimeConfig.allowlist = addAllowlistEntry(runtimeConfig.allowlist ?? [], pattern);
      onConfigUpdate?.(runtimeConfig);
    }

    return {
      approved: decision !== "deny",
      decision,
    };
  };
}

/**
 * Extract an allowlist pattern from a command.
 * Uses the binary name + "**" for broad matching.
 */
function extractAllowlistPattern(command: string): string {
  const trimmed = command.trim();
  const binary = trimmed.split(/\s+/)[0];
  return binary ? `${binary} **` : trimmed;
}

/**
 * Prompt the user for an approval decision via readline.
 */
function promptTerminal(
  command: string,
  cwd: string | undefined,
  riskLevel: string,
  reasons: string[],
  timeoutMs: number,
): Promise<ApprovalDecision> {
  return new Promise((resolve) => {
    const rl = readline.createInterface({
      input: process.stdin,
      output: process.stderr, // Use stderr to avoid mixing with stdout piping
    });

    let resolved = false;
    const cleanup = () => {
      if (resolved) return;
      resolved = true;
      rl.close();
    };

    // Timeout: auto-deny
    const timer = setTimeout(() => {
      if (resolved) return;
      process.stderr.write(dim(`\n  Approval timed out (${timeoutMs / 1000}s). Denying.\n\n`));
      cleanup();
      resolve("deny");
    }, timeoutMs);

    // Display approval prompt
    process.stderr.write("\n");
    process.stderr.write(bold("  Exec approval required\n"));
    process.stderr.write(`  ${dim("Command:")} ${command}\n`);
    if (cwd) process.stderr.write(`  ${dim("CWD:")}     ${cwd}\n`);
    process.stderr.write(`  ${dim("Risk:")}    ${colorRisk(riskLevel)}\n`);
    if (reasons.length > 0) {
      for (const reason of reasons) {
        process.stderr.write(`  ${dim("  -")} ${reason}\n`);
      }
    }
    process.stderr.write("\n");

    rl.question(
      `  ${bold("[a]")}llow once / ${bold("[A]")}llow always / ${bold("[d]")}eny (default: deny): `,
      (answer) => {
        clearTimeout(timer);
        cleanup();

        const trimmed = answer.trim();
        if (trimmed === "a" || trimmed === "allow-once") {
          resolve("allow-once");
        } else if (trimmed === "A" || trimmed === "allow-always") {
          resolve("allow-always");
        } else {
          resolve("deny");
        }
      },
    );

    // Handle Ctrl+C gracefully
    rl.on("close", () => {
      clearTimeout(timer);
      if (!resolved) {
        resolved = true;
        resolve("deny");
      }
    });
  });
}
