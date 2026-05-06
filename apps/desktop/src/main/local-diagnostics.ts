import type { LocalStackStatus } from "../shared/local-stack-types";
import type { LocalDataPaths } from "./local-data-paths";

/**
 * Immutable snapshot of "what's the local stack doing right now". Collected
 * on demand by the renderer (Settings → Labs → Local diagnostics) and
 * formatted as plain text for the user to paste into a bug report. Strictly
 * local — never POSTed anywhere; the local-only product has no remote
 * submission path (`feedback.allowRemoteSubmission` is false).
 */
export type LocalDiagnostics = {
  appVersion: string;
  apiUrl: string;
  os: "macos" | "windows" | "linux" | "unknown";
  paths: LocalDataPaths;
  stack: LocalStackStatus;
  daemonVersion: string | null;
  /** ISO 8601 timestamp the snapshot was collected. */
  collectedAt: string;
};

export type DiagnosticsCollectorOptions = {
  appVersion: string;
  apiUrl: string;
  os: LocalDiagnostics["os"];
  paths: LocalDataPaths;
  /** Read fresh on every snapshot — never memoized. */
  getStackStatus: () => LocalStackStatus;
  /** Returns null when the daemon CLI hasn't been resolved yet. */
  getDaemonVersion: () => string | null;
  /** Wall-clock now() — injectable for tests. */
  now?: () => Date;
};

/**
 * Construct a diagnostics collector. The collector owns no state of its own;
 * each `snapshot()` call re-reads from the injected callbacks so the renderer
 * always sees the live supervisor status, even if the user opens the
 * diagnostics tab while the stack is still warming up.
 */
export function createDiagnosticsCollector(opts: DiagnosticsCollectorOptions) {
  const now = opts.now ?? (() => new Date());

  return {
    snapshot(): LocalDiagnostics {
      return {
        appVersion: opts.appVersion,
        apiUrl: opts.apiUrl,
        os: opts.os,
        paths: opts.paths,
        stack: opts.getStackStatus(),
        daemonVersion: opts.getDaemonVersion(),
        collectedAt: now().toISOString(),
      };
    },

    /**
     * Format diagnostics as plain text suitable for copy/paste into a bug
     * report. Greppable, not JSON — humans read this. Format is intentionally
     * stable enough that grep "name (state)" works across versions.
     */
    formatAsText(diagnostics: LocalDiagnostics): string {
      const lines: string[] = [];
      lines.push("Multica local diagnostics");
      lines.push(`Collected at: ${diagnostics.collectedAt}`);
      lines.push("");
      lines.push(`App version: ${diagnostics.appVersion}`);
      lines.push(`API URL: ${diagnostics.apiUrl}`);
      lines.push(`Client OS: ${diagnostics.os}`);
      lines.push(
        `Daemon version: ${diagnostics.daemonVersion ?? "(not resolved)"}`,
      );
      lines.push("");
      lines.push(`Stack: ${diagnostics.stack.overall}`);
      for (const component of diagnostics.stack.components) {
        const detail =
          component.detail !== null && component.detail.length > 0
            ? ` — ${component.detail}`
            : "";
        lines.push(`  ${component.name} (${component.state})${detail}`);
      }
      lines.push("");
      lines.push("Paths:");
      const labels = Object.keys(diagnostics.paths) as Array<
        keyof LocalDataPaths
      >;
      for (const label of labels) {
        lines.push(`  ${label}: ${diagnostics.paths[label]}`);
      }
      return lines.join("\n");
    },
  };
}
