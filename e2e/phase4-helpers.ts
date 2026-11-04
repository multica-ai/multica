import "./env";

import pg from "pg";
import { TestApiClient } from "./fixtures";

// Helpers for the phase 4 governance suites. The API is called with the same
// token and workspace selector TestApiClient uses; the seeds below are the only
// writes that go around the API, and each one says why it is needed where it is
// used. Setup and teardown still go through TestApiClient.

export const API_BASE = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
const DATABASE_URL = process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

export interface Session {
  token: string;
  slug: string;
  workspaceId: string;
}

/** The signed-in session of a TestApiClient for the workspace it selected. */
export function sessionOf(api: TestApiClient, workspace: { id: string; slug: string }): Session {
  const token = api.getToken();
  if (!token) throw new Error("the test API client is not logged in");
  return { token, slug: workspace.slug, workspaceId: workspace.id };
}

/** A raw API call as the session's user. Returns the response; the caller decides. */
export async function apiRaw(s: Session, method: string, path: string, body?: unknown, extraHeaders: Record<string, string> = {}) {
  return fetch(`${API_BASE}${path}`, {
    method,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${s.token}`,
      "X-Workspace-Slug": s.slug,
      ...extraHeaders,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

/** A JSON API call that throws on a non-2xx answer. */
export async function api<T = any>(s: Session, method: string, path: string, body?: unknown): Promise<T> {
  const res = await apiRaw(s, method, path, body);
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path} failed: ${res.status} ${text}`);
  return (text ? JSON.parse(text) : null) as T;
}

async function withDb<T>(fn: (client: pg.Client) => Promise<T>): Promise<T> {
  const client = new pg.Client(DATABASE_URL);
  await client.connect();
  try {
    return await fn(client);
  } finally {
    await client.end();
  }
}

/**
 * Seed a built-in metric reading. Built-in readings come from the nightly rollup,
 * which has no HTTP trigger, and the API refuses to write them (ErrMetricBuiltinReadOnly).
 * The metric row itself is created by GET /api/metrics, which seeds the built-ins.
 */
export async function seedBuiltinReading(workspaceId: string, key: string, value: number, observedAt: Date) {
  await withDb((db) =>
    db.query(
      `INSERT INTO metric_reading (workspace_id, metric_key, scope_type, scope_id, value, observed_at, actor_type, actor_id, note)
       VALUES ($1, $2, 'company', NULL, $3, $4, 'system', NULL, 'e2e seed of the nightly rollup')`,
      [workspaceId, key, value, observedAt.toISOString()],
    ),
  );
}

/**
 * Set a budget period's spend. The accounting job books task usage into the same
 * row on its own schedule and adds only the difference, so a seeded spend stays.
 * The period is the calendar month (UTC) the budget's gate reads.
 */
export async function seedBudgetSpend(workspaceId: string, budgetId: string, spentMicros: number) {
  await withDb(async (db) => {
    const period = await db.query<{ start: Date; end: Date }>(
      `SELECT date_trunc('month', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS start,
              (date_trunc('month', now() AT TIME ZONE 'UTC') + interval '1 month') AT TIME ZONE 'UTC' AS end`,
    );
    const { start, end } = period.rows[0]!;
    const updated = await db.query(
      `UPDATE budget_period SET spent_micros = $1, runs = 1
       WHERE budget_id = $2 AND period_start = $3`,
      [spentMicros, budgetId, start],
    );
    if (updated.rowCount === 0) {
      await db.query(
        `INSERT INTO budget_period (budget_id, workspace_id, period_start, period_end, spent_micros, runs)
         VALUES ($1, $2, $3, $4, $5, 1)`,
        [budgetId, workspaceId, start, end, spentMicros],
      );
    }
  });
}

/**
 * Seed a scorecard row. The nightly scorecard job has no HTTP trigger, so a test
 * writes the row the job would write: the same metrics document and status.
 */
export async function seedScorecard(input: {
  workspaceId: string;
  subjectType: "agent" | "squad" | "company";
  subjectId: string;
  periodStart: string;
  periodEnd: string;
  sampleSize: number;
  metrics: Record<string, { value: number | null; sample: number }>;
}) {
  const document = { metrics: input.metrics, failures_by_cause: {} };
  await withDb((db) =>
    db.query(
      `INSERT INTO scorecard (workspace_id, subject_type, subject_id, period, period_start, period_end, metrics, sample_size, status)
       VALUES ($1, $2, $3, 'month', $4, $5, $6, $7, 'ok')`,
      [input.workspaceId, input.subjectType, input.subjectId, input.periodStart, input.periodEnd, JSON.stringify(document), input.sampleSize],
    ),
  );
}

/** Read one scalar with SQL. Used to check what the API must have written. */
export async function scalar<T = unknown>(sql: string, params: unknown[] = []): Promise<T | undefined> {
  return withDb(async (db) => {
    const res = await db.query(sql, params);
    return res.rows[0] ? (Object.values(res.rows[0])[0] as T) : undefined;
  });
}

/** Today's date in UTC as YYYY-MM-DD, offset by a number of days. */
export function utcDate(offsetDays = 0): string {
  const d = new Date();
  d.setUTCDate(d.getUTCDate() + offsetDays);
  return d.toISOString().slice(0, 10);
}

/**
 * Claim queued tasks for a runtime as the signed-in user, the same call the daemon
 * makes. A refused budget or a paused company leaves the task queued, so the claim
 * returns no task for it. The response carries the task and its task token.
 */
export async function claimTasks(s: Session, runtimeId: string, daemonId = "e2e-phase4-daemon", maxTasks = 1) {
  return api<any>(s, "POST", "/api/daemon/tasks/claim", {
    daemon_id: daemonId,
    runtime_ids: [runtimeId],
    max_tasks: maxTasks,
  });
}

/**
 * Publish a monthly review of a squad as its workspace's reviewer, the way a running
 * reviewer does. The scorecard the nightly job would write for last month is seeded;
 * the reviewer's run is an issue assigned to the reviewer, and the claim gives that
 * task's own token, which the publish call carries. The review proposes an
 * instruction change for the squad's leader. Returns the review and its proposal.
 */
export async function publishMonthlyReview(input: {
  client: TestApiClient;
  s: Session;
  workspaceId: string;
  runtimeId: string;
  subject: { id: string; leaderId: string };
  instructions: string;
  narrative: string;
  rating?: "exceeds" | "meets" | "below" | "at_risk";
}): Promise<{ review: any; proposalId: string; reviewerId: string; periodStart: string }> {
  const reviewerId = await scalar<string>(
    `SELECT sm.member_id::text FROM squad_member sm JOIN squad sq ON sq.id = sm.squad_id
     WHERE sq.workspace_id = $1 AND sm.role = 'reviewer' AND sm.member_type = 'agent' LIMIT 1`,
    [input.workspaceId],
  );
  if (!reviewerId) throw new Error("the workspace has no reviewer agent");

  const now = new Date();
  const lastMonthStart = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 1, 1));
  const thisMonthStart = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1));
  const periodStart = lastMonthStart.toISOString().slice(0, 10);
  await seedScorecard({
    workspaceId: input.workspaceId,
    subjectType: "squad",
    subjectId: input.subject.id,
    periodStart,
    periodEnd: thisMonthStart.toISOString().slice(0, 10),
    sampleSize: 40,
    metrics: {
      runs: { value: 40, sample: 40 },
      success_rate: { value: 0.85, sample: 40 },
      issues_done: { value: 12, sample: 40 },
    },
  });

  await input.client.createIssue("Monthly review", { assignee_type: "agent", assignee_id: reviewerId });
  const claim = await claimTasks(input.s, input.runtimeId, "e2e-phase4-daemon", 10);
  const reviewerTask = (claim.tasks as any[]).find((task) => task.agent_id === reviewerId);
  if (!reviewerTask) throw new Error("the reviewer's task was not claimed");
  const reviewerSession: Session = { ...input.s, token: reviewerTask.auth_token as string };

  const published = await apiRaw(reviewerSession, "POST", "/api/reviews", {
    subject_type: "squad",
    subject_id: input.subject.id,
    period: "month",
    period_start: periodStart,
    rating: input.rating ?? "meets",
    narrative: input.narrative,
    cited_metrics: ["runs", "success_rate"],
    recommendations: [
      {
        type: "instruction_change",
        rationale: "The brief is hard to scan; short bullets with one decision name fix it.",
        payload: {
          type: "instruction_change",
          agent_id: input.subject.leaderId,
          instructions: input.instructions,
          diff_summary: "Ask for bullets and one named decision.",
          rationale: "The scorecard shows steady output; the change is about format.",
        },
      },
    ],
  });
  if (published.status !== 201) throw new Error(`publish review failed: ${published.status} ${await published.text()}`);
  const review = ((await published.json()) as any).review;
  const proposalId = review.recommendations[0].proposal_id as string;
  if (!proposalId) throw new Error("the review's change did not become a proposal");
  return { review, proposalId, reviewerId, periodStart };
}

/** The fourteen starter squads (spec 3.4), for a starter company install. */
export const STARTER_SQUAD_KEYS = [
  "executive_office", "market_research", "opportunity_lab", "competitor_intelligence", "customer_voice",
  "product_discovery", "product_analysis", "planning", "build", "release_ops", "growth_gtm", "support",
  "finance_risk", "people_performance",
];
