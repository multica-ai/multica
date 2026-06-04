"use client";

// Value #1 — "See every agent on one board". A focused, auto-playing board:
// agents are working in In Progress; one by one they finish and their card
// advances to In Review / Done while the "working" chip ticks down, then it
// loops. Built from the SAME product components the hero demo uses — real
// BoardCardContent cards, real per-status column chrome (STATUS_CONFIG), the
// real WorkspaceAgentWorkingChip — so it stays visually consistent with the
// hero board. Cards are driven by local state (no api mutation), so it's
// fully isolated from the hero demo's shared mock data.

import { useEffect, useMemo, useRef, useState } from "react";
import {
  QueryClient,
  QueryClientProvider,
  useQueryClient,
} from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { agentTaskSnapshotKeys } from "@multica/core/agents";
import {
  agentListOptions,
  memberListOptions,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { useIssueViewStore } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { RESOURCES } from "@multica/views/locales";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";
import {
  BoardCardContent,
  StatusHeading,
  WorkspaceAgentWorkingChip,
} from "@multica/views/issues/components";
import type { AgentTask, Issue, IssueStatus } from "@multica/core/types";
import { AGENTS, MEMBERS, WORKSPACE } from "./mock-data";
import { createMockApi } from "./mock-api";
import { DEMO_ZOOM } from "./zoom";

setApiInstance(createMockApi());

const WS_ID = "ws-demo";

// Natural (unscaled) size of the board canvas. 4 columns × 280 + 3 gaps × 16 +
// p-1 ×2 = 1176 wide; chip row (44) + board (718) = 762 tall. Scaled by the
// shared DEMO_ZOOM (0.85) the board ends up ~1000 × 648 — the same height as
// the hero demo, so the two boards read as the same product, not a squashed
// variant.
const NATURAL_W = 1176;
const NATURAL_H = 762;
const BOARD_H = 718;

const NOOP_NAV: NavigationAdapter = {
  push: () => {},
  replace: () => {},
  back: () => {},
  pathname: "/demo/issues",
  searchParams: new URLSearchParams(),
  getShareableUrl: (p) => p,
};

// Mirror the real board's column order/width (BOARD_COL_WIDTH = 280).
const COLUMNS: IssueStatus[] = ["todo", "in_progress", "in_review", "done"];
const COL_WIDTH = 280;
const NOW = "2026-06-01T09:00:00Z";
const STARTED = "2026-06-04T08:30:00Z";

function mk(
  n: number,
  title: string,
  status: IssueStatus,
  at: "member" | "agent",
  aid: string,
  priority: Issue["priority"],
): Issue {
  return {
    id: `vb-${n}`,
    workspace_id: WS_ID,
    number: n,
    identifier: `MUL-${n}`,
    title,
    description: null,
    status,
    priority,
    assignee_type: at,
    assignee_id: aid,
    creator_type: "member",
    creator_id: "u-alex",
    parent_issue_id: null,
    project_id: null,
    position: n,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: NOW,
    updated_at: NOW,
  };
}

const INITIAL: Issue[] = [
  // Todo
  mk(214, "Design pricing page v2", "todo", "member", "u-alex", "high"),
  mk(218, "Add SSO (SAML) to enterprise plan", "todo", "member", "u-sam", "low"),
  mk(156, "Refactor billing webhooks handler", "todo", "agent", "a-kimi", "medium"),
  mk(241, "Audit npm dependencies for CVEs", "todo", "member", "u-alex", "low"),
  // In Progress — the agents currently working
  mk(129, "Implement OAuth login flow", "in_progress", "agent", "a-claude", "high"),
  mk(133, "Migrate analytics events to new schema", "in_progress", "agent", "a-gemini", "medium"),
  mk(138, "Fix flaky checkout E2E test", "in_progress", "agent", "a-codex", "medium"),
  mk(147, "Polish onboarding empty states", "in_progress", "member", "u-alex", "medium"),
  // In Review
  mk(119, "Write API docs for webhooks", "in_review", "member", "u-sam", "medium"),
  mk(124, "Weekly dependency upgrade", "in_review", "agent", "a-claude", "medium"),
  mk(122, "Improve search relevance scoring", "in_review", "member", "u-alex", "low"),
  // Done
  mk(108, "Ship dark-mode polish", "done", "member", "u-alex", "medium"),
  mk(131, "Nightly DB backup job", "done", "agent", "a-gemini", "medium"),
  mk(116, "Fix mobile nav overflow", "done", "member", "u-sam", "low"),
];

// Each step advances one card. The "working" chip ticks 3 → 2 → 1 as agents
// finish (Codex then Claude); Gemini stays working through the loop so the
// chip never reads "0", then everything resets.
const MOVES: { id: string; to: IssueStatus }[] = [
  { id: "vb-138", to: "in_review" },
  { id: "vb-138", to: "done" },
  { id: "vb-129", to: "in_review" },
  { id: "vb-129", to: "done" },
];
const STEP_MS = 2200;

// Build the running-task snapshot the real working chip reads from, derived
// from whichever agent-assigned cards are currently in progress.
function runningTasksFor(issues: Issue[]): AgentTask[] {
  return issues
    .filter((i) => i.assignee_type === "agent" && i.status === "in_progress")
    .map(
      (i) =>
        ({
          id: `vt-${i.id}`,
          agent_id: i.assignee_id,
          runtime_id: "rt-demo",
          issue_id: i.id,
          status: "running",
          priority: 0,
          dispatched_at: STARTED,
          started_at: STARTED,
          completed_at: null,
          result: null,
          error: null,
          created_at: NOW,
          updated_at: NOW,
        }) as unknown as AgentTask,
    );
}

export function ValueBoardDemo() {
  const queryClient = useMemo(() => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    qc.setQueryData(workspaceListOptions().queryKey, [WORKSPACE]);
    qc.setQueryData(memberListOptions(WS_ID).queryKey, MEMBERS);
    qc.setQueryData(agentListOptions(WS_ID).queryKey, AGENTS);
    qc.setQueryData(agentTaskSnapshotKeys.list(WS_ID), runningTasksFor(INITIAL));
    return qc;
  }, []);

  const resources = useMemo(() => ({ en: RESOURCES.en }), []);

  return (
    // Scale the natural-size board down by the shared DEMO_ZOOM so it renders at
    // the same scale as the hero demo. The visible box is sized to the scaled
    // dimensions and centered; the inner box lays out at full size.
    <div
      className="mx-auto overflow-hidden"
      style={{ width: NATURAL_W * DEMO_ZOOM, height: NATURAL_H * DEMO_ZOOM, maxWidth: "100%" }}
    >
      <div
        className="origin-top-left"
        style={{ width: NATURAL_W, transform: `scale(${DEMO_ZOOM})` }}
      >
        <QueryClientProvider client={queryClient}>
          <I18nProvider locale="en" resources={resources}>
            <NavigationProvider value={NOOP_NAV}>
              <WorkspaceSlugProvider slug="demo">
                <ViewStoreProvider store={useIssueViewStore}>
                  <Board />
                </ViewStoreProvider>
              </WorkspaceSlugProvider>
            </NavigationProvider>
          </I18nProvider>
        </QueryClientProvider>
      </div>
    </div>
  );
}

function Board() {
  const qc = useQueryClient();
  const [issues, setIssues] = useState<Issue[]>(INITIAL);
  const [movedId, setMovedId] = useState<string | null>(null);
  const stepRef = useRef(0);

  // Auto-play: advance one card per tick, loop after the last move.
  useEffect(() => {
    const tick = () => {
      const step = stepRef.current;
      if (step >= MOVES.length) {
        stepRef.current = 0;
        setIssues(INITIAL);
        setMovedId(null);
        return;
      }
      const { id, to } = MOVES[step]!;
      stepRef.current = step + 1;
      setIssues((prev) => prev.map((i) => (i.id === id ? { ...i, status: to } : i)));
      setMovedId(id);
      window.setTimeout(() => setMovedId((m) => (m === id ? null : m)), 650);
    };
    const interval = window.setInterval(tick, STEP_MS);
    return () => window.clearInterval(interval);
  }, []);

  // Keep the working chip's snapshot in sync with the in-progress agents.
  useEffect(() => {
    qc.setQueryData(agentTaskSnapshotKeys.list(WS_ID), runningTasksFor(issues));
  }, [issues, qc]);

  return (
    // Non-interactive: this is a playing illustration, not the interactive demo.
    <div className="pointer-events-none select-none">
      <div className="mb-3 flex items-center px-1">
        <WorkspaceAgentWorkingChip value={false} onToggle={() => {}} />
      </div>
      {/* Pinned height so the board never reflows as cards move between
          columns; columns stretch to fill and scroll internally if needed. */}
      <div className="flex gap-4 overflow-x-auto p-1" style={{ height: BOARD_H }}>
        {COLUMNS.map((status) => {
          const cards = issues.filter((i) => i.status === status);
          const cfg = STATUS_CONFIG[status];
          return (
            <div
              key={status}
              style={{ width: COL_WIDTH }}
              className={`flex shrink-0 flex-col rounded-xl ${cfg?.columnBg ?? "bg-muted/40"} p-2`}
            >
              <div className="mb-2 flex items-center px-1.5">
                <StatusHeading status={status} count={cards.length} />
              </div>
              <div className="relative flex-1 rounded-lg">
                <div className="absolute inset-0 space-y-2 overflow-y-auto rounded-lg p-1">
                  {cards.map((issue) => (
                    <div
                      key={issue.id}
                      className={`group/card ${movedId === issue.id ? "newhome-card-land" : ""}`}
                    >
                      <BoardCardContent issue={issue} />
                    </div>
                  ))}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
