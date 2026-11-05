// @vitest-environment node
import { describe, expect, it } from "vitest";
import { AGENT_INSTRUCTION_TEMPLATES } from "../agents/instruction-templates";
import {
  SDLC_AUTO_WORKFLOW,
  SQUAD_ROLES,
  SQUAD_TEMPLATES,
  type SquadTemplate,
} from "./templates";

const template = (id: SquadTemplate["id"]) =>
  SQUAD_TEMPLATES.find((t) => t.id === id)!;

describe("SQUAD_TEMPLATES", () => {
  it("gives every template a unique id and a squad name", () => {
    const ids = SQUAD_TEMPLATES.map((t) => t.id);

    expect(new Set(ids).size).toBe(ids.length);
    for (const t of SQUAD_TEMPLATES) expect(t.name.trim()).not.toBe("");
  });

  it("starts every squad from a leader and only known roles", () => {
    for (const t of SQUAD_TEMPLATES) {
      expect(t.roles, t.id).toContain(t.leader);
      expect(t.roles[0], t.id).toBe(t.leader);
      expect(new Set(t.roles).size, t.id).toBe(t.roles.length);
      for (const role of t.roles) expect(SQUAD_ROLES[role], `${t.id}: ${role}`).toBeDefined();
    }
  });

  it("builds every role from an existing agent instruction template", () => {
    const ids = new Set(AGENT_INSTRUCTION_TEMPLATES.map((t) => t.id));
    const names = new Set<string>();

    for (const role of Object.values(SQUAD_ROLES)) {
      expect(ids.has(role.agentTemplate), role.key).toBe(true);
      expect([...role.description].length, role.key).toBeLessThanOrEqual(255);
      expect(names.has(role.name), role.name).toBe(false);
      names.add(role.name);
    }
  });

  it("hands every workflow step to a role its squad has", () => {
    for (const t of SQUAD_TEMPLATES) {
      if (!t.workflow) continue;
      for (const step of t.workflow.steps) {
        if (step.handler) expect(t.roles, t.id).toContain(step.handler);
      }
    }
  });

  it("keeps each workflow step within the step instruction limit", () => {
    for (const step of SDLC_AUTO_WORKFLOW.steps) {
      expect([...step.instructions].length).toBeLessThanOrEqual(8000);
    }
  });

  it("ends the build workflow on an agent step so an agent run can close the issue", () => {
    const last = SDLC_AUTO_WORKFLOW.steps.at(-1);

    expect(last?.handler).toBe("qa");
    expect(SDLC_AUTO_WORKFLOW.steps.every((s) => s.handler !== null)).toBe(true);
  });
});

describe("market research template", () => {
  const market = template("market_research");

  it("leads with the research lead and seats the squad on a Research floor", () => {
    expect(market.leader).toBe("research_lead");
    expect(market.roles).toEqual(["research_lead", "scout", "competitor", "critic"]);
    expect(market.floorName).toBe("Research");
    expect(market.workflow).toBeNull();
  });

  it("runs a Monday-morning routine that opens a dated issue for the squad", () => {
    const routine = market.routine!;

    expect(routine.cron).toBe("0 8 * * 1");
    expect(routine.issueTitleTemplate).toContain("{{date}}");
    expect(routine.description).toContain("File zero to three proposals");
  });

  it("gives the critic the review role and keeps proposals behind it", () => {
    expect(market.instructions).toContain("multica proposal create --squad");
    expect(market.instructions).toContain("after the critic has reviewed them");
    expect(market.instructions).toContain("Do not write product code");
  });

  it("keeps the SDLC templates without a routine or a floor name", () => {
    for (const t of SQUAD_TEMPLATES.filter((x) => x.id !== "market_research")) {
      expect(t.routine, t.id).toBeNull();
      expect(t.floorName, t.id).toBeNull();
      expect(t.leader, t.id).toBe("lead");
    }
  });
});

describe("squad instructions", () => {
  it("delegate through sub-issues, not a mention that would start a member twice", () => {
    for (const t of SQUAD_TEMPLATES) {
      expect(t.instructions, t.id).toContain("--parent <request>");
      expect(t.instructions, t.id).toContain("Do not @mention a member");
    }
  });

  it("make the leader verify a report before closing a sub-issue", () => {
    for (const t of SQUAD_TEMPLATES) {
      expect(t.instructions, t.id).toContain("A report is a claim until the evidence supports it");
      expect(t.instructions, t.id).toContain("Leave done to a person");
    }
  });

  it("keep the leader from building, and route a build skill request to the developer", () => {
    for (const t of SQUAD_TEMPLATES) {
      expect(t.instructions, t.id).toContain("You never build");
      expect(t.instructions, t.id).toContain("do not run it yourself");
      expect(t.instructions, t.id).toContain('"/bmad-build-auto Implement epic 01"');
      expect(t.instructions, t.id).toContain('building an epic or story goes to "SDLC Developer"');
    }
  });

  it("say that the squad's sub-issue rule overrides the protocol's mention rule", () => {
    for (const t of SQUAD_TEMPLATES) {
      expect(t.instructions, t.id).toContain("They override the squad protocol's mention rule");
    }
  });

  it("never let the squad deploy to production or delete on its own", () => {
    for (const t of SQUAD_TEMPLATES) {
      expect(t.instructions, t.id).toContain(
        "Never deploy to production, and never delete anything, on your own",
      );
    }
  });

  it("queue the greenfield epics as numbered issues in the build project", () => {
    const greenfield = template("sdlc_greenfield").instructions;

    expect(greenfield).toContain("multica issue queue-order <issue> --number <N>");
    expect(greenfield).toContain("(an issue, not a sub-issue)");
    expect(greenfield).toContain("workflow_id is set");
    expect(greenfield).toContain("holds the queue");
  });

  it("name the BMAD planning skills in order for a greenfield", () => {
    const greenfield = template("sdlc_greenfield").instructions;
    const order = [
      "bmad-product-brief",
      "bmad-prd",
      "bmad-ux",
      "bmad-architecture",
      "bmad-project-context",
      "bmad-create-epics-and-stories",
      "bmad-sprint-planning",
    ].map((skill) => greenfield.indexOf(skill));

    expect(order.every((i) => i >= 0)).toBe(true);
    expect([...order].sort((a, b) => a - b)).toEqual(order);
  });

  it("route a maintenance request by type and send big changes through course correction", () => {
    const maintenance = template("sdlc_maintenance").instructions;

    for (const route of ["A. Hotfix", "B. A bug", "C. Feature", "D. Optimization", "E. Upgrade"]) {
      expect(maintenance).toContain(route);
    }
    expect(maintenance).toContain("bmad-correct-course");
  });

  it("only name BMAD skills that exist", () => {
    const shipped = new Set([
      "bmad-architecture",
      "bmad-build",
      "bmad-build-auto",
      "bmad-correct-course",
      "bmad-create-epics-and-stories",
      "bmad-prd",
      "bmad-prfaq",
      "bmad-product-brief",
      "bmad-project-context",
      "bmad-sprint-planning",
      "bmad-ux",
    ]);

    for (const t of SQUAD_TEMPLATES) {
      for (const skill of t.instructions.match(/\bbmad-[a-z0-9]+(?:-[a-z0-9]+)*/g) ?? []) {
        expect(shipped.has(skill), `${t.id}: ${skill}`).toBe(true);
      }
    }
  });
});
