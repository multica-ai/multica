import { z } from "zod";
import { parseWithFallback } from "./schema";
import type { AnalyticsWorkspaceBreakdownResult } from "./types";

const AnalyticsWorkspaceBreakdownItemSchema = z.object({
  workspaceId: z.string(),
  workspaceName: z.string(),
  count: z.number(),
});

const AnalyticsWorkspaceBreakdownSchema = z.object({
  items: z.array(AnalyticsWorkspaceBreakdownItemSchema),
});

const EMPTY_ANALYTICS_WORKSPACE_BREAKDOWN: AnalyticsWorkspaceBreakdownResult = { items: [] };

export function parseAnalyticsWorkspaceBreakdown(raw: unknown): AnalyticsWorkspaceBreakdownResult {
  return parseWithFallback(raw, AnalyticsWorkspaceBreakdownSchema, EMPTY_ANALYTICS_WORKSPACE_BREAKDOWN, {
    endpoint: "/api/analytics/workspaces",
  });
}
