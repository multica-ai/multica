// @vitest-environment node
import { describe, expect, it, vi, type Mock } from "vitest";
import type { Agent, IssueWorkflow, Squad } from "../types";
import {
  planQuickstartAgents,
  runSquadQuickstart,
  type SquadQuickstartClient,
} from "./quickstart";
import { SQUAD_ROLES, SQUAD_TEMPLATES } from "./templates";

const greenfield = SQUAD_TEMPLATES.find((t) => t.id === "sdlc_greenfield")!;
const asAgent = (id: string, name: string, archived = false) =>
  ({ id, name, archived_at: archived ? "2026-01-01" : null }) as unknown as Agent;

type FakeClient = {
  [K in keyof SquadQuickstartClient]: Mock<SquadQuickstartClient[K]>;
};

function fakeClient(over: Partial<FakeClient> = {}): FakeClient {
  let n = 0;
  const client = {
    listAgents: vi.fn(async () => [] as Agent[]),
    createAgent: vi.fn(async (data) => asAgent(`agent-${++n}`, data.name)),
    listSquads: vi.fn(async () => [] as Squad[]),
    createSquad: vi.fn(async () => ({ id: "squad-1" }) as Squad),
    updateSquad: vi.fn(async () => ({}) as Squad),
    addSquadMember: vi.fn(async () => ({}) as never),
    listIssueWorkflows: vi.fn(async () => ({ workflows: [], total: 0 })),
    createIssueWorkflow: vi.fn(async () => ({}) as IssueWorkflow),
    listAutopilots: vi.fn(async () => ({ autopilots: [], total: 0 })),
    createAutopilot: vi.fn(async () => ({ id: "ap-1" }) as never),
    createAutopilotTrigger: vi.fn(async () => ({}) as never),
    ...over,
  } as FakeClient;
  return client;
}

const options = { runtimeId: "rt-1", createWorkflow: true };

describe("runSquadQuickstart", () => {
  it("creates the agents, the squad with its instructions and roster, and the workflow", async () => {
    const client = fakeClient();

    const out = await runSquadQuickstart(client, greenfield, options);

    expect(client.createAgent).toHaveBeenCalledTimes(greenfield.roles.length);
    expect(client.createAgent.mock.calls[0]![0]).toMatchObject({
      name: SQUAD_ROLES.lead.name,
      runtime_id: "rt-1",
    });
    expect(client.createSquad).toHaveBeenCalledWith(
      expect.objectContaining({ name: greenfield.name, leader_id: "agent-1" }),
    );
    expect(client.updateSquad).toHaveBeenCalledWith("squad-1", {
      instructions: greenfield.instructions,
    });
    expect(client.addSquadMember).toHaveBeenCalledTimes(greenfield.roles.length - 1);
    expect(client.addSquadMember).toHaveBeenCalledWith(
      "squad-1",
      expect.objectContaining({ member_type: "agent", role: SQUAD_ROLES.qa.role }),
    );
    expect(out.squad).toEqual({ id: "squad-1", status: "created" });
    expect(out.workflow.status).toBe("created");
    const steps = client.createIssueWorkflow.mock.calls[0]![0].steps!;
    expect(steps.map((s) => ("status_key" in s ? s.status_key : s.new_status.name))).toEqual([
      "todo",
      "in_review",
      "QA",
    ]);
    expect(steps[0]).toMatchObject({ handler: { type: "agent" } });
  });

  it("reuses a live agent with the same name and ignores an archived one", async () => {
    const client = fakeClient({
      listAgents: vi.fn(async () => [
        asAgent("keep", SQUAD_ROLES.analyst.name),
        asAgent("gone", SQUAD_ROLES.qa.name, true),
      ]),
    });

    const out = await runSquadQuickstart(client, greenfield, options);

    const status = (name: string) => out.agents.find((a) => a.name === name)?.status;
    expect(status(SQUAD_ROLES.analyst.name)).toBe("reused");
    expect(status(SQUAD_ROLES.qa.name)).toBe("created");
    expect(client.createAgent).toHaveBeenCalledTimes(greenfield.roles.length - 1);
  });

  it("leaves an existing squad and workflow as they are", async () => {
    const client = fakeClient({
      listSquads: vi.fn(async () => [{ id: "s", name: greenfield.name, archived_at: null } as Squad]),
      listIssueWorkflows: vi.fn(async () => ({
        workflows: [{ name: "SDLC Auto" } as IssueWorkflow],
        total: 1,
      })),
    });

    const out = await runSquadQuickstart(client, greenfield, options);

    expect(client.createSquad).not.toHaveBeenCalled();
    expect(client.updateSquad).not.toHaveBeenCalled();
    expect(client.createIssueWorkflow).not.toHaveBeenCalled();
    expect(out.squad).toEqual({ id: "s", status: "existing" });
    expect(out.workflow.status).toBe("existing");
  });

  it("builds the squad without a member whose agent could not be created", async () => {
    const client = fakeClient({
      createAgent: vi.fn(async (data) => {
        if (data.name === SQUAD_ROLES.ux.name) throw new Error("runtime offline");
        return asAgent(data.name, data.name);
      }),
    });

    const out = await runSquadQuickstart(client, greenfield, options);

    expect(out.agents.find((a) => a.role === "ux")).toMatchObject({
      status: "failed",
      error: "runtime offline",
    });
    expect(client.addSquadMember).toHaveBeenCalledTimes(greenfield.roles.length - 2);
    expect(out.squad.status).toBe("created");
  });

  it("makes no squad when the leader could not be created", async () => {
    const client = fakeClient({
      createAgent: vi.fn(async (data) => {
        if (data.name === SQUAD_ROLES.lead.name) throw new Error("no access");
        return asAgent(data.name, data.name);
      }),
    });

    const out = await runSquadQuickstart(client, greenfield, options);

    expect(client.createSquad).not.toHaveBeenCalled();
    expect(out.squad.status).toBe("failed");
    expect(out.workflow.status).toBe("skipped");
  });

  it("reports member and instruction failures but keeps the squad", async () => {
    const client = fakeClient({
      updateSquad: vi.fn(async () => {
        throw new Error("too long");
      }),
    });

    const out = await runSquadQuickstart(client, greenfield, options);

    expect(out.squad.status).toBe("created");
    expect(out.squad.error).toContain("instructions: too long");
  });

  it("builds the workflow without a QA step when the workspace admin has not allowed new statuses", async () => {
    const forbidden = Object.assign(new Error("admins only"), {
      body: { code: "status_create_forbidden" },
    });
    const createIssueWorkflow = vi
      .fn()
      .mockRejectedValueOnce(forbidden)
      .mockResolvedValueOnce({} as IssueWorkflow);
    const client = fakeClient({ createIssueWorkflow });

    const out = await runSquadQuickstart(client, greenfield, options);

    expect(out.workflow).toEqual({ status: "created", note: "qa_step_left_out" });
    expect(createIssueWorkflow.mock.calls[1]![0].steps).toHaveLength(2);
  });

  it("skips the workflow when asked not to create it or when the template has none", async () => {
    const client = fakeClient();
    const maintenance = SQUAD_TEMPLATES.find((t) => t.id === "sdlc_maintenance")!;

    expect((await runSquadQuickstart(client, greenfield, { ...options, createWorkflow: false })).workflow.status).toBe("skipped");
    expect((await runSquadQuickstart(client, maintenance, options)).workflow.status).toBe("skipped");
    expect(client.createIssueWorkflow).not.toHaveBeenCalled();
  });

  it("reports progress after each step", async () => {
    const onProgress = vi.fn();

    await runSquadQuickstart(fakeClient(), greenfield, options, onProgress);

    expect(onProgress.mock.calls.length).toBeGreaterThan(greenfield.roles.length);
    expect(onProgress.mock.calls[0]![0].agents).toHaveLength(1);
  });
});

describe("runSquadQuickstart for market research", () => {
  const market = SQUAD_TEMPLATES.find((t) => t.id === "market_research")!;

  it("leads the squad with the research lead and passes the Research floor name", async () => {
    const client = fakeClient();

    const out = await runSquadQuickstart(client, market, options);

    expect(client.createAgent.mock.calls[0]![0]).toMatchObject({
      name: SQUAD_ROLES.research_lead.name,
      runtime_id: "rt-1",
    });
    expect(client.createSquad).toHaveBeenCalledWith(
      expect.objectContaining({ name: market.name, leader_id: "agent-1", floor_name: "Research" }),
    );
    expect(out.squad).toEqual({ id: "squad-1", status: "created" });
  });

  it("creates a squad-assigned autopilot that opens an issue, then its weekly schedule", async () => {
    const client = fakeClient();

    const out = await runSquadQuickstart(client, market, { ...options, timezone: "Europe/Paris" });

    expect(client.createAutopilot).toHaveBeenCalledWith({
      title: "Market scan",
      description: market.routine!.description,
      assignee_type: "squad",
      assignee_id: "squad-1",
      execution_mode: "create_issue",
      issue_title_template: "Market scan, week of {{date}}",
    });
    expect(client.createAutopilotTrigger).toHaveBeenCalledWith("ap-1", {
      kind: "schedule",
      cron_expression: "0 8 * * 1",
      timezone: "Europe/Paris",
    });
    expect(out.routine).toEqual({ status: "created" });
  });

  it("uses the browser's timezone when the caller passes none", async () => {
    const client = fakeClient();

    await runSquadQuickstart(client, market, options);

    expect(client.createAutopilotTrigger.mock.calls[0]![1].timezone).toBe(
      Intl.DateTimeFormat().resolvedOptions().timeZone,
    );
  });

  it("keeps a routine with the same name on the squad instead of making another", async () => {
    const client = fakeClient({
      listAutopilots: vi.fn(async () => ({
        autopilots: [
          { id: "old", title: "Market scan", assignee_type: "squad", assignee_id: "squad-1" },
        ] as never[],
        total: 1,
      })),
    });

    const out = await runSquadQuickstart(client, market, options);

    expect(client.createAutopilot).not.toHaveBeenCalled();
    expect(out.routine).toEqual({ status: "existing" });
  });

  it("reports a routine whose schedule could not be set, so the boss can add it", async () => {
    const client = fakeClient({
      createAutopilotTrigger: vi.fn(async () => {
        throw new Error("bad cron");
      }),
    });

    const out = await runSquadQuickstart(client, market, options);

    expect(out.routine).toEqual({ status: "failed", error: "schedule: bad cron" });
  });

  it("makes no routine for a template without one, or when the squad was not made", async () => {
    const greenfieldClient = fakeClient();
    expect((await runSquadQuickstart(greenfieldClient, greenfield, options)).routine.status).toBe("skipped");
    expect(greenfieldClient.createAutopilot).not.toHaveBeenCalled();

    const failedClient = fakeClient({
      createSquad: vi.fn(async () => {
        throw new Error("nope");
      }),
    });
    expect((await runSquadQuickstart(failedClient, market, options)).routine.status).toBe("skipped");
    expect(failedClient.createAutopilot).not.toHaveBeenCalled();
  });

  it("sends no floor name for the SDLC squads", async () => {
    const client = fakeClient();

    await runSquadQuickstart(client, greenfield, options);

    expect(client.createSquad.mock.calls[0]![0]).not.toHaveProperty("floor_name");
  });
});

describe("planQuickstartAgents", () => {
  it("marks which agents will be reused", () => {
    const plan = planQuickstartAgents(greenfield, [
      asAgent("a", SQUAD_ROLES.lead.name),
      asAgent("b", SQUAD_ROLES.qa.name, true),
    ]);

    expect(plan.find((p) => p.role === "lead")?.reuse).toBe(true);
    expect(plan.find((p) => p.role === "qa")?.reuse).toBe(false);
    expect(plan).toHaveLength(greenfield.roles.length);
  });
});
