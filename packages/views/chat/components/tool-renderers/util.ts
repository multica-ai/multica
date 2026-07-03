import type { ChatTimelineItem } from "@multica/core/chat";

/** Collapse a long path to ".../parent/file" so it fits a compact row. */
export function shortenPath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return ".../" + parts.slice(-2).join("/");
}

/** A one-line, default-visible summary of a tool call, derived from its input. */
export function getToolSummary(item: ChatTimelineItem): string {
  if (!item.input) return "";
  const inp = item.input as Record<string, string>;
  if (inp.query) return inp.query;
  if (inp.file_path) return shortenPath(inp.file_path);
  if (inp.path) return shortenPath(inp.path);
  if (inp.pattern) return inp.pattern;
  if (inp.description) return String(inp.description);
  if (inp.command) {
    const cmd = String(inp.command);
    return cmd.length > 100 ? cmd.slice(0, 100) + "..." : cmd;
  }
  if (inp.prompt) {
    const p = String(inp.prompt);
    return p.length > 100 ? p.slice(0, 100) + "..." : p;
  }
  if (inp.skill) return String(inp.skill);
  for (const v of Object.values(inp)) {
    if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
  }
  return "";
}

/**
 * Compact tool duration: sub-second in ms, otherwise seconds with one decimal
 * up to a minute, then m/s. Kept finer-grained than formatElapsedMs (which
 * floors to whole seconds) because most tool calls finish in well under a
 * second and would otherwise all read as "0s".
 */
export function formatToolDuration(ms: number): string {
  if (ms < 0) return "";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const secs = ms / 1000;
  if (secs < 60) return `${secs.toFixed(secs < 10 ? 1 : 0)}s`;
  const m = Math.floor(secs / 60);
  const s = Math.round(secs % 60);
  return `${m}m ${s}s`;
}

/** The last `n` non-empty lines of an output blob, for a zero-click preview. */
export function lastLines(text: string, n: number): string[] {
  const lines = text.replace(/\s+$/, "").split("\n");
  return lines.slice(Math.max(0, lines.length - n));
}

/**
 * JS-side reduced-motion check for animations CSS can't reach (e.g. the
 * timer-driven running spinner). CSS transitions are already stopped by
 * base.css; this only gates JS animation. Evaluated per-render so a mid-session
 * OS change is respected.
 */
export function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true
  );
}
