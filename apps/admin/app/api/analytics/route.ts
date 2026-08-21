import { NextRequest, NextResponse } from "next/server";
import { failureClassOf, FAILURE_CLASSES } from "@multica/core/dashboard";
import { getAnalyticsTimeSeries } from "@/lib/queries";
import { listLiteLlmKeys, litellmConfigured } from "@/lib/litellm";
import {
  GRANULARITY_HOURS,
  type AnalyticsResult,
  type ErrorClassCounts,
  type GranularityHours,
} from "@/lib/types";

const MAX_BUCKETS = 500;

function emptyErrorClassCounts(): ErrorClassCounts {
  return Object.fromEntries(FAILURE_CLASSES.map((c) => [c, 0])) as ErrorClassCounts;
}

// GET /api/analytics?from=<ISO>&to=<ISO>&granularityHours=<1|3|6|12|24|168>
//
// Global (cross-workspace) time series for the Analytics page. Bucketing and
// raw failure_reason grouping happen in lib/queries.ts; this handler's job is
// input validation plus folding raw reasons into the 7 display classes the
// per-workspace Errors tab already uses (failureClassOf), so the wire
// payload is small and pre-shaped — mirrors app/api/workspaces/route.ts's
// validate-then-delegate shape.
export async function GET(request: NextRequest) {
  const url = new URL(request.url);
  const fromParam = url.searchParams.get("from");
  const toParam = url.searchParams.get("to");
  const granularityParam = Number(url.searchParams.get("granularityHours"));

  const from = fromParam ? new Date(fromParam) : null;
  const to = toParam ? new Date(toParam) : null;
  if (!from || Number.isNaN(from.getTime()) || !to || Number.isNaN(to.getTime())) {
    return NextResponse.json({ error: "from/to must be valid ISO timestamps" }, { status: 400 });
  }
  if (to.getTime() <= from.getTime()) {
    return NextResponse.json({ error: "to must be after from" }, { status: 400 });
  }
  if (!GRANULARITY_HOURS.includes(granularityParam as GranularityHours)) {
    return NextResponse.json(
      { error: `granularityHours must be one of ${GRANULARITY_HOURS.join(", ")}` },
      { status: 400 },
    );
  }
  const granularityHours = granularityParam as GranularityHours;

  // Backstop against a window/granularity combination (e.g. 1h over 90d)
  // that would ask Postgres/the client to shuffle thousands of buckets for
  // no readable benefit — same intent as MAX_UNPAGED_ROWS in lib/queries.ts.
  const bucketCount = Math.ceil((to.getTime() - from.getTime()) / (granularityHours * 3_600_000));
  if (bucketCount > MAX_BUCKETS) {
    return NextResponse.json(
      {
        error: `window/granularity would produce ${bucketCount} buckets (max ${MAX_BUCKETS}) — widen granularityHours or narrow the window`,
      },
      { status: 400 },
    );
  }

  try {
    const [rawBuckets, keys] = await Promise.all([
      getAnalyticsTimeSeries({ from: from.toISOString(), to: to.toISOString(), granularityHours }),
      listLiteLlmKeys(),
    ]);

    const buckets = rawBuckets.map((raw) => {
      const errors = emptyErrorClassCounts();
      for (const [reason, count] of Object.entries(raw.errorsByReason)) {
        errors[failureClassOf(reason)] += count;
      }
      return {
        bucketStart: raw.bucketStart,
        workspacesCreated: raw.workspacesCreated,
        issuesCreated: raw.issuesCreated,
        autopilotRuns: raw.autopilotRuns,
        errors,
      };
    });

    // listLiteLlmKeys() already degrades to [] on any proxy failure — a
    // flaky/unreachable LiteLLM proxy must not take down the time series.
    // litellmConfigured() distinguishes "genuinely $0 across all keys" from
    // "not linked at all" (null), matching this app's explicit-empty-state
    // convention (see lib/types.ts's header comment) rather than fabricating
    // a 0.
    const totalLiteLlmSpendUsd = litellmConfigured()
      ? keys.reduce((sum, k) => sum + (k.spend ?? 0), 0)
      : null;

    const result: AnalyticsResult = {
      from: from.toISOString(),
      to: to.toISOString(),
      granularityHours,
      buckets,
      totalLiteLlmSpendUsd,
    };
    return NextResponse.json(result);
  } catch (error) {
    console.error("[admin] GET /api/analytics failed", error);
    return NextResponse.json({ error: "Failed to load analytics" }, { status: 500 });
  }
}
