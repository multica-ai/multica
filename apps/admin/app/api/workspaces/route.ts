import { NextRequest, NextResponse } from "next/server";
import { listWorkspaces } from "@/lib/queries";
import { attachLiteLlmToList } from "@/lib/litellm-join";
import {
  SORTABLE_COLUMNS,
  type ListWorkspacesParams,
  type SortColumn,
  type WorkspaceStatus,
} from "@/lib/types";

const STATUSES: WorkspaceStatus[] = ["active", "idle", "error"];
const DATE_ONLY = /^\d{4}-\d{2}-\d{2}$/;

/** Validates a "YYYY-MM-DD" query param, discarding anything malformed
 * rather than passing it through to the SQL layer as a raw string. */
function parseDateOnly(value: string | null): string | undefined {
  return value && DATE_ONLY.test(value) ? value : undefined;
}

function parseParams(url: URL): ListWorkspacesParams {
  const search = url.searchParams.get("search") ?? "";
  const statusParam = url.searchParams.get("status") ?? "all";
  const status: ListWorkspacesParams["status"] = STATUSES.includes(
    statusParam as WorkspaceStatus,
  )
    ? (statusParam as WorkspaceStatus)
    : "all";
  // Only accept columns the SQL layer can actually ORDER BY (see
  // SORTABLE_COLUMNS' doc comment in lib/types.ts) — llmKey/team fall back to
  // "activity" here the same as any other invalid/unknown value, matching
  // what the UI now enforces by not offering a sort control for them.
  const sortParam = url.searchParams.get("sort") ?? "activity";
  const sort: SortColumn = SORTABLE_COLUMNS.includes(sortParam as SortColumn)
    ? (sortParam as SortColumn)
    : "activity";
  const direction = url.searchParams.get("direction") === "asc" ? "asc" : "desc";
  const page = Math.max(1, Number(url.searchParams.get("page")) || 1);
  const pageSize = Math.min(100, Math.max(1, Number(url.searchParams.get("pageSize")) || 50));
  const activityFrom = parseDateOnly(url.searchParams.get("activityFrom"));
  const activityTo = parseDateOnly(url.searchParams.get("activityTo"));
  return { search, status, sort, direction, page, pageSize, activityFrom, activityTo };
}

// GET /api/workspaces?search=&status=&sort=&direction=&page=&pageSize=
// Runs one read-only Postgres query (lib/queries.ts) then merges in a
// best-effort LiteLLM key/team lookup. Never proxies to the Go API — this
// app talks directly to Postgres + LiteLLM per the architecture decision.
export async function GET(request: NextRequest) {
  const params = parseParams(new URL(request.url));
  try {
    const result = await listWorkspaces(params);
    // attachLiteLlmToList never throws (lib/litellm.ts's listLiteLlmKeys
    // degrades to [] / partial results on any proxy failure) — a flaky or
    // unreachable LiteLLM proxy must not take down the DB-backed list, so
    // this stays outside the try/catch's "500 the whole request" path.
    const items = await attachLiteLlmToList(result.items);
    return NextResponse.json({ ...result, items });
  } catch (error) {
    console.error("[admin] GET /api/workspaces failed", error);
    return NextResponse.json(
      { error: "Failed to list workspaces" },
      { status: 500 },
    );
  }
}
