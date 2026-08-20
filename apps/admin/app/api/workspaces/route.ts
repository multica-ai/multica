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
  // llmKey/team fall back to "activity" here the same as any other
  // invalid/unknown value, matching what the UI enforces by not offering a
  // sort control for them. keySpend IS accepted, but takes the separate
  // in-memory sort path in GET below (see SORTABLE_COLUMNS' doc comment in
  // lib/types.ts) rather than SORT_COLUMN_SQL.
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
    // keySpend has no DB column to ORDER BY (it's resolved via the LiteLLM
    // join below) — fetch every matching workspace unpaged, join, sort in
    // memory, then paginate by slicing. Every other sort column stays on
    // the normal DB-ordered path. See SORTABLE_COLUMNS' doc comment in
    // lib/types.ts.
    //
    // `result.total` comes from the query's count(*) OVER() window, which
    // reflects every row matching WHERE regardless of the unpaged LIMIT — use
    // it as the response's `total`, not allItems.length (that's capped at
    // MAX_UNPAGED_ROWS in lib/queries.ts and would undercount once the real
    // match set exceeds the cap). If the cap IS hit, allItems only contains
    // the DB's default-ordered first MAX_UNPAGED_ROWS rows, so the in-memory
    // cost sort below is best-effort beyond that boundary — same tradeoff
    // MAX_KEY_PAGES/MAX_TEAM_PAGES already accept in lib/litellm.ts. Warn so
    // it's visible rather than silently wrong.
    if (params.sort === "keySpend") {
      const result = await listWorkspaces(params, { unpaged: true });
      const allItems = await attachLiteLlmToList(result.items);
      if (result.total > allItems.length) {
        console.warn(
          `[admin] GET /api/workspaces: keySpend sort covers ${allItems.length}/${result.total} matching workspaces (MAX_UNPAGED_ROWS cap reached)`,
        );
      }
      const dir = params.direction === "asc" ? 1 : -1;
      const sorted = [...allItems].sort((a, b) => {
        if (a.keySpend === null && b.keySpend === null) return 0;
        if (a.keySpend === null) return 1; // nulls last regardless of direction
        if (b.keySpend === null) return -1;
        return (a.keySpend - b.keySpend) * dir;
      });
      const start = (params.page - 1) * params.pageSize;
      const items = sorted.slice(start, start + params.pageSize);
      return NextResponse.json({
        items,
        total: result.total,
        page: params.page,
        pageSize: params.pageSize,
      });
    }

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
