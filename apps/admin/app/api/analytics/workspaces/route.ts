import { NextRequest, NextResponse } from "next/server";
import { failureClassOf, FAILURE_CLASSES } from "@multica/core/dashboard";
import { getAnalyticsWorkspaceBreakdown } from "@/lib/queries";
import type {
  AnalyticsBreakdownKind,
  AnalyticsWorkspaceBreakdownItem,
  AnalyticsWorkspaceBreakdownResult,
} from "@/lib/types";

const AUTOPILOT_SEGMENTS = new Set(["completed", "failed", "skipped", "other"]);

function autopilotSegment(status: string): string {
  if (status === "completed" || status === "failed" || status === "skipped") return status;
  return "other";
}

// GET /api/analytics/workspaces?from=<ISO>&to=<ISO>&kind=<errors|autopilotRuns>&segment=<segment>
//
// Workspace-level drill-down for one clicked chart segment. Its time bounds
// come from the selected bucket, rather than the page window, so the drawer
// reconciles exactly with the number the user clicked.
export async function GET(request: NextRequest) {
  const url = new URL(request.url);
  const fromParam = url.searchParams.get("from");
  const toParam = url.searchParams.get("to");
  const kindParam = url.searchParams.get("kind");
  const segment = url.searchParams.get("segment");
  const from = fromParam ? new Date(fromParam) : null;
  const to = toParam ? new Date(toParam) : null;

  if (!from || Number.isNaN(from.getTime()) || !to || Number.isNaN(to.getTime()) || to <= from) {
    return NextResponse.json({ error: "from/to must be valid ascending ISO timestamps" }, { status: 400 });
  }
  if (kindParam !== "errors" && kindParam !== "autopilotRuns") {
    return NextResponse.json({ error: "kind must be errors or autopilotRuns" }, { status: 400 });
  }
  if (!segment) {
    return NextResponse.json({ error: "segment is required" }, { status: 400 });
  }
  if (kindParam === "errors" && !FAILURE_CLASSES.includes(segment as (typeof FAILURE_CLASSES)[number])) {
    return NextResponse.json({ error: "invalid error segment" }, { status: 400 });
  }
  if (kindParam === "autopilotRuns" && !AUTOPILOT_SEGMENTS.has(segment)) {
    return NextResponse.json({ error: "invalid autopilot-run segment" }, { status: 400 });
  }

  try {
    const rows = await getAnalyticsWorkspaceBreakdown({
      from: from.toISOString(),
      to: to.toISOString(),
      kind: kindParam as AnalyticsBreakdownKind,
    });
    const counts = new Map<string, AnalyticsWorkspaceBreakdownItem>();
    for (const row of rows) {
      const rowSegment = kindParam === "errors" ? failureClassOf(row.segment) : autopilotSegment(row.segment);
      if (rowSegment !== segment) continue;
      const current = counts.get(row.workspaceId);
      if (current) current.count += row.count;
      else counts.set(row.workspaceId, { workspaceId: row.workspaceId, workspaceName: row.workspaceName, count: row.count });
    }
    const result: AnalyticsWorkspaceBreakdownResult = {
      items: Array.from(counts.values()).sort((a, b) => b.count - a.count || a.workspaceName.localeCompare(b.workspaceName)),
    };
    return NextResponse.json(result);
  } catch (error) {
    console.error("[admin] GET /api/analytics/workspaces failed", error);
    return NextResponse.json({ error: "Failed to load workspace breakdown" }, { status: 500 });
  }
}
