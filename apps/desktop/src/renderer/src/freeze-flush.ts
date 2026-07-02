import { normalizePageviewPath, redactText } from "@multica/core/analytics";
import type { FreezeBreadcrumb } from "../../shared/freeze-breadcrumb";
import type { CpuProfilePayload } from "../../shared/cpu-profile";

// Build the telemetry event for a freeze/crash breadcrumb parked by the main
// process in a previous session (MUL-3738). Pure so it can be unit-tested
// without the renderer; App.tsx wires the result into captureEvent on boot.
//
// Two privacy transforms happen HERE, at the "before reporting" boundary:
//   * route → bucketed template (`/:slug/inbox`, no resource ids) for P0②
//     attribution. The breadcrumb already carries a bucketed route, but we
//     re-normalize idempotently so a raw path can never slip through.
//   * cpuProfile script URLs → the same redaction the `$exception` pipeline
//     uses (strips emails / query tokens / long opaque ids) for P0①.

export interface FreezeEvent {
  name: "client_crash" | "client_unresponsive";
  props: Record<string, unknown>;
}

export function buildFreezeEventProps(last: FreezeBreadcrumb): FreezeEvent {
  const crashed = last.kind === "render-process-gone";
  const ctx = last.context ?? {};

  const props: Record<string, unknown> = {
    source: crashed ? "render-process-gone" : "main-unresponsive",
    recovered: false,
    breadcrumb_ts: last.ts,
    crashed_version: last.version,
  };

  const route = normalizePageviewPath(ctx.desktopRoute?.path);
  if (route) props.path = route;
  if (ctx.windowUrl) props.window_url = ctx.windowUrl;
  if (crashed && ctx.details?.reason) props.crash_reason = ctx.details.reason;
  if (ctx.cpuProfile) props.cpu_profile = redactCpuProfileUrls(ctx.cpuProfile);

  return {
    name: crashed ? "client_crash" : "client_unresponsive",
    props,
  };
}

function redactCpuProfileUrls(profile: CpuProfilePayload): CpuProfilePayload {
  return {
    ...profile,
    nodes: profile.nodes.map((node) => ({
      ...node,
      callFrame: {
        ...node.callFrame,
        url: redactScriptUrl(node.callFrame.url),
      },
    })),
  };
}

function redactScriptUrl(url: string): string {
  const redacted = redactText(url);
  return typeof redacted === "string" ? redacted : "";
}
