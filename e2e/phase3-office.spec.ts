import "./env";

import { expect, test } from "@playwright/test";
import { createTestApi } from "./helpers";
import type { TestApiClient, TestInitiative, TestSquad } from "./fixtures";

// Phase 3 acceptance over the API (spec 3.8, plan 10.20). Setup and teardown go
// through TestApiClient. Each test starts from its own empty workspace, so the
// starter company is installed from nothing; the workspace is deleted afterwards.
//
// The leaders are scripted, as in office-first-loop.spec.ts: the owner files
// proposals and makes the moves that a leader or the boss would make. No agent
// runs, so nothing depends on a live model.
//
// Covered here: the starter company's floors, squads, charters, routines, pipeline
// and knowledge kinds; a second apply that changes nothing; an approved opportunity
// that starts an initiative at Validate with its checklist; the refusal to advance
// while the checklist is open; and the stage timeline for each manual move.
//
// Not covered here, each with a reason in its skipped test below:
// - Publishing the required documents, the kind's required headings, the superseding
//   document and the restricted base need the WeKnora sidecar. Without it a document
//   write answers 409 before any heading check runs, so they are checked in the runbook.
// - The gate proposal is filed only by the hourly initiative sweep. The sweep has no
//   HTTP trigger, and a stage_gate cannot be filed by hand (FiledByAgents is false), so
//   a gate approval cannot be reached deterministically from this suite.
// - An apply killed halfway is a Go test (failStep in company_template_test.go).
// - Workspace deletion with the knowledge hold needs WeKnora.

const RUN = Date.now().toString(36);

const STARTER_SQUADS: { key: string; name: string; floor: string }[] = [
  { key: "executive_office", name: "Executive Office", floor: "Executive" },
  { key: "market_research", name: "Market Research", floor: "Research" },
  { key: "opportunity_lab", name: "Opportunity Lab", floor: "Research" },
  { key: "competitor_intelligence", name: "Competitor Intelligence", floor: "Research" },
  { key: "customer_voice", name: "Customer Voice", floor: "Research" },
  { key: "product_discovery", name: "Product Discovery", floor: "Product" },
  { key: "product_analysis", name: "Product Analysis", floor: "Product" },
  { key: "planning", name: "Planning", floor: "Product" },
  { key: "build", name: "Build", floor: "Build" },
  { key: "release_ops", name: "Release & Ops", floor: "Launch" },
  { key: "growth_gtm", name: "Growth & GTM", floor: "Launch" },
  { key: "support", name: "Support", floor: "Launch" },
  { key: "finance_risk", name: "Finance & Risk", floor: "Governance" },
  { key: "people_performance", name: "People & Performance", floor: "Governance" },
];

const STARTER_FLOORS = ["Build", "Executive", "Governance", "Launch", "Product", "Research"];

// The default pipeline (spec 3.1): each stage's owner squad and exit mode.
const PIPELINE_STAGES = [
  { key: "validate", name: "Validate", owner: "Product Discovery", exit: "gate" },
  { key: "define", name: "Define", owner: "Product Analysis", exit: "attest" },
  { key: "plan", name: "Plan", owner: "Planning", exit: "gate" },
  { key: "build", name: "Build", owner: "Build", exit: "attest" },
  { key: "launch", name: "Launch", owner: "Release & Ops", exit: "gate" },
  { key: "operate", name: "Operate", owner: "Support", exit: "gate" },
  { key: "learn", name: "Learn", owner: "Build", exit: "auto" },
];

// The 27 seeded knowledge kinds (spec 3.3).
const SEEDED_KINDS = [
  "market_report", "competitor_profile", "customer_insight", "opportunity_brief", "validation_report",
  "product_brief", "analysis", "risk_register", "prd", "roadmap", "launch_plan", "budget_request",
  "adr", "runbook", "test_report", "release_candidate", "release_notes", "launch_checklist",
  "gtm_plan", "metrics_report", "retrospective", "postmortem", "decision_record", "brief_archive",
  "policy", "playbook", "reference",
];

// The step that needs WeKnora (the ventures base) fails without it, and the steps
// before it still install. The policies step follows it and still runs.
const WEKNORA_STEP = "knowledge:ventures";

// The server requires evidence_doc_ids on an opportunity but does not resolve
// them, so a placeholder id is enough here. Real documents need WeKnora.
const EVIDENCE_DOC_ID = "e2e-market-note";

const suffix = () => `${RUN}${Math.random().toString(36).slice(2, 6)}`;

/**
 * A workspace with nothing in it, and an online Hermes runtime. The Office setup
 * needs Hermes for the Secretary; the worker agents use the same runtime here.
 */
async function emptyWorkspace() {
  const api = await createTestApi();
  const id = suffix();
  await api.createWorkspace(`Phase 3 ${id}`, `e2e-phase3-${id}`);
  const runtimeId = await api.seedRuntime("Phase 3 Hermes runtime", "hermes");
  return { api, runtimeId };
}

/**
 * The wizard's order: the Office setup (the Secretary) first, then the starter
 * company apply. The apply refuses until the Secretary exists.
 */
async function installCompany(api: TestApiClient, runtimeId: string) {
  await api.setupOffice();
  return api.applyStarterCompany({
    runtime_id: runtimeId,
    squads: STARTER_SQUADS.map((squad) => squad.key),
    start: "leave_paused",
  });
}

async function squadsByName(api: TestApiClient): Promise<Map<string, TestSquad>> {
  return new Map((await api.listSquads()).map((squad) => [squad.name, squad]));
}

function squadNamed(squads: Map<string, TestSquad>, name: string): TestSquad {
  const squad = squads.get(name);
  if (!squad) throw new Error(`squad ${name} is missing`);
  return squad;
}

/** Start an initiative from an approved opportunity filed by a squad. */
async function openInitiativeFromOpportunity(
  api: TestApiClient,
  filedBy: TestSquad,
  title: string,
  thesis: string,
): Promise<TestInitiative> {
  const proposal = await api.fileProposal({
    kind: "opportunity",
    squad_id: filedBy.id,
    title,
    summary: "A market pattern worth a validation pass.",
    evidence: ["https://example.com/market-note"],
    recommendation: "Validate the thesis with five customer interviews.",
    options: ["Validate", "Drop"],
    cost_risk: "Two weeks of research time.",
    ask: "Approve the validation pass.",
    payload: {
      thesis,
      market: "Freelance accounting",
      target_stage: "validate",
      evidence_doc_ids: [EVIDENCE_DOC_ID],
    },
  });
  expect(proposal.state).toBe("pending");

  const approved = await api.decideProposal(proposal.id, "approve", "Validate it first.");
  expect(approved.state).toBe("approved");

  const initiative = (await api.listInitiatives()).find((item) => item.title === title);
  if (!initiative) throw new Error(`no initiative titled ${title} after approval`);
  return initiative;
}

/** The parts of the company a second apply must leave as they were. */
async function companySnapshot(api: TestApiClient) {
  const [floors, squads, agents, pipelines] = await Promise.all([
    api.listFloors(),
    api.listSquads(),
    api.listAgents(),
    api.listPipelines(),
  ]);
  const routines = await Promise.all(
    squads.map(async (squad) => ({
      squad: squad.name,
      routines: await api.listSquadRoutines(squad.id),
      mission: (await api.getSquadCharter(squad.id)).mission,
    })),
  );
  return {
    floors: floors.map((floor) => ({ id: floor.id, name: floor.name, position: floor.position })).sort(byId),
    squads: squads.map((squad) => ({ id: squad.id, name: squad.name, floor_id: squad.floor_id, office_slot: squad.office_slot })).sort(byId),
    agents: agents.map((agent) => ({ id: agent.id, name: agent.name })).sort(byId),
    pipelines,
    routines,
  };
}

function byId<T extends { id: string }>(a: T, b: T) {
  return a.id.localeCompare(b.id);
}

test("the starter company installs on an empty workspace, and a second apply changes nothing", async () => {
  test.setTimeout(300_000);
  const { api, runtimeId } = await emptyWorkspace();
  try {
    expect(await api.listFloors()).toHaveLength(0);
    expect(await api.listSquads()).toHaveLength(0);

    const first = await installCompany(api, runtimeId);
    // Only the ventures step may fail. The policies step after it is the last step.
    const failed = first.steps.filter((step) => step.status === "failed");
    expect(failed.every((step) => step.key === WEKNORA_STEP)).toBe(true);
    expect(first.steps.at(-1)?.key).toBe("knowledge:policies");
    expect(first.unowned_stages).toEqual([]);
    expect(first.pipeline_id).not.toBeNull();

    // Floors.
    const floors = await api.listFloors();
    expect(floors.map((floor) => floor.name).sort()).toEqual(STARTER_FLOORS);
    const floorNameById = new Map(floors.map((floor) => [floor.id, floor.name]));

    // Squads, each on its floor.
    const squads = await squadsByName(api);
    for (const card of STARTER_SQUADS) {
      const squad = squadNamed(squads, card.name);
      expect(floorNameById.get(squad.floor_id ?? "")).toBe(card.floor);
    }

    // Charters: every squad has a mission.
    for (const card of STARTER_SQUADS) {
      const charter = await api.getSquadCharter(squadNamed(squads, card.name).id);
      expect(charter.mission.trim(), `charter of ${card.name}`).not.toBe("");
    }

    // Routines: the market scan is the one active squad routine, and every other
    // squad routine is paused. The daily brief is the Secretary's autopilot, which
    // the Office setup starts, so it is not one of the squad routines listed here.
    const activeRoutines: string[] = [];
    const pausedRoutines: string[] = [];
    for (const card of STARTER_SQUADS) {
      for (const routine of await api.listSquadRoutines(squadNamed(squads, card.name).id)) {
        if (routine.autopilot_status === "active") activeRoutines.push(routine.purpose);
        else pausedRoutines.push(routine.purpose);
      }
    }
    expect(activeRoutines).toEqual(["Market scan"]);
    expect(pausedRoutines).toEqual(expect.arrayContaining(["Operating review", "Librarian daily triage"]));
    expect((await api.officeSetup()).autopilot_daily).toBe(true);

    // Pipeline: one default pipeline, its stages in order, owners resolved to squads.
    const pipelines = await api.listPipelines();
    expect(pipelines.filter((pipeline) => pipeline.is_default)).toHaveLength(1);
    const pipeline = pipelines.find((candidate) => candidate.is_default)!;
    expect(pipeline.id).toBe(first.pipeline_id);
    expect(pipeline.stages.map((stage) => stage.key)).toEqual(PIPELINE_STAGES.map((stage) => stage.key));
    for (const expected of PIPELINE_STAGES) {
      const stage = pipeline.stages.find((candidate) => candidate.key === expected.key)!;
      expect(stage.name).toBe(expected.name);
      expect(stage.exit.mode).toBe(expected.exit);
      expect(stage.owner_squad_id).toBe(squadNamed(squads, expected.owner).id);
    }

    // Knowledge kinds: the 27 seeded kinds exist.
    const kindKeys = (await api.listKnowledgeKinds()).map((kind) => kind.key);
    expect(kindKeys).toEqual(expect.arrayContaining(SEEDED_KINDS));

    // Second apply: every step reuses what exists, and the company is unchanged.
    const before = await companySnapshot(api);
    const second = await api.applyStarterCompany({
      runtime_id: runtimeId,
      squads: STARTER_SQUADS.map((squad) => squad.key),
      start: "leave_paused",
    });
    expect(second.steps.map((step) => step.key)).toEqual(first.steps.map((step) => step.key));
    for (const step of second.steps) {
      if (step.key === WEKNORA_STEP) continue;
      expect(step.status, `step ${step.key}`).toBe("reused");
    }
    expect(second.pipeline_id).toBe(first.pipeline_id);
    expect(await companySnapshot(api)).toEqual(before);
  } finally {
    await api.cleanup();
    await api.deleteWorkspace();
  }
});

test("an approved opportunity starts an initiative at Validate, and it cannot advance until its checklist is met", async () => {
  test.setTimeout(300_000);
  const { api, runtimeId } = await emptyWorkspace();
  try {
    const apply = await installCompany(api, runtimeId);
    const squads = await squadsByName(api);
    const title = "Invoicing for freelancers";
    const thesis = "Freelancers chase late payments by hand; a reminder tool could cut that time.";

    const initiative = await openInitiativeFromOpportunity(
      api,
      squadNamed(squads, "Opportunity Lab"),
      title,
      thesis,
    );
    expect(initiative.identifier).toMatch(/^INI-\d+$/);
    expect(initiative.pipeline_id).toBe(apply.pipeline_id);
    expect(initiative.stage_key).toBe("validate");
    expect(initiative.owner_squad_id).toBe(squadNamed(squads, "Product Discovery").id);
    expect(initiative.status).toBe("active");
    expect(initiative.thesis).toBe(thesis);
    expect(initiative.issue_id).toBeTruthy();
    expect(initiative.pending_gate_proposal_id).toBeNull();

    // The checklist of Validate: two required artifacts and one optional.
    const detail = await api.getInitiative(initiative.id);
    expect(detail.artifacts.map((artifact) => artifact.stage_key)).toEqual(["validate", "validate", "validate"]);
    const checklist = detail.artifacts
      .map((artifact) => ({ kind: artifact.kind, required: artifact.required, waived: artifact.waived }))
      .sort((a, b) => a.kind.localeCompare(b.kind));
    expect(checklist).toEqual([
      { kind: "competitor_profile", required: true, waived: false },
      { kind: "customer_insight", required: false, waived: false },
      { kind: "validation_report", required: true, waived: false },
    ]);
    expect(initiative.artifacts_required).toBe(2);
    expect(initiative.artifacts_present).toBe(0);

    // The first timeline row is the creation at Validate.
    expect(detail.events.map((event) => event.kind)).toEqual(["created"]);
    expect(detail.events[0]!.to_stage).toBe("validate");

    // Advance is refused, and the refusal names the missing documents.
    const refused = await api.initiativeAction(initiative.id, "advance", { note: "Ready?" });
    expect(refused.status).toBe(409);
    const body = (await refused.json()) as { code: string; missing: string[] };
    expect(body.code).toBe("stage_not_ready");
    expect([...body.missing].sort()).toEqual(["competitor_profile", "validation_report"]);
  } finally {
    await api.cleanup();
    await api.deleteWorkspace();
  }
});

test("every manual move is recorded in the stage timeline", async () => {
  test.setTimeout(300_000);
  const { api, runtimeId } = await emptyWorkspace();
  try {
    await installCompany(api, runtimeId);
    const squads = await squadsByName(api);
    const initiative = await openInitiativeFromOpportunity(
      api,
      squadNamed(squads, "Opportunity Lab"),
      "Timeline check",
      "A thesis that is only here to be moved.",
    );

    // Admin moves: each one is a row in the timeline, in order.
    const moves: [string, Record<string, unknown>][] = [
      ["shelve", { note: "Paused while the pricing question is open." }],
      ["resume", { note: "Pricing question closed." }],
      ["force", { to_stage: "define", reason: "Skipping validation for this check." }],
      ["send-back", { note: "The thesis needs evidence before it goes further." }],
      ["close", { reason: "Timeline check complete." }],
    ];
    for (const [action, body] of moves) {
      const res = await api.initiativeAction(initiative.id, action, body);
      expect(res.ok, `${action} should succeed`).toBe(true);
    }

    const detail = await api.getInitiative(initiative.id);
    expect(detail.events.map((event) => event.kind)).toEqual([
      "created",
      "shelved",
      "resumed",
      "forced",
      "sent_back",
      "closed",
    ]);
    const forced = detail.events.find((event) => event.kind === "forced")!;
    expect([forced.from_stage, forced.to_stage]).toEqual(["validate", "define"]);
    expect(forced.actor_type).toBe("member");
    const sentBack = detail.events.find((event) => event.kind === "sent_back")!;
    expect([sentBack.from_stage, sentBack.to_stage]).toEqual(["define", "validate"]);
    expect(detail.initiative.status).toBe("closed");
    expect(detail.initiative.closed_reason).toBe("Timeline check complete.");
  } finally {
    await api.cleanup();
    await api.deleteWorkspace();
  }
});

// The steps below need the WeKnora sidecar or a scheduler run. Each is a skipped
// test so the report shows it, and each names what would make it run.

test.skip(
  "[needs WeKnora] a required document satisfies the checklist once it is published, and the gate proposal reaches the brief",
  async () => {
    // Filing a document needs knowledge ready (knowledge_write.go, workspaceClient)
    // and publishing it indexes it in WeKnora. Runbook section 3 does this by hand.
  },
);

test.skip(
  "[needs the hourly initiative sweep] approving the gate moves the backing issue, assigns the next squad and wakes its leader",
  async () => {
    // The gate is filed by InitiativeSweepJob, which runs hourly and has no HTTP trigger.
    // A stage_gate cannot be filed through POST /api/proposals (FiledByAgents is false),
    // so the gate cannot be reached here without waiting for the clock. The approval
    // path itself is in server/internal/service/proposal_kinds and initiative tests.
  },
);

test.skip(
  "[needs WeKnora] a document without the kind's required headings is rejected with the missing list",
  async () => {
    // validateKnowledgeInput runs first, then workspaceClient (refuses with 409 when
    // knowledge is not ready), then resolveWriteKind, which checks the headings.
    // Without WeKnora the heading check is never reached.
  },
);

test.skip(
  "[needs WeKnora] a superseding document hides the old one from search, and the weekly retrieval check lowers its hit rate when the expectation is wrong",
  async () => {
    // Search is WeKnora retrieval; supersession and eval runs both go through it.
  },
);

test.skip(
  "[needs WeKnora] a restricted base is invisible to a squad outside its list and visible to one inside it",
  async () => {
    // Base visibility is listed by list_knowledge_bases, which is served from WeKnora.
  },
);

test.skip(
  "[covered in Go] an apply stopped halfway completes when run again",
  async () => {
    // server/internal/service/company_template_test.go injects a failure with failStep
    // at each step. The HTTP API has no way to stop an apply partway.
  },
);

test.skip(
  "[needs WeKnora] deleting a workspace removes its tenant only after the seven-day hold",
  async () => {
    // Deprovisioning is a WeKnora tenant operation; the hold is checked in the runbook.
  },
);
