import { DEV_RECORDING_WINDOW_MS, type DevRecording } from "@multica/core/diagnostics";
import { apiHost, formatClock, formatDuration, formatTimestamp, requestFailed } from "./metrics";

export interface ReportEnvironment {
  build: string;
  apiUrl: string;
  route: string;
  workspaceSlug: string | null;
  role: string | null;
  userAgent: string;
  capturedAt: number;
}

export interface ReportSections {
  trail: boolean;
  errors: boolean;
  failedRequests: boolean;
}

/** What the report would attach from this recording, per section. */
export function reportCounts(recording: DevRecording, capturedAt: number) {
  const since = capturedAt - DEV_RECORDING_WINDOW_MS;
  return {
    trail: recording.navigations.filter((n) => n.at >= since).length,
    errors: recording.errors.filter((e) => e.at >= since).length,
    failedRequests: recording.requests.filter((r) => r.startedAt >= since && requestFailed(r)).length,
  };
}

function textBlock(lines: string[]): string {
  // A longer fence than any backtick run inside keeps stack traces and server
  // messages from closing the block early.
  const longestRun = Math.max(0, ...lines.join("\n").match(/`+/g)?.map((run) => run.length) ?? []);
  const fence = "`".repeat(Math.max(3, longestRun + 1));
  return [`${fence}text`, ...lines, fence].join("\n");
}

export function buildReportMarkdown({
  description,
  environment,
  recording,
  sections,
  screenshotUrl,
}: {
  description: string;
  environment: ReportEnvironment;
  recording: DevRecording;
  sections: ReportSections;
  screenshotUrl: string | null;
}): string {
  const since = environment.capturedAt - DEV_RECORDING_WINDOW_MS;
  const blocks: string[] = [];

  if (description.trim()) blocks.push(description.trim());
  if (screenshotUrl) blocks.push(`![Screenshot](${screenshotUrl})`);

  const workspace = environment.workspaceSlug
    ? `${environment.workspaceSlug}${environment.role ? ` (${environment.role})` : ""}`
    : "none";
  blocks.push(
    [
      "## Environment",
      "",
      `- Client: ${environment.build}`,
      `- API: ${apiHost(environment.apiUrl)}`,
      `- Workspace: ${workspace}`,
      `- Route: \`${environment.route}\``,
      `- Captured: ${formatTimestamp(environment.capturedAt)}`,
      `- User agent: ${environment.userAgent}`,
    ].join("\n"),
  );

  if (sections.trail) {
    const trail = recording.navigations.filter((n) => n.at >= since);
    if (trail.length > 0) {
      blocks.push(
        [
          "## Route trail",
          "",
          textBlock(
            trail.map(
              (n) =>
                `${formatClock(n.at)}  ${n.route}${n.durationMs === null ? "" : `  (${formatDuration(n.durationMs)})`}`,
            ),
          ),
        ].join("\n"),
      );
    }
  }

  if (sections.errors) {
    const errors = recording.errors.filter((e) => e.at >= since);
    if (errors.length > 0) {
      blocks.push(
        [
          `## Errors (${errors.length})`,
          "",
          textBlock(
            errors.flatMap((e) => [
              `${formatClock(e.at)}  ${e.source}  ${e.route ?? ""}`.trimEnd(),
              ...e.message.split("\n").map((line) => `  ${line}`),
              ...(e.stack ?? "")
                .split("\n")
                .filter((line) => /^\s*at\s/.test(line))
                .slice(0, 5)
                .map((line) => `    ${line.trim()}`),
            ]),
          ),
        ].join("\n"),
      );
    }
  }

  if (sections.failedRequests) {
    const failed = recording.requests.filter((r) => r.startedAt >= since && requestFailed(r));
    if (failed.length > 0) {
      blocks.push(
        [
          `## Failed requests (${failed.length})`,
          "",
          textBlock(
            failed.flatMap((r) => [
              `${formatClock(r.startedAt)}  ${r.method} ${r.path}  → ${r.status ?? "no response"}  ${formatDuration(r.durationMs ?? 0)}`,
              `  X-Request-ID: ${r.requestId}`,
              ...(r.error ? [`  ${r.error}`] : []),
            ]),
          ),
        ].join("\n"),
      );
    }
  }

  return blocks.join("\n\n");
}
