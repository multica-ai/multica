import { AGENT_INSTRUCTION_TEMPLATES } from "../agents/instruction-templates";
import type {
  Agent,
  Autopilot,
  AutopilotTrigger,
  CreateAgentRequest,
  CreateAutopilotRequest,
  CreateAutopilotTriggerRequest,
  IssueWorkflow,
  IssueWorkflowHandler,
  IssueWorkflowStepInput,
  IssueWorkflowWriteRequest,
  ListAutopilotsResponse,
  ListIssueWorkflowsResponse,
  Squad,
  SquadMember,
} from "../types";
import {
  CHARTER_DEFAULT_RESPONSE_HOURS,
  type CreateSquadRoutineRequest,
  type SquadCharter,
  type SquadRoutine,
  type UpdateSquadCharterRequest,
} from "../office/charter-schema";
import { freeSlots } from "../office/seat-plan";
import type {
  OfficeFloor,
  OfficeSeatAssignment,
  OfficeSeatsResponse,
} from "../office/types";
import {
  SQUAD_ROLES,
  type SquadCharterTemplate,
  type SquadRoleKey,
  type SquadRoutineTemplate,
  type SquadTemplate,
  type SquadWorkflowStep,
} from "./templates";

/** The API calls the quickstart makes; the web client satisfies it as is. */
export interface SquadQuickstartClient {
  listAgents(params?: { include_archived?: boolean }): Promise<Agent[]>;
  createAgent(data: CreateAgentRequest): Promise<Agent>;
  listSquads(): Promise<Squad[]>;
  createSquad(data: {
    name: string;
    description?: string;
    leader_id: string;
  }): Promise<Squad>;
  listOfficeFloors(): Promise<OfficeFloor[]>;
  createOfficeFloor(data: { name: string }): Promise<OfficeFloor>;
  setOfficeSeats(seats: OfficeSeatAssignment[]): Promise<OfficeSeatsResponse>;
  updateSquad(id: string, data: { instructions?: string }): Promise<Squad>;
  updateSquadCharter(squadId: string, data: UpdateSquadCharterRequest): Promise<SquadCharter>;
  createSquadRoutine(squadId: string, data: CreateSquadRoutineRequest): Promise<SquadRoutine>;
  addSquadMember(
    squadId: string,
    data: { member_type: string; member_id: string; role?: string },
  ): Promise<SquadMember>;
  listIssueWorkflows(): Promise<ListIssueWorkflowsResponse>;
  createIssueWorkflow(data: IssueWorkflowWriteRequest): Promise<IssueWorkflow>;
  listAutopilots(params?: { status?: string }): Promise<ListAutopilotsResponse>;
  createAutopilot(data: CreateAutopilotRequest): Promise<Autopilot>;
  createAutopilotTrigger(
    autopilotId: string,
    data: CreateAutopilotTriggerRequest,
  ): Promise<AutopilotTrigger>;
}

export type QuickstartAgentStatus = "created" | "reused" | "failed";

export interface QuickstartAgentOutcome {
  role: SquadRoleKey;
  name: string;
  id: string | null;
  status: QuickstartAgentStatus;
  error?: string;
}

export interface QuickstartOutcome {
  agents: QuickstartAgentOutcome[];
  squad: {
    id: string | null;
    /** "existing": a squad with this name was already there and is left as it is. */
    status: "created" | "existing" | "failed";
    /** Members that could not be added, or why the squad was not made. */
    error?: string;
  };
  workflow: {
    status: "created" | "existing" | "skipped" | "failed";
    /** "qa_step_left_out": the QA status could not be created, so the workflow has no QA step. */
    note?: "qa_step_left_out";
    error?: string;
  };
  routine: {
    /** "existing": the squad already has an autopilot with the routine's name, or the routine is already linked. */
    status: "created" | "existing" | "skipped" | "failed";
    /** A routine created without its schedule reports that here; add the schedule on the autopilot. */
    error?: string;
  };
}

export interface QuickstartOptions {
  runtimeId: string;
  /** Also create the template's workflow when the workspace has none by that name. */
  createWorkflow: boolean;
  /** IANA timezone for the routine's schedule. Defaults to the browser's; the server uses UTC when none is sent. */
  timezone?: string;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : "unknown error";
}

function errorCode(err: unknown): string | undefined {
  const body = (err as { body?: unknown } | null)?.body;
  if (body && typeof body === "object" && "code" in body) {
    const code = (body as { code?: unknown }).code;
    return typeof code === "string" ? code : undefined;
  }
  return undefined;
}

/** The browser's IANA timezone, or undefined when the runtime cannot tell. */
function browserTimezone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
  } catch {
    return undefined;
  }
}

function instructionsFor(role: SquadRoleKey): string {
  const id = SQUAD_ROLES[role].agentTemplate;
  const template = AGENT_INSTRUCTION_TEMPLATES.find((t) => t.id === id);
  if (!template) throw new Error(`no instruction template "${id}"`);
  return template.instructions;
}

/** What the quickstart would do with the agents the workspace already has. */
export function planQuickstartAgents(
  template: SquadTemplate,
  agents: readonly Agent[],
): { role: SquadRoleKey; name: string; reuse: boolean }[] {
  const live = new Set(agents.filter((a) => !a.archived_at).map((a) => a.name));
  return template.roles.map((role) => {
    const { name } = SQUAD_ROLES[role];
    return { role, name, reuse: live.has(name) };
  });
}

function workflowSteps(
  steps: readonly SquadWorkflowStep[],
  agentIds: Partial<Record<SquadRoleKey, string>>,
  withNewStatuses: boolean,
): IssueWorkflowStepInput[] | null {
  const out: IssueWorkflowStepInput[] = [];
  for (const step of steps) {
    if ("name" in step.status && !withNewStatuses) continue;
    let handler: IssueWorkflowHandler;
    if (step.handler) {
      const id = agentIds[step.handler];
      if (!id) return null;
      handler = { type: "agent", id };
    } else {
      handler = { type: "none" };
    }
    const base = {
      handler,
      instructions: step.instructions,
      ...(step.back ? { back_status_key: step.back } : {}),
    };
    out.push(
      "key" in step.status
        ? { status_key: step.status.key, ...base }
        : { new_status: { name: step.status.name, category: "started" }, ...base },
    );
  }
  return out;
}

/**
 * The office floor with this name, created when the workspace has none. The
 * server only applies a squad's floor name when every floor is full, so the
 * squad is seated on this floor by a separate seat request.
 */
async function ensureOfficeFloor(
  client: SquadQuickstartClient,
  name: string,
): Promise<OfficeFloor> {
  const floors = await client.listOfficeFloors();
  return floors.find((floor) => floor.name === name) ?? client.createOfficeFloor({ name });
}

/**
 * Creates the routine's autopilot for the squad, assigned to the squad so each
 * run opens an issue for its leader, and its weekly schedule. Then links it as
 * a routine of the squad, so the charter and the digest see it. A squad that
 * already has an autopilot with the routine's name keeps it, and it is linked
 * when it is not a routine yet.
 */
async function ensureRoutine(
  client: SquadQuickstartClient,
  squadId: string,
  routine: SquadRoutineTemplate,
  timezone: string | undefined,
): Promise<QuickstartOutcome["routine"]> {
  const { autopilots } = await client.listAutopilots();
  const found = autopilots.find(
    (a) => a.assignee_type === "squad" && a.assignee_id === squadId && a.title === routine.title,
  );
  let autopilotId: string;
  let status: "created" | "existing" = "created";
  if (found) {
    autopilotId = found.id;
    status = "existing";
  } else {
    const autopilot = await client.createAutopilot({
      title: routine.title,
      description: routine.description,
      assignee_type: "squad",
      assignee_id: squadId,
      execution_mode: "create_issue",
      issue_title_template: routine.issueTitleTemplate,
    });
    autopilotId = autopilot.id;
    try {
      await client.createAutopilotTrigger(autopilot.id, {
        kind: "schedule",
        cron_expression: routine.cron,
        ...(timezone ? { timezone } : {}),
      });
    } catch (err) {
      return { status: "failed", error: `schedule: ${errorMessage(err)}` };
    }
  }
  try {
    await client.createSquadRoutine(squadId, {
      autopilot_id: autopilotId,
      purpose: routine.title,
      expected_output: "report",
      required: true,
    });
  } catch (err) {
    // Already linked: an earlier run finished the job.
    if (errorCode(err) === "routine_already_linked") return { status: "existing" };
    return { status: "failed", error: `routine: ${errorMessage(err)}` };
  }
  return { status };
}

/** The charter a template saves on creation. Revision 0 is the first save; operating review stays on. */
function charterRequest(charter: SquadCharterTemplate): UpdateSquadCharterRequest {
  return {
    revision: 0,
    mission: charter.mission,
    responsibilities: charter.responsibilities,
    outputs: charter.outputs.map((o) => ({
      key: o.key,
      kind: o.kind,
      kb_key: "",
      cadence_hint: o.cadenceHint,
    })),
    boundaries: charter.boundaries,
    escalation: charter.escalation,
    intake: charter.intake,
    response_hours: CHARTER_DEFAULT_RESPONSE_HOURS,
  };
}

/**
 * Creates the agents a squad template needs (reusing agents that already have
 * the template's names), the squad with its instructions and roster, and
 * optionally its workflow and routine. It never edits something that already
 * exists, so running it again after a partial failure finishes the job.
 */
export async function runSquadQuickstart(
  client: SquadQuickstartClient,
  template: SquadTemplate,
  options: QuickstartOptions,
  onProgress?: (outcome: QuickstartOutcome) => void,
): Promise<QuickstartOutcome> {
  const outcome: QuickstartOutcome = {
    agents: [],
    squad: { id: null, status: "failed" },
    workflow: { status: "skipped" },
    routine: { status: "skipped" },
  };
  const report = () => onProgress?.(structuredClone(outcome));

  const existing = await client.listAgents({ include_archived: false });
  const byName = new Map(
    existing.filter((a) => !a.archived_at).map((a) => [a.name, a] as const),
  );
  const agentIds: Partial<Record<SquadRoleKey, string>> = {};

  for (const role of template.roles) {
    const { name, description } = SQUAD_ROLES[role];
    const found = byName.get(name);
    if (found) {
      agentIds[role] = found.id;
      outcome.agents.push({ role, name, id: found.id, status: "reused" });
      report();
      continue;
    }
    try {
      const created = await client.createAgent({
        name,
        description,
        instructions: instructionsFor(role),
        runtime_id: options.runtimeId,
      });
      agentIds[role] = created.id;
      outcome.agents.push({ role, name, id: created.id, status: "created" });
    } catch (err) {
      outcome.agents.push({
        role,
        name,
        id: null,
        status: "failed",
        error: errorMessage(err),
      });
    }
    report();
  }

  const leaderId = agentIds[template.leader];
  if (!leaderId) {
    outcome.squad = {
      id: null,
      status: "failed",
      error: "the squad leader could not be created",
    };
    report();
    return outcome;
  }

  try {
    const squads = await client.listSquads();
    const found = squads.find((s) => !s.archived_at && s.name === template.name);
    if (found) {
      outcome.squad = { id: found.id, status: "existing" };
    } else {
      const failures: string[] = [];
      // Set up the floor first. If that fails the squad is still created, and
      // its error says it has no seat on the floor.
      let floor: OfficeFloor | null = null;
      if (template.floorName) {
        try {
          floor = await ensureOfficeFloor(client, template.floorName);
        } catch (err) {
          failures.push(`could not set up the ${template.floorName} floor: ${errorMessage(err)}`);
        }
      }
      const squad = await client.createSquad({
        name: template.name,
        description: template.description,
        leader_id: leaderId,
      });
      outcome.squad = { id: squad.id, status: "created" };
      if (floor) {
        // Free slots come from the squads listed before the create, so the
        // new squad's own auto seat (if it is on this floor) shows as free.
        // The squad is not moved when it already holds the chosen slot.
        const slot = freeSlots(floor.id, squads)[0];
        if (slot === undefined) {
          failures.push(`no free seat on the ${template.floorName} floor`);
        } else if (squad.floor_id !== floor.id || squad.office_slot !== slot) {
          try {
            await client.setOfficeSeats([{ squad_id: squad.id, floor_id: floor.id, slot }]);
          } catch (err) {
            failures.push(`seat on the ${template.floorName} floor: ${errorMessage(err)}`);
          }
        }
      }
      try {
        await client.updateSquad(squad.id, { instructions: template.instructions });
      } catch (err) {
        failures.push(`instructions: ${errorMessage(err)}`);
      }
      // The first charter save also creates the squad's operating review.
      try {
        await client.updateSquadCharter(squad.id, charterRequest(template.charter));
      } catch (err) {
        failures.push(`charter: ${errorMessage(err)}`);
      }
      for (const role of template.roles) {
        const memberId = agentIds[role];
        if (role === template.leader || !memberId) continue;
        try {
          await client.addSquadMember(squad.id, {
            member_type: "agent",
            member_id: memberId,
            role: SQUAD_ROLES[role].role,
          });
        } catch (err) {
          failures.push(`${SQUAD_ROLES[role].name}: ${errorMessage(err)}`);
        }
      }
      if (failures.length > 0) outcome.squad.error = failures.join("; ");
    }
  } catch (err) {
    outcome.squad = { id: null, status: "failed", error: errorMessage(err) };
  }
  report();

  if (options.createWorkflow && template.workflow) {
    try {
      const { workflows } = await client.listIssueWorkflows();
      if (workflows.some((w) => w.name === template.workflow!.name)) {
        outcome.workflow = { status: "existing" };
      } else {
        const write = (withNewStatuses: boolean) => {
          const steps = workflowSteps(template.workflow!.steps, agentIds, withNewStatuses);
          if (!steps) throw new Error("a workflow step has no agent to hand to");
          return client.createIssueWorkflow({
            name: template.workflow!.name,
            description: template.workflow!.description,
            steps,
          });
        };
        try {
          await write(true);
          outcome.workflow = { status: "created" };
        } catch (err) {
          // Only a workspace admin can add a status. Without one the workflow
          // still builds and reviews; it just has no separate QA step.
          if (errorCode(err) !== "status_create_forbidden") throw err;
          await write(false);
          outcome.workflow = { status: "created", note: "qa_step_left_out" };
        }
      }
    } catch (err) {
      outcome.workflow = { status: "failed", error: errorMessage(err) };
    }
    report();
  }

  if (template.routine && outcome.squad.id) {
    try {
      outcome.routine = await ensureRoutine(
        client,
        outcome.squad.id,
        template.routine,
        options.timezone ?? browserTimezone(),
      );
    } catch (err) {
      outcome.routine = { status: "failed", error: errorMessage(err) };
    }
    report();
  }

  return outcome;
}
