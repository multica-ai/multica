import "./env";

import { expect, test } from "@playwright/test";
import { createTestApi } from "./helpers";
import { API_BASE } from "./phase4-helpers";
import type { TestApiClient } from "./fixtures";
import {
  api,
  apiRaw,
  claimTasks,
  scalar,
  seedBudgetSpend,
  publishMonthlyReview,
  STARTER_SQUAD_KEYS,
  seedBuiltinReading,
  sessionOf,
  utcDate,
  type Session,
} from "./phase4-helpers";

// Phase 4 acceptance over the API (spec 4.10, plan 11.21). Each scenario starts
// from its own workspace and deletes it afterwards. Setup and teardown go through
// TestApiClient; the calls that exercise the governance behaviour are raw API
// calls as the workspace owner, the same user the boss is in the product.
//
// Three kinds of step do not run here, and each skipped test says why:
// - Anything that needs WeKnora (publishing a postmortem, the search that finds it).
//   Those steps run only when KNOWLEDGE_URL is set; otherwise they are skipped.
// - The 14-day probation check, which the scheduler runs with no HTTP trigger. It is
//   a Go test (probation_test.go).
// - The hourly retrospective job is run over HTTP by POST /api/test/retro/evaluate,
//   which exists only when the server runs with MULTICA_TEST_ROUTES=1 (runbook
//   phase 4, section 0). The job's own code runs; only the clock is replaced.
//
// Where a step needs data that a nightly job would write (built-in metric readings,
// budget spend, scorecards), the test seeds the row the job would write, through
// the helpers in phase4-helpers.ts, and says so at the seed.
//
// No agent runs. A routine run is a queued task; the claim endpoint, called as the
// daemon calls it, is what admits or holds it. The claimed task's own token is what
// a reviewer uses to publish a review, as a running reviewer would.

const RUN = Date.now().toString(36);

const suffix = () => `${RUN}${Math.random().toString(36).slice(2, 6)}`;

/** A fresh workspace with an owner session and an online Hermes runtime. */
async function freshWorkspace(label: string) {
  const client = await createTestApi();
  const id = suffix();
  const workspace = await client.createWorkspace(`Phase 4 ${label} ${id}`, `e2e-phase4-${label}-${id}`);
  const runtimeId = await client.seedRuntime(`Phase 4 ${label} runtime`, "hermes");
  return { api: client, workspace, s: sessionOf(client, workspace), runtimeId };
}

/** A squad with a leader and one weekly routine whose autopilot is active. */
async function routineSquad(client: TestApiClient, runtimeId: string, name: string) {
  const leader = await client.createAgent(`${name} leader`, runtimeId);
  const squad = await client.createSquad(name, leader.id, `${name} runs its routine.`);
  const routine = await client.createSquadRoutine(squad.id, { purpose: `${name} routine`, cadence: "weekly" });
  return { leader, squad, routine };
}

/**
 * Queue a routine run. A manual "run now" is a person's act and is classed human,
 * which the company controls never hold; a run from a schedule or a webhook trigger
 * is a routine run (run_class.go). The webhook trigger is the one an API test can
 * fire, so the run comes in the way a scheduled one does, through its own trigger.
 * The delivery worker stores the run in the background, so the caller reads it back.
 */
async function postRoutineWebhook(s: Session, autopilotId: string): Promise<void> {
  const trigger = await api<{ webhook_path: string }>(s, "POST", `/api/autopilots/${autopilotId}/triggers`, {
    kind: "webhook",
    label: "e2e routine run",
  });
  const res = await fetch(`${API_BASE}${trigger.webhook_path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ event: "e2e.routine" }),
  });
  expect(res.ok, `the webhook run is accepted: ${res.status}`).toBe(true);
}

/** The newest webhook run of an autopilot, polled until `ready` accepts it. */
async function waitForWebhookRun(
  s: Session,
  autopilotId: string,
  ready: (run: any) => boolean,
  timeoutMs = 30_000,
): Promise<any> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const body = await api<any>(s, "GET", `/api/autopilots/${autopilotId}/runs`);
    const runs: any[] = Array.isArray(body) ? body : body.runs;
    const run = runs.find((r) => r.source === "webhook");
    if (run && ready(run)) return run;
    if (Date.now() > deadline) throw new Error(`webhook run not ready: ${JSON.stringify(run ?? null)}`);
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
}

/** Queue a routine run and return the task it queued. */
async function fireRoutine(s: Session, autopilotId: string): Promise<string> {
  await postRoutineWebhook(s, autopilotId);
  const run = await waitForWebhookRun(s, autopilotId, (r) => Boolean(r.task_id));
  return run.task_id as string;
}

/** The task ids a claim returned. */
function claimedIds(claim: { tasks?: { id: string }[] }): string[] {
  return (claim.tasks ?? []).map((task) => task.id);
}

// ---------------------------------------------------------------------------
// (a) Goals: progress from three kinds of reading, confidence, and the brief.
// ---------------------------------------------------------------------------

test("(a) a goal with three key results shows progress from a built-in, a manual and a webhook reading; confidence moves from on track to at risk; the brief names the cause", async () => {
  test.setTimeout(180_000);
  const { api: t, workspace, s } = await freshWorkspace("goal");
  try {
    const me = await api<{ id: string }>(s, "GET", "/api/me");
    // GET /api/metrics seeds the built-in metric rows; the readings below are written
    // against them. The nightly rollup that writes built-in readings has no trigger,
    // so the built-in reading is seeded (see phase4-helpers.ts).
    await api(s, "GET", "/api/metrics");

    // A key result names a metric that must exist, and a manual or webhook metric
    // comes into existence with its first reading. The first readings are a day old
    // and are superseded by the readings below.
    const yesterday = new Date(Date.now() - 86_400_000).toISOString();
    await api(s, "POST", "/api/metrics/e2e.signups/readings", {
      scope_type: "company", value: 0, source: "manual", name: "Signups", direction: "increase", observed_at: yesterday,
    });
    await api(s, "POST", "/api/metrics/e2e.revenue/readings", {
      scope_type: "company", value: 0, source: "webhook", name: "Webhook revenue", direction: "increase", observed_at: yesterday,
    });

    // The period runs from 50 days ago to 50 days from now, so half the period has
    // gone and the expected share is 50 per cent.
    const created = await api<any>(s, "POST", "/api/goals", {
      scope_type: "company",
      title: "Grow paying customers",
      why: "Revenue has to cover the next quarter.",
      period_start: utcDate(-50),
      period_end: utcDate(50),
      owner_type: "member",
      owner_id: me.id,
      key_results: [
        { title: "Paid runs", metric_key: "runs.count", scope_type: "company", start_value: 0, target_value: 100, weight: 1 },
        { title: "Signups", metric_key: "e2e.signups", scope_type: "company", start_value: 0, target_value: 100, weight: 1 },
        { title: "Webhook revenue", metric_key: "e2e.revenue", scope_type: "company", start_value: 0, target_value: 100, weight: 1 },
      ],
    });
    const goal = created.goal;
    expect(goal.status).toBe("draft");
    await api(s, "POST", `/api/goals/${goal.id}/activate`);

    // Readings on track: built-in 60, manual 50 (a member's report), webhook 55
    // (a member's token reporting from outside, source "webhook").
    await seedBuiltinReading(workspace.id, "runs.count", 60, new Date(Date.now() - 3_600_000));
    await api(s, "POST", "/api/metrics/e2e.signups/readings", {
      scope_type: "company", value: 50, source: "manual", name: "Signups", direction: "increase",
    });
    await api(s, "POST", "/api/metrics/e2e.revenue/readings", {
      scope_type: "company", value: 55, source: "webhook", name: "Webhook revenue", direction: "increase",
    });

    const onTrack = await api<any>(s, "GET", `/api/goals/${goal.id}`);
    expect(onTrack.goal.key_results).toHaveLength(3);
    const byKey = (key: string) => onTrack.goal.key_results.find((kr: any) => kr.metric_key === key);
    expect(byKey("runs.count").progress).toBeCloseTo(0.6, 2);
    expect(byKey("e2e.signups").progress).toBeCloseTo(0.5, 2);
    expect(byKey("e2e.revenue").progress).toBeCloseTo(0.55, 2);
    expect(onTrack.goal.confidence).toBe("on_track");

    // The readings fall behind the line: built-in 30, manual 20, webhook 25. The
    // weighted progress is 0.25: under the on-track line (0.5 less ten points, 0.4) and
    // above the at-risk floor (0.5 less thirty points, 0.2).
    await seedBuiltinReading(workspace.id, "runs.count", 30, new Date());
    await api(s, "POST", "/api/metrics/e2e.signups/readings", {
      scope_type: "company", value: 20, source: "manual", name: "Signups", direction: "increase",
    });
    await api(s, "POST", "/api/metrics/e2e.revenue/readings", {
      scope_type: "company", value: 25, source: "webhook", name: "Webhook revenue", direction: "increase",
    });

    const behind = await api<any>(s, "GET", `/api/goals/${goal.id}`);
    expect(behind.goal.confidence).toBe("at_risk");
    expect(behind.goal.progress).toBeCloseTo(0.25, 2);

    // The brief's Goals section reads the same goal and names the key result that
    // holds it back: the furthest behind its expected progress, here Signups (0.2).
    const digest = await api<any>(s, "GET", "/api/company/digest");
    const briefGoal = digest.goals.find((g: any) => g.id === goal.id);
    expect(briefGoal, "the brief lists the goal").toBeTruthy();
    expect(briefGoal.confidence).toBe("at_risk");
    expect(briefGoal.cause).toContain("Signups is at 20 of a target of 100");
  } finally {
    await t.cleanup();
    await t.deleteWorkspace();
  }
});

// ---------------------------------------------------------------------------
// (b) Budget hold, the boss's chat still working, and Extend releasing the hold.
// ---------------------------------------------------------------------------

test("(b) a squad at its budget limit holds its next routine run at claim with a visible reason, chat still works, and Extend releases the held task", async () => {
  test.setTimeout(180_000);
  const { api: t, workspace, s, runtimeId } = await freshWorkspace("budget");
  try {
    const { leader, squad, routine } = await routineSquad(t, runtimeId, "Scout squad");
    const budget = await api<any>(s, "POST", "/api/budgets", {
      scope_type: "squad",
      scope_id: squad.id,
      period: "month",
      amount_micros: 1_000_000,
      mode: "hold",
    });
    expect(budget.mode).toBe("hold");

    // The spend reaches the limit. The accounting job would book this from usage;
    // the seed writes the period's total directly (see phase4-helpers.ts).
    await seedBudgetSpend(workspace.id, budget.id, 1_000_000);

    // A routine run that starts at the limit is admitted: its issue and task are
    // created and queued. The claim gate holds the task, so the run is held, not skipped.
    await postRoutineWebhook(s, routine.autopilot_id);
    const held = await waitForWebhookRun(s, routine.autopilot_id, (r) => Boolean(r.task_id));
    expect(held.status, "the routine run is not skipped at the limit").not.toBe("skipped");
    expect(held.held, "the run is held at claim").toBe(true);
    expect(held.reason_code).toBe("budget_hold");
    const heldTask = held.task_id as string;

    const state = await api<any>(s, "GET", "/api/budgets");
    expect(state.budgets.find((b: any) => b.id === budget.id)?.state).toBe("hard");

    // The boss's chat still runs: a chat message is a human run, which the budget
    // does not hold. The claim returns the chat task and not the held routine task.
    const chat = await api<any>(s, "POST", "/api/chat/sessions", { agent_id: leader.id, title: "Budget check" });
    await api(s, "POST", `/api/chat/sessions/${chat.id}/messages`, { content: "Where does the budget stand?" });
    const chatTasks = claimedIds(await claimTasks(s, runtimeId, "e2e-phase4-daemon", 5));
    expect(chatTasks.length, "the chat task is claimed").toBeGreaterThan(0);
    expect(chatTasks, "the held routine task is not claimed").not.toContain(heldTask);

    // The hold is still in force: the run stays held and the claim leaves its task queued.
    const stillHeld = await waitForWebhookRun(s, routine.autopilot_id, (r) => Boolean(r.task_id));
    expect(stillHeld.held, "the run is still held while the limit stands").toBe(true);
    expect(claimedIds(await claimTasks(s, runtimeId))).not.toContain(heldTask);

    // Extend lifts the limit: the hold is released, the run is no longer held, and
    // the claim runs the held task.
    await api(s, "POST", `/api/budgets/${budget.id}/extend`, { amount_micros: 1_000_000 });
    const released = await waitForWebhookRun(s, routine.autopilot_id, (r) => Boolean(r.task_id));
    expect(released.held, "Extend releases the hold").toBe(false);
    expect(released.reason_code).toBeFalsy();
    expect(claimedIds(await claimTasks(s, runtimeId))).toEqual([heldTask]);

    // The log has both events: the hold when the claim refused the task, and the extension.
    const log = await api<any>(s, "GET", "/api/governance/events?limit=200");
    const kinds = log.events.map((e: any) => e.kind);
    expect(kinds).toContain("budget_hold");
    expect(kinds).toContain("budget_extended");
  } finally {
    await t.cleanup();
    await t.deleteWorkspace();
  }
});

// ---------------------------------------------------------------------------
// (c) Standing approvals: auto-approve at L2, refuse at L1, revoke, and the
// forbidden classes.
// ---------------------------------------------------------------------------

test("(c) a standing approval auto-approves a matching proposal from an L2 squad, refuses the same from an L1 squad, is revoked in one click, and never touches a launch gate or an org change", async () => {
  test.setTimeout(180_000);
  const { api: t, s, runtimeId } = await freshWorkspace("policy");
  try {
    const trusted = await routineSquad(t, runtimeId, "Trusted squad");
    const ordinary = await routineSquad(t, runtimeId, "Ordinary squad");

    // Squads start at L1. The trusted squad moves to L2 with a reason.
    const level = await api<any>(s, "GET", `/api/squads/${trusted.squad.id}/autonomy`);
    expect(level.level).toBe(1);
    await api(s, "PUT", `/api/squads/${trusted.squad.id}/autonomy`, { level: 2, reason: "Two good months of decisions." });

    const policy = await api<any>(s, "POST", "/api/approval-policies", {
      name: "Routine decisions",
      kind: "decision",
      match: { squad_ids: [trusted.squad.id, ordinary.squad.id] },
      max_per_week: 5,
    });
    expect(policy.active).toBe(true);

    const decision = (squadId: string, title: string) =>
      t.fileProposal({
        squad_id: squadId,
        title,
        summary: "A routine change inside the squad's own charter.",
        evidence: ["https://example.com/evidence"],
        recommendation: "Approve the change.",
        options: ["Approve", "Hold"],
        cost_risk: "None beyond the squad's time.",
        ask: "Approve the change.",
        kind: "decision",
        payload: {},
      });

    // The L2 squad's decision is approved by the policy.
    const approved = await decision(trusted.squad.id, "Trusted decision");
    expect(approved.state).toBe("approved");

    // The same proposal from the L1 squad is refused by the policy and waits.
    const waiting = await decision(ordinary.squad.id, "Ordinary decision");
    expect(waiting.state).toBe("pending");

    const auto = await api<any>(s, "GET", `/api/approval-policies/${policy.id}/auto-approvals`);
    expect(auto.auto_approvals.map((a: any) => a.proposal_id)).toEqual([approved.id]);

    const listed = await api<any>(s, "GET", "/api/approval-policies");
    const listedPolicy = listed.policies.find((p: any) => p.id === policy.id);
    expect(listedPolicy.week_approvals).toBe(1);

    // Forbidden kinds: a policy cannot be made for an organisation change or
    // another standing approval. A stage_gate or budget policy is allowed, and
    // the engine refuses each filing it must not approve (a launch gate, or a
    // budget over the remaining amount), so those are not refused at creation.
    for (const kind of ["org_change", "standing_approval"]) {
      const res = await apiRaw(s, "POST", "/api/approval-policies", {
        name: `Forbidden ${kind}`,
        kind,
        match: {},
        max_per_week: 1,
      });
      expect(res.status, `a policy for ${kind} is refused`).toBe(400);
    }

    // An organisation change from the L2 squad is not auto-approved, even though
    // the squad is trusted: the policy's kind is decision.
    const orgChange = await t.fileProposal({
      squad_id: trusted.squad.id,
      title: "Change the leader's instructions",
      summary: "A proposed change to an agent's instructions.",
      evidence: ["https://example.com/evidence"],
      recommendation: "Adopt the new instructions.",
      options: ["Adopt", "Keep"],
      cost_risk: "None.",
      ask: "Adopt the instructions.",
      kind: "org_change",
      payload: {
        type: "instruction_change",
        agent_id: trusted.leader.id,
        instructions: "Report in short bullet points.",
        rationale: "Shorter reports are easier to read.",
      },
    });
    expect(orgChange.state).toBe("pending");

    // Revoke in one click. The next matching proposal from the L2 squad waits.
    const revoked = await api<any>(s, "POST", `/api/approval-policies/${policy.id}/revoke`);
    expect(revoked.active).toBe(false);
    const afterRevoke = await decision(trusted.squad.id, "Decision after revoke");
    expect(afterRevoke.state).toBe("pending");

    const log = await api<any>(s, "GET", "/api/governance/events?limit=200");
    const kinds = log.events.map((e: any) => e.kind);
    expect(kinds).toContain("policy_created");
    expect(kinds).toContain("policy_auto_approve");
    expect(kinds).toContain("policy_revoked");
  } finally {
    await t.cleanup();
    await t.deleteWorkspace();
  }
});

// ---------------------------------------------------------------------------
// (d) Pause and resume, quiet hours that defer rather than drop, and chat.
// ---------------------------------------------------------------------------

test("(d) pause stops routine runs and not chat; resume restores them; quiet hours defer a run rather than drop it", async () => {
  test.setTimeout(300_000);
  const { api: t, s, runtimeId } = await freshWorkspace("pause");
  try {
    const { leader, routine } = await routineSquad(t, runtimeId, "Pause squad");

    // Paused: a routine run that starts now is admitted with its issue and task
    // queued, and the claim gate holds the task. It is not skipped. A chat message
    // still runs, and the claim does not return the held task.
    const paused = await api<any>(s, "PUT", "/api/company/company-state", { state: "paused", stop_running: false });
    expect(paused.state).toBe("paused");

    await postRoutineWebhook(s, routine.autopilot_id);
    const pausedRun = await waitForWebhookRun(s, routine.autopilot_id, (r) => Boolean(r.task_id));
    expect(pausedRun.status, "the routine run is not skipped while paused").not.toBe("skipped");
    expect(pausedRun.held, "the routine run is held at claim").toBe(true);
    expect(pausedRun.reason_code).toBe("company_paused");
    const pausedTask = pausedRun.task_id as string;

    const chat = await api<any>(s, "POST", "/api/chat/sessions", { agent_id: leader.id, title: "While paused" });
    await api(s, "POST", `/api/chat/sessions/${chat.id}/messages`, { content: "Still there?" });
    const chatTasks = claimedIds(await claimTasks(s, runtimeId, "e2e-phase4-daemon", 5));
    expect(chatTasks.length, "chat runs while the company is paused").toBeGreaterThan(0);
    expect(chatTasks, "the held routine task is not claimed while paused").not.toContain(pausedTask);

    // Resume: the held run is no longer held, and the claim runs its task.
    await api(s, "PUT", "/api/company/company-state", { state: "running", stop_running: false });
    const resumed = await waitForWebhookRun(s, routine.autopilot_id, (r) => Boolean(r.task_id));
    expect(resumed.held, "resume releases the hold").toBe(false);
    expect(claimedIds(await claimTasks(s, runtimeId))).toEqual([pausedTask]);

    // The governance log has the pause and the resume.
    const pauseLog = await api<any>(s, "GET", "/api/governance/events?limit=200");
    const pauseKinds = pauseLog.events.map((e: any) => e.kind);
    expect(pauseKinds).toContain("company_paused");
    expect(pauseKinds).toContain("company_resumed");

    // Quiet hours: a window around now defers a routine run rather than dropping
    // it. A second routine, so the first one's recent run does not count as a
    // duplicate of this one.
    const quiet = await routineSquad(t, runtimeId, "Quiet squad");
    const now = new Date();
    const clock = (minutesFromNow: number) => {
      const d = new Date(now.getTime() + minutesFromNow * 60_000);
      return `${String(d.getUTCHours()).padStart(2, "0")}:${String(d.getUTCMinutes()).padStart(2, "0")}`;
    };
    await api(s, "PUT", "/api/company/quiet-hours", {
      enabled: true, start: clock(-30), end: clock(30), timezone: "UTC",
    });
    await postRoutineWebhook(s, quiet.routine.autopilot_id);
    const deferred = await waitForWebhookRun(s, quiet.routine.autopilot_id, (r) => r.status === "deferred" || Boolean(r.task_id));
    expect(deferred.status, "quiet hours defer the routine run").toBe("deferred");
    expect(deferred.reason_code).toBe("quiet_hours");
    expect(deferred.task_id).toBeFalsy();

    // The window closes (here: quiet hours are switched off). The deferred run is
    // released by the scheduler's sweep, which queues its task; the claim then runs it.
    await api(s, "PUT", "/api/company/quiet-hours", {
      enabled: false, start: clock(-30), end: clock(30), timezone: "UTC",
    });
    const quietTask = await waitForWebhookRun(s, quiet.routine.autopilot_id, (r) => Boolean(r.task_id), 120_000).then(
      (r) => r.task_id as string,
    );
    expect(claimedIds(await claimTasks(s, runtimeId))).toEqual([quietTask]);
    const status = await scalar<string>(`SELECT status FROM agent_task_queue WHERE id = $1`, [quietTask]);
    expect(status).toBe("dispatched");
  } finally {
    await t.cleanup();
    await t.deleteWorkspace();
  }
});

// ---------------------------------------------------------------------------
// (e) Monthly review: scorecard cited, instruction change proposed with a diff,
// approved into a revision, rolled back.
// ---------------------------------------------------------------------------

test("(e) a monthly review cites its scorecard and files an instruction change with a diff; approving writes a revision; rolling back restores the earlier text", async () => {
  test.setTimeout(300_000);
  const { api: t, workspace, s, runtimeId } = await freshWorkspace("review");
  try {
    // The starter company provides the People & Performance reviewer and the Build squad.
    await t.setupCompany();
    await t.applyStarterCompany({ runtime_id: runtimeId, squads: STARTER_SQUAD_KEYS, start: "leave_paused" });

    const squads = (await t.listSquads()) as any[];
    const build = squads.find((sq) => sq.name === "Build")!;
    expect(build, "the Build squad exists").toBeTruthy();
    const leaderId = build.leader_id as string;
    const original = ((await api<any>(s, "GET", `/api/agents/${leaderId}`)).instructions ?? "") as string;
    expect(original.trim()).not.toBe("");

    const revised = "Report in short bullet points. Name the one decision you need from the boss.";
    const { review, proposalId, reviewerId } = await publishMonthlyReview({
      client: t,
      s,
      workspaceId: workspace.id,
      runtimeId,
      subject: { id: build.id, leaderId },
      instructions: revised,
      narrative: "Output is steady and the success rate is above target. The brief is hard to scan.",
    });

    // The review cites its scorecard, and it is listed to the owner.
    expect(review.scorecard_id, "the review cites a scorecard").toBeTruthy();
    expect(review.reviewer_agent_id).toBe(reviewerId);
    const reviews = await api<any>(s, "GET", "/api/reviews");
    expect(reviews.reviews.map((r: any) => r.id)).toContain(review.id);

    // The change is a proposal the boss decides. Its diff is the current instructions
    // against the proposed text, which the Reviews and Decisions views render.
    const proposal = await api<any>(s, "GET", `/api/proposals/${proposalId}`);
    expect(proposal.kind).toBe("org_change");
    expect(proposal.state).toBe("pending");
    expect(proposal.payload?.instructions ?? proposal.payload?.payload?.instructions).toBe(revised);
    expect(revised).not.toBe(original);

    // Approving writes a revision with the new text.
    const approved = await t.decideProposal(proposalId, "approve", "Adopt the format.");
    expect(approved.state).toBe("approved");
    const afterApprove = await api<any>(s, "GET", `/api/agents/${leaderId}`);
    expect(afterApprove.instructions).toBe(revised);

    const revisions = (await api<any>(s, "GET", `/api/agents/${leaderId}/instruction-revisions`)).revisions as any[];
    const originalRevision = revisions.find((r) => r.instructions === original);
    const newRevision = revisions.find((r) => r.instructions === revised);
    expect(originalRevision, "the earlier text is a revision").toBeTruthy();
    expect(newRevision, "approving wrote a revision").toBeTruthy();

    // Rolling back restores the earlier text.
    const restored = await apiRaw(s, "POST", `/api/agents/${leaderId}/instruction-revisions/${originalRevision.revision}/restore`, {});
    expect(restored.status, await restored.clone().text()).toBeLessThan(300);
    const afterRollback = await api<any>(s, "GET", `/api/agents/${leaderId}`);
    expect(afterRollback.instructions).toBe(original);
  } finally {
    await t.cleanup();
    await t.deleteWorkspace();
  }
});

test.skip(
  "[needs the 14-day probation job] (e) the probation check records an outcome for the change, and a worse outcome files a rollback proposal",
  async () => {
    // The probation check runs 14 days after an approved change, from the scheduler
    // (jobs_org_change.go), and nothing in the API triggers it. The before and after
    // comparison (improved, neutral, worse, insufficient) is covered by
    // server/internal/service/probation_test.go, which runs the job's own code.
  },
);

// ---------------------------------------------------------------------------
// (f) A failed initiative: retrospective, postmortem and search.
// ---------------------------------------------------------------------------

test("(f) a failed initiative produces a retrospective issue assigned to a leader, and a proposal the boss can decide", async () => {
  test.setTimeout(180_000);
  const { api: t, s, runtimeId } = await freshWorkspace("retro");
  try {
    // The starter pipeline is what an initiative runs through. The steps that need
    // WeKnora fail without it, and the pipeline step still installs.
    await t.setupCompany();
    await t.applyStarterCompany({ runtime_id: runtimeId, squads: ["executive_office"], start: "leave_paused" });
    const pipelines = await t.listPipelines();
    const pipeline = pipelines.find((p) => p.is_default) ?? pipelines[0];
    if (!pipeline) throw new Error("the starter company installed no pipeline");

    const ini = await t.createInitiative({
      pipeline_id: pipeline.id,
      title: `Failed pilot ${RUN}`,
      thesis: "A pilot that is meant to show demand before the build.",
    });
    const closed = await t.initiativeAction(ini.id, "close", { reason: "Failed: the pilot missed its target by half." });
    expect(closed.status, await closed.clone().text()).toBe(200);

    // The retrospective job is hourly. The test route runs the same triggers for this
    // workspace now; it does not wait for the clock.
    const ran = await api<{ fired: number }>(s, "POST", "/api/test/retro/evaluate");
    expect(ran.fired, "the close fires one retrospective").toBeGreaterThanOrEqual(1);

    // The trigger row the job wrote names the retrospective issue. Reading it from
    // the database is the one check the API has no route for.
    const issueId = await scalar<string>(
      `SELECT issue_id::text FROM retro_trigger
       WHERE workspace_id = $1 AND trigger_kind = 'initiative_closed' AND subject_id = $2
       ORDER BY fired_at DESC LIMIT 1`,
      [s.workspaceId, ini.id],
    );
    expect(issueId, "a retrospective trigger row for the closed initiative").toBeTruthy();
    const issue = await t.getIssue(issueId as string);
    expect(issue.title).toMatch(/^Retrospective: /);
    expect(issue.assignee_type).toBe("agent");
    expect(issue.assignee_id, "the retrospective has an agent to take it").toBeTruthy();

    // The retrospective asks for up to three improvement proposals. No agent runs in
    // this test, so the proposal is filed the way the leader would file it.
    const executive = (await t.listSquads()).find((sq) => sq.name === "Executive Office");
    if (!executive) throw new Error("the starter company has no Executive Office squad");
    const proposal = await t.fileProposal({
      squad_id: executive.id,
      title: `Set a target from the baseline before the next pilot (${RUN})`,
      summary: `Follows the retrospective issue for the failed pilot ${ini.title}.`,
      evidence: [`retrospective issue ${issue.title}`],
      recommendation: "Write the target and its baseline into the pilot brief before the pilot starts.",
      options: ["Adopt", "Keep the current practice"],
      cost_risk: "None.",
      ask: "Adopt the rule for the next pilot.",
    });
    expect(proposal.state).toBe("pending");
  } finally {
    await t.cleanup();
    await t.deleteWorkspace();
  }
});

// The postmortem is a published knowledge document and the search reads WeKnora.
// The test is skipped, with the reason, when WeKnora is not configured for this run
// (KNOWLEDGE_URL) or when this workspace's knowledge is not ready. The retrospective
// steps above do not depend on it.
test("(f) the postmortem is published and a later search for its topic returns it", async () => {
  test.setTimeout(180_000);
  test.skip(!process.env.KNOWLEDGE_URL, "needs WeKnora: KNOWLEDGE_URL is not set for this run");
  const { api: t, s, runtimeId } = await freshWorkspace("postmortem");
  try {
    const knowledge = await api<{ state: string }>(s, "GET", "/api/knowledge");
    test.skip(knowledge.state !== "ready", `needs WeKnora: knowledge is "${knowledge.state}" in this workspace`);
    await t.setupCompany();
    await t.applyStarterCompany({ runtime_id: runtimeId, squads: ["executive_office"], start: "leave_paused" });
    const pipelines = await t.listPipelines();
    const pipeline = pipelines.find((p) => p.is_default) ?? pipelines[0];
    if (!pipeline) throw new Error("the starter company installed no pipeline");
    const ini = await t.createInitiative({ pipeline_id: pipeline.id, title: `Postmortem pilot ${RUN}`, thesis: "A pilot to postmortem." });
    expect((await t.initiativeAction(ini.id, "close", { reason: "Failed: no demand." })).status).toBe(200);
    await api(s, "POST", "/api/test/retro/evaluate");

    // The postmortem is written by the retrospective's agent. The test writes it as
    // that agent would, as a published postmortem document.
    const topic = `postmortem topic ${RUN}`;
    const doc = await api<any>(s, "POST", "/api/knowledge/documents", {
      title: `Postmortem: ${topic}`,
      content: `# Postmortem\n\nThe ${topic} failed because demand was never tested.`,
      kind: "postmortem",
      initiative_id: ini.id,
      publish: true,
    });
    expect(doc.id, "the postmortem document is created").toBeTruthy();

    // A later search for the topic returns the published postmortem.
    const found = await api<any>(s, "GET", `/api/knowledge/search?q=${encodeURIComponent(topic)}&kind=postmortem`);
    expect(JSON.stringify(found)).toContain(`Postmortem: ${topic}`);
  } finally {
    await t.cleanup();
    await t.deleteWorkspace();
  }
});

// ---------------------------------------------------------------------------
// (g) Scale: more than 30 squads, digest_mode auto reads floors.
// ---------------------------------------------------------------------------

test("(g) with more than 30 squads, digest_mode auto reads floors, and the company digest carries each floor's brief", async () => {
  test.setTimeout(300_000);
  const { api: t, s, runtimeId } = await freshWorkspace("scale");
  try {
    await t.setupCompany();
    await t.applyStarterCompany({ runtime_id: runtimeId, squads: STARTER_SQUAD_KEYS, start: "leave_paused" });

    // Seventeen more squads on top of the fourteen starter squads: 31 in all.
    for (let i = 1; i <= 17; i += 1) {
      const leader = await t.createAgent(`Scale leader ${i}`, runtimeId);
      await t.createSquad(`Scale squad ${i}`, leader.id, `Scale squad ${i} works its own area.`);
    }

    // Above thirty squads, auto reads the floors and not the squads.
    const mode = await api<any>(s, "GET", "/api/company/digest-mode");
    expect(mode.setting).toBe("auto");
    expect(mode.active_squads).toBe(31);
    expect(mode.resolved).toBe("floors");

    // The company digest is by floor: every squad is on a floor, and the floors are the starter's six.
    const before = await api<any>(s, "GET", "/api/company/digest");
    expect(before.mode).toBe("floors");
    expect(before.floors.map((f: any) => f.name).sort()).toEqual(["Build", "Executive", "Governance", "Launch", "Product", "Research"]);
    expect(before.floors.reduce((sum: number, f: any) => sum + f.squad_count, 0)).toBe(31);
    expect(before.floors.every((f: any) => f.brief === null)).toBe(true);

    // A floor brief is published by the floor's chief, or by an admin member, who may
    // publish any floor's. The owner publishes one for each floor. Floor briefs carry
    // the floor layout: Headline, Progress, Blockers and risks, FYI.
    for (const floor of before.floors as any[]) {
      const res = await apiRaw(s, "POST", "/api/briefs", {
        kind: "daily",
        title: `${floor.name} floor`,
        body: [
          "## Headline",
          `${floor.name} floor is steady; ${floor.squad_count} squads are at work.`,
          "## Progress",
          "Routines ran to plan this week.",
          "## Blockers and risks",
          "None on this floor.",
          "## FYI",
          "Nothing for the boss this week.",
        ].join("\n"),
        scope_type: "floor",
        scope_id: floor.id,
      });
      expect(res.status, await res.clone().text()).toBe(201);
    }

    // The company digest, which the Secretary's company brief reads, now carries each
    // floor's brief with its body.
    const after = await api<any>(s, "GET", "/api/company/digest");
    for (const floor of after.floors as any[]) {
      expect(floor.brief, `floor ${floor.name} carries its brief`).toBeTruthy();
      expect(floor.brief.title).toBe(`${floor.name} floor`);
      expect(floor.brief.body).toContain(`${floor.name} floor is steady`);
    }

    // A floor-scoped digest is the floor's own view, for the floor chief's brief.
    const floorView = await api<any>(s, "GET", `/api/company/digest?floor_id=${after.floors[0].id}`);
    expect(floorView.scope.type).toBe("floor");
    expect(floorView.scope.floor_id).toBe(after.floors[0].id);

    // The company brief itself is published by the Secretary or an admin, with the company headings.
    const company = await apiRaw(s, "POST", "/api/briefs", {
      kind: "daily",
      title: "Company brief",
      body: [
        "## Headline",
        "Six floors report; no floor needs a decision.",
        "## Needs your decision",
        "Nothing today.",
        "## Progress",
        "Every floor reports a steady week.",
        "## Blockers and risks",
        "None across the company.",
        "## FYI",
        "The floor briefs are linked from the Company.",
      ].join("\n"),
    });
    expect(company.status, await company.clone().text()).toBe(201);
  } finally {
    await t.cleanup();
    await t.deleteWorkspace();
  }
});

// The Secretary writing the company brief from the floor briefs is a live model
// step. The digest it reads is covered above; the wording and the length target
// need a run of the Secretary, which is the runbook's manual step.
test.skip(
  "[needs a live model] the Secretary's company brief reads the floor briefs and stays within the length target",
  async () => {
    // Runs the Secretary's own daily brief over the floor digest and checks the
    // result against the brief length limit (briefMax) and the required headings.
  },
);
