/**
 * Exec Safety Evaluation Engine
 *
 * Evaluates shell commands for safety using layered checks:
 * 1. Allowlist matching
 * 2. Shell syntax analysis (dangerous syntax detection)
 * 3. Safe binary detection
 * 4. Dangerous pattern detection
 */

import type {
  ExecSecurity,
  ExecAsk,
  ExecApprovalConfig,
  ExecAllowlistEntry,
  SafetyEvaluation,
} from "./exec-approval-types.js";
import { matchAllowlist } from "./exec-allowlist.js";

// ============ Safe Binaries ============

/** Known-safe read-only binaries that can auto-approve */
export const DEFAULT_SAFE_BINS = new Set([
  "ls", "cat", "head", "tail", "wc", "grep", "egrep", "fgrep",
  "sort", "uniq", "cut", "tr", "jq", "yq",
  "echo", "printf", "pwd", "which", "whereis", "whoami",
  "env", "date", "uname", "hostname",
  "file", "stat", "basename", "dirname", "realpath",
  "diff", "comm", "tee",
  "find", "xargs",
  "git", "node", "pnpm", "npm", "npx", "yarn", "bun",
  "python", "python3", "pip", "pip3",
  "go", "cargo", "rustc",
  "docker", "kubectl",
  "curl", "wget",
  "tar", "gzip", "gunzip", "zip", "unzip",
  "sed", "awk", "rg", "fd", "ag",
  "tree", "less", "more",
  "true", "false", "test",
  "mkdir", "touch", "cp", "mv", "ln",
]);

// ============ Dangerous Patterns ============

/** Patterns indicating dangerous operations */
const DANGEROUS_PATTERNS: Array<{ regex: RegExp; reason: string }> = [
  { regex: /\brm\s+(-[^\s]*r[^\s]*|--recursive)\s/i, reason: "Recursive delete (rm -r)" },
  { regex: /\brm\s+(-[^\s]*f[^\s]*)\s/i, reason: "Force delete (rm -f)" },
  { regex: /\bsudo\b/, reason: "Elevated privileges (sudo)" },
  { regex: /\bsu\s/, reason: "Switch user (su)" },
  { regex: /\bchmod\s+777\b/, reason: "World-writable permissions (chmod 777)" },
  { regex: /\bchmod\s+-[^\s]*R/, reason: "Recursive permission change (chmod -R)" },
  { regex: /\bchown\s+-[^\s]*R/, reason: "Recursive ownership change (chown -R)" },
  { regex: /\bmkfs\b/, reason: "Filesystem format (mkfs)" },
  { regex: /\bdd\s/, reason: "Low-level disk write (dd)" },
  { regex: /\beval\s/, reason: "Dynamic code evaluation (eval)" },
  { regex: /\bexec\s/, reason: "Process replacement (exec)" },
  { regex: />\s*\/etc\//, reason: "Write to /etc/" },
  { regex: />\s*\/usr\//, reason: "Write to /usr/" },
  { regex: />\s*\/sys\//, reason: "Write to /sys/" },
  { regex: />\s*\/proc\//, reason: "Write to /proc/" },
  { regex: />\s*\/dev\//, reason: "Write to /dev/" },
  { regex: /\bcurl\b.*\|\s*(ba)?sh/, reason: "Pipe URL to shell (curl | sh)" },
  { regex: /\bwget\b.*\|\s*(ba)?sh/, reason: "Pipe URL to shell (wget | sh)" },
  { regex: /\b(shutdown|reboot|halt|poweroff)\b/, reason: "System control command" },
  { regex: /\bkill\s+-9\b/, reason: "Force kill (kill -9)" },
  { regex: /\bkillall\b/, reason: "Kill all processes (killall)" },
  { regex: /\bpkill\b/, reason: "Pattern kill (pkill)" },
  { regex: />\s*\/dev\/sd[a-z]/, reason: "Direct disk write" },
  { regex: /\biptables\b/, reason: "Firewall modification (iptables)" },
  { regex: /\bufw\b/, reason: "Firewall modification (ufw)" },
];

// ============ Dangerous Shell Syntax ============

/** Shell syntax patterns that are inherently dangerous */
const DANGEROUS_SYNTAX: Array<{ regex: RegExp; reason: string }> = [
  { regex: /\|&/, reason: "Stderr redirect to pipe (|&)" },
  { regex: /\|\|/, reason: "Logical OR (||) — fallback execution" },
  { regex: /(?<!\|)\|(?!\|).*\b(ba)?sh\b/, reason: "Pipe to shell interpreter" },
  { regex: /[^\\]`[^`]+`/, reason: "Command substitution (backticks)" },
  { regex: /\$\(/, reason: "Command substitution $(...)" },
  { regex: /(?<![&])&(?!&)\s*$/, reason: "Background execution (&)" },
  { regex: /(?<![&])&(?!&)(?!\s*$)/, reason: "Background execution (&)" },
  { regex: /;\s*\S/, reason: "Command chaining (;)" },
  { regex: /\(\s*\S/, reason: "Subshell execution ()" },
];

// ============ Core Functions ============

/**
 * Extract the leading binary name from a shell command.
 * Handles common patterns: env prefix, path prefix.
 */
export function extractBinaryName(command: string): string | null {
  const trimmed = command.trim();
  if (!trimmed) return null;

  // Skip env prefix: "env FOO=bar cmd" → "cmd"
  let cmd = trimmed;
  if (cmd.startsWith("env ")) {
    const parts = cmd.split(/\s+/);
    // Skip "env" and any VAR=VAL assignments
    let i = 1;
    while (i < parts.length && parts[i]!.includes("=")) i++;
    cmd = parts.slice(i).join(" ");
  }

  // For piped commands, only check the first command
  const firstCmd = cmd.split(/\s*\|\s*/)[0]!.trim();

  // Extract just the binary (strip path prefix)
  const binary = firstCmd.split(/\s+/)[0];
  if (!binary) return null;

  // Get basename
  const parts = binary.split("/");
  return parts[parts.length - 1] || null;
}

/**
 * Check if a command has file-path arguments.
 * Safe binaries should not have file-path args to be auto-approved.
 */
export function hasFilePathArgs(command: string): boolean {
  const parts = command.trim().split(/\s+/).slice(1); // skip binary

  for (const part of parts) {
    // Skip flags
    if (part.startsWith("-")) {
      // Check if flag value is a file path (e.g., --output=/tmp/file)
      const eqIndex = part.indexOf("=");
      if (eqIndex !== -1) {
        const value = part.slice(eqIndex + 1);
        if (isFilePath(value)) return true;
      }
      continue;
    }
    if (isFilePath(part)) return true;
  }
  return false;
}

function isFilePath(s: string): boolean {
  return s.startsWith("/") || s.startsWith("./") || s.startsWith("../") || s.startsWith("~/") || /^[A-Za-z]:\\/.test(s);
}

/**
 * Check if a command uses only safe binaries in a safe manner.
 * For piped commands, all components must be safe.
 */
export function isSafeBinUsage(command: string, safeBins: Set<string> = DEFAULT_SAFE_BINS): boolean {
  const trimmed = command.trim();
  if (!trimmed) return false;

  // For piped commands, check each segment
  const segments = splitPipeSegments(trimmed);
  if (!segments) return false; // parsing failed

  for (const segment of segments) {
    const binary = extractBinaryName(segment);
    if (!binary) return false;

    // Check if binary is in safe list (case-insensitive)
    if (!safeBins.has(binary.toLowerCase())) return false;

    // Safe bins should not reference file paths as arguments
    if (hasFilePathArgs(segment)) return false;
  }

  return true;
}

/**
 * Split command into pipe segments.
 * Returns null if dangerous syntax is detected in the pipe chain.
 */
function splitPipeSegments(command: string): string[] | null {
  // Simple split on single pipes (not |& or ||)
  const parts: string[] = [];
  let current = "";
  let inSingleQuote = false;
  let inDoubleQuote = false;
  let escaped = false;

  for (let i = 0; i < command.length; i++) {
    const ch = command[i]!;

    if (escaped) {
      current += ch;
      escaped = false;
      continue;
    }

    if (ch === "\\") {
      current += ch;
      escaped = true;
      continue;
    }

    if (ch === "'" && !inDoubleQuote) {
      inSingleQuote = !inSingleQuote;
      current += ch;
      continue;
    }

    if (ch === '"' && !inSingleQuote) {
      inDoubleQuote = !inDoubleQuote;
      current += ch;
      continue;
    }

    if (ch === "|" && !inSingleQuote && !inDoubleQuote) {
      // Check for |& or ||
      const next = command[i + 1];
      if (next === "&" || next === "|") return null; // dangerous
      parts.push(current.trim());
      current = "";
      continue;
    }

    current += ch;
  }

  if (current.trim()) {
    parts.push(current.trim());
  }

  return parts.length > 0 ? parts : null;
}

/**
 * Analyze shell syntax for dangerous constructs.
 * Returns list of reasons if dangerous syntax is found.
 */
export function analyzeShellSyntax(command: string): string[] {
  const reasons: string[] = [];

  for (const { regex, reason } of DANGEROUS_SYNTAX) {
    if (regex.test(command)) {
      reasons.push(reason);
    }
  }

  return reasons;
}

/**
 * Detect dangerous command patterns.
 * Returns list of reasons if dangerous patterns are found.
 */
export function detectDangerousPatterns(command: string): string[] {
  const reasons: string[] = [];

  for (const { regex, reason } of DANGEROUS_PATTERNS) {
    if (regex.test(command)) {
      reasons.push(reason);
    }
  }

  return reasons;
}

/**
 * Main safety evaluation function.
 * Evaluates a shell command through multiple safety layers.
 */
export function evaluateCommandSafety(
  command: string,
  config?: ExecApprovalConfig,
): SafetyEvaluation {
  const allowlist = config?.allowlist ?? [];
  const allReasons: string[] = [];

  // Layer 1: Allowlist matching
  const allowlistMatch = matchAllowlist(allowlist, command);
  if (allowlistMatch) {
    return {
      riskLevel: "safe",
      reasons: [],
      analysisOk: true,
      allowlistSatisfied: true,
    };
  }

  // Layer 2: Shell syntax analysis
  const syntaxReasons = analyzeShellSyntax(command);
  const analysisOk = syntaxReasons.length === 0;
  if (!analysisOk) {
    allReasons.push(...syntaxReasons);
  }

  // Layer 3: Safe binary detection
  if (analysisOk && isSafeBinUsage(command)) {
    return {
      riskLevel: "safe",
      reasons: [],
      analysisOk: true,
      allowlistSatisfied: false,
    };
  }

  // Layer 4: Dangerous pattern detection
  const dangerousReasons = detectDangerousPatterns(command);
  allReasons.push(...dangerousReasons);

  // Determine risk level
  let riskLevel: "safe" | "needs-review" | "dangerous";
  if (dangerousReasons.length > 0 || !analysisOk) {
    riskLevel = "dangerous";
  } else {
    riskLevel = "needs-review";
  }

  return {
    riskLevel,
    reasons: allReasons,
    analysisOk,
    allowlistSatisfied: false,
  };
}

// ============ Policy Helpers ============

/**
 * Determine if human approval is required.
 * Same logic as OpenClaw's requiresExecApproval.
 */
export function requiresApproval(params: {
  ask: ExecAsk;
  security: ExecSecurity;
  analysisOk: boolean;
  allowlistSatisfied: boolean;
}): boolean {
  const { ask, security, analysisOk, allowlistSatisfied } = params;

  if (ask === "always") return true;
  if (ask === "off") return false;

  // ask === "on-miss"
  if (security === "allowlist" && (!analysisOk || !allowlistSatisfied)) return true;

  return false;
}

/**
 * Merge two security levels, taking the stricter (lower) one.
 * deny < allowlist < full
 */
export function minSecurity(a: ExecSecurity, b: ExecSecurity): ExecSecurity {
  const order: Record<ExecSecurity, number> = { deny: 0, allowlist: 1, full: 2 };
  return order[a] <= order[b] ? a : b;
}

/**
 * Merge two ask modes, taking the more frequent (higher) one.
 * off < on-miss < always
 */
export function maxAsk(a: ExecAsk, b: ExecAsk): ExecAsk {
  const order: Record<ExecAsk, number> = { off: 0, "on-miss": 1, always: 2 };
  return order[a] >= order[b] ? a : b;
}
