import { NextRequest, NextResponse } from "next/server";
import {
  getIssueMetrics,
  getRecentActivity,
  getTaskOutcomeCounts,
  getWorkspaceMembers,
  getWorkspaceMetadata,
  getWorkspaceStatus,
} from "@/lib/queries";
import { findKeyForSlug, resolveTeamName } from "@/lib/litellm-join";
import { listLiteLlmKeys, listLiteLlmTeams } from "@/lib/litellm";
import { deriveHealth, deriveSuccessRate } from "@/lib/derive";
import type { LiteLlmSection, WorkspaceDetail } from "@/lib/types";

async function buildLiteLlmSection(slug: string): Promise<LiteLlmSection> {
  const [keys, teams] = await Promise.all([listLiteLlmKeys(), listLiteLlmTeams()]);
  const match = findKeyForSlug(keys, slug);
  if (!match) {
    return {
      linked: false,
      keyAlias: null,
      teamAlias: null,
      keySpend: null,
      costPerTicket: null,
    };
  }
  const teamAlias = resolveTeamName(teams, match.team_id);
  return {
    linked: true,
    keyAlias: match.key_alias ?? null,
    teamAlias,
    keySpend: match.spend ?? null,
    // Filled in by the caller once issue metrics are available.
    costPerTicket: null,
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

    const [status, activity, issues, outcomes, members, litellmBase] = await Promise.all([
      getWorkspaceStatus(id),
      getRecentActivity(id),
      getIssueMetrics(id),
      getTaskOutcomeCounts(id),
      getWorkspaceMembers(id),
      buildLiteLlmSection(metadata.slug).catch((error) => {
        console.error("[admin] LiteLLM lookup failed", error);
        return {
          linked: false,
          keyAlias: null,
          teamAlias: null,
          keySpend: null,
          costPerTicket: null,
        } satisfies LiteLlmSection;
      }),
    ]);

    const successRate = deriveSuccessRate(outcomes.completed, outcomes.failed);
    const health = deriveHealth({
      status,
      successRate,
      avgResolutionHours: issues.avgResolutionHours,
    });

    const litellm: LiteLlmSection = {
      ...litellmBase,
      costPerTicket:
        litellmBase.keySpend !== null && issues.activeIssueCount > 0
          ? litellmBase.keySpend / issues.activeIssueCount
          : null,
    };

    const detail: WorkspaceDetail = {
      metadata,
      status,
      activity,
      issues,
      litellm,
      members,
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
