import type { AgentRuntime } from "@multica/core/types";
import { splitRuntimeName } from "@multica/views/runtimes/components/runtime-machines";

// FIR-2669: the human-facing "computer" (machine / hostname) a runtime runs on.
//
// A runtime name reads as "base (hostname)" — the runtime list's name column
// shows only the base, so the hostname is otherwise invisible. This resolves
// the computer name for the opt-in Machine column and the mobile card:
//
//   "claude-code (Jespers-MacBook)" -> "Jespers-MacBook"   (hostname in parens)
//   "Jespers-MacBook"               -> "Jespers-MacBook"   (no parens: name IS the machine)
//   device_info fallback            -> first " · "-delimited segment
//
// Returns null only when nothing identifies the machine (e.g. a cloud runtime
// with an empty name) so callers can render a neutral placeholder.
export function runtimeComputerName(runtime: AgentRuntime): string | null {
  const { base, hostname } = splitRuntimeName(runtime.name);
  if (hostname) return hostname;

  const device = runtime.device_info?.trim();
  if (device) {
    const first = device.split(" · ")[0]?.trim();
    if (first) return first;
  }

  return base.trim() || null;
}
