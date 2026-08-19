import { NextRequest, NextResponse } from "next/server";
import {
  getIssueMetrics,
  getRecentActivity,
  getTaskOutcomeCounts,
  getWorkspaceMetadata,
  getWorkspaceStatus,
} from "@/lib/queries";
import { findKeyForSlug } from "@/lib/litellm-join";
import { getTeamUsage, listLiteLlmKeys } from "@/lib/litellm";
import { deriveHealth, deriveSuccessRate } from "@/lib/derive";
import type { LiteLlmSection, WorkspaceDetail } from "@/lib/types";

async function buildLiteLlmSection(slug: string): Promise<LiteLlmSection> {
  const keys = await listLiteLlmKeys();
  const match = findKeyForSlug(keys, slug);
  if (!match) {
    return {
      linked: false,
      keyAlias: null,
      teamAlias: null,
      members: [],
      cost24h: null,
      cost30d: null,
      tokens24h: null,
    };
  }
  const usage = match.team_alias ? await getTeamUsage(match.team_alias) : null;
  return {
    linked: true,
    keyAlias: match.key_alias ?? null,
    teamAlias: match.team_alias ?? null,
    // LiteLLM's /key/list and /team/daily/activity responses don't carry a
    // member-username list for a team — no such field exists in the schemas
    // in lib/litellm-schema.ts. Left empty rather than invented; the UI
    // renders "No members reported" for an empty list.
    members: [],
    cost24h: usage?.cost24h ?? null,
    cost30d: usage?.cost30d ?? null,
    tokens24h: usage?.tokens24h ?? null,
  };
}

// GET /api/workspaces/[id] — Postgres detail query + LiteLLM merge, run
// concurrently. Degrades the LiteLLM section to "not linked"/null fields
// (never fabricated numbers) if the proxy isn't configured or reachable.
export async function GET(
  _request: NextRequest,
  { params }: { params: Promise<{ id: string }> },
) {
  const { id } = await params;
  try {
    const metadata = await getWorkspaceMetadata(id);
    if (!metadata) {
      return NextResponse.json({ error: "Workspace not found" }, { status: 404 });
    }

    const [status, activity, issues, outcomes, litellm] = await Promise.all([
      getWorkspaceStatus(id),
      getRecentActivity(id),
      getIssueMetrics(id),
      getTaskOutcomeCounts(id),
      buildLiteLlmSection(metadata.slug).catch((error) => {
        console.error("[admin] LiteLLM lookup failed", error);
        return {
          linked: false,
          keyAlias: null,
          teamAlias: null,
          members: [],
          cost24h: null,
          cost30d: null,
          tokens24h: null,
        } satisfies LiteLlmSection;
      }),
    ]);

    const successRate = deriveSuccessRate(outcomes.completed, outcomes.failed);
    const health = deriveHealth({
      status,
      successRate,
      avgResolutionHours: issues.avgResolutionHours,
    });

    const detail: WorkspaceDetail = {
      metadata,
      status,
      activity,
      issues,
      litellm,
      insights: { successRate, health },
    };
    return NextResponse.json(detail);
  } catch (error) {
    console.error("[admin] GET /api/workspaces/[id] failed", error);
    return NextResponse.json(
      { error: "Failed to load workspace detail" },
      { status: 500 },
    );
  }
}
