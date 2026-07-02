// Sanitized V8 CPU sampling profile, captured by the main process from a hung
// renderer and carried to telemetry through the freeze breadcrumb (MUL-3738).
//
// Shared across main (producer), preload, and renderer (flush) because all
// three touch it via FreezeBreadcrumb. This file is types only — the capture
// logic and the CDP-command allowlist live in main/renderer-cpu-profile.ts.
//
// PRIVACY: a CPU sampling profile is code-location data only — function names,
// script URLs, and line/column positions with hit counts. It never carries
// variable values, function arguments, user content, or heap objects. The
// producer keeps only the fields below; `url` is additionally run through the
// shared `$exception` redaction before the renderer reports it.

export interface CpuProfileCallFrame {
  /** Function name as it appears in the (possibly minified) bundle. */
  functionName: string;
  /** Script URL. Redacted (query/token-stripped) before it leaves the client. */
  url: string;
  /** 0-based line number of the function in its script. */
  lineNumber: number;
  /** 0-based column number of the function in its script. */
  columnNumber: number;
}

export interface CpuProfileNode {
  /** Node id, referenced by `samples` and parents' `children`. */
  id: number;
  /** Code location for this node. The only content-bearing risk is `url`. */
  callFrame: CpuProfileCallFrame;
  /** Number of samples that landed directly in this node. */
  hitCount: number;
  /** Child node ids, present for non-leaf nodes. */
  children?: number[];
}

export interface CpuProfilePayload {
  /** Call tree, each node a code location with a hit count. */
  nodes: CpuProfileNode[];
  /** Profiler start timestamp (µs, monotonic). */
  startTime: number;
  /** Profiler stop timestamp (µs, monotonic). */
  endTime: number;
  /** Per-sample node ids (the sampled stacks), if the engine reported them. */
  samples?: number[];
  /** Per-sample time deltas (µs), parallel to `samples`. */
  timeDeltas?: number[];
}
