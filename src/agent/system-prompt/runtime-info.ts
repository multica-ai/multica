/**
 * Runtime Info — collection and formatting
 */

import os from "node:os";
import type { RuntimeInfo } from "./types.js";

/**
 * Collect runtime environment information.
 * Overrides take precedence over auto-detected values.
 */
export function collectRuntimeInfo(overrides?: Partial<RuntimeInfo>): RuntimeInfo {
  return {
    agentName: overrides?.agentName,
    hostName: overrides?.hostName ?? os.hostname(),
    os: overrides?.os ?? process.platform,
    arch: overrides?.arch ?? process.arch,
    nodeVersion: overrides?.nodeVersion ?? process.version,
    provider: overrides?.provider,
    model: overrides?.model,
    cwd: overrides?.cwd ?? process.cwd(),
  };
}

/**
 * Format runtime info as a single-line summary.
 *
 * Example: "Runtime: agent=multica | host=macbook | os=darwin (arm64) | node=v22.0.0 | model=anthropic/claude-3.5-sonnet | cwd=/workspace"
 */
export function formatRuntimeLine(info: RuntimeInfo): string {
  const parts: string[] = [];

  if (info.agentName) parts.push(`agent=${info.agentName}`);
  if (info.hostName) parts.push(`host=${info.hostName}`);
  if (info.os) {
    parts.push(info.arch ? `os=${info.os} (${info.arch})` : `os=${info.os}`);
  } else if (info.arch) {
    parts.push(`arch=${info.arch}`);
  }
  if (info.nodeVersion) parts.push(`node=${info.nodeVersion}`);
  if (info.model) {
    const modelStr = info.provider ? `${info.provider}/${info.model}` : info.model;
    parts.push(`model=${modelStr}`);
  }
  if (info.cwd) parts.push(`cwd=${info.cwd}`);

  return `Runtime: ${parts.join(" | ")}`;
}
