// @vitest-environment node
import { describe, expect, it } from "vitest";
import { AGENT_INSTRUCTION_TEMPLATES } from "../agents/instruction-templates";
import {
  COMPANY_FLOOR_KEYS,
  COMPANY_FLOOR_NAMES,
  COMPANY_SQUAD_CARDS,
  COMPANY_SQUAD_KEYS,
  COMPANY_STAGE_KEYS,
  COMPANY_STAGE_OWNERS,
  CompanySquadCardSchema,
  DEFAULT_RUN_COST_USD,
  companySquadCard,
  estimateCompanyRuns,
  routinesStartedBy,
  unownedStages,
  type CompanySquadKey,
} from "./company-template";
import { SQUAD_ROLES } from "./templates";

const ALL = COMPANY_SQUAD_KEYS as readonly CompanySquadKey[];

describe("squad cards", () => {
  it("validates every card against the card schema", () => {
    for (const card of COMPANY_SQUAD_CARDS) {
      const parsed = CompanySquadCardSchema.safeParse(card);
      expect(parsed.success, `${card.key}: ${parsed.success ? "" : parsed.error.message}`).toBe(true);
    }
  });

  it("has exactly one card for each of the thirteen squads in spec 3.4, and one plan 10.18d addition", () => {
    const keys = COMPANY_SQUAD_CARDS.map((card) => card.key);

    expect(keys).toHaveLength(14);
    expect(new Set(keys).size).toBe(keys.length);
    expect([...keys].sort()).toEqual([...COMPANY_SQUAD_KEYS].sort());
    // The spec 3.4 table, floor by floor, in its order. People & Performance follows it.
    expect(keys.slice(0, 13)).toEqual([
      "executive_office",
      "market_research",
      "opportunity_lab",
      "competitor_intelligence",
      "customer_voice",
      "product_discovery",
      "product_analysis",
      "planning",
      "build",
      "release_ops",
      "growth_gtm",
      "support",
      "finance_risk",
    ]);
    expect(keys[13]).toBe("people_performance");
  });

  it("validates the People & Performance card with its phase 4 stubs", () => {
    const card = companySquadCard("people_performance");

    expect(CompanySquadCardSchema.safeParse(card).success).toBe(true);
    expect(card.floor).toBe("governance");
    expect(card.routines).toEqual([]);
    expect(card.governance).toEqual({ autonomyLevel: 1, budgetSharePct: 0, goalsScaffold: [] });
  });

  it("puts every card on a floor that has a name", () => {
    for (const card of COMPANY_SQUAD_CARDS) {
      expect(COMPANY_FLOOR_KEYS, card.key).toContain(card.floor);
      expect(COMPANY_FLOOR_NAMES[card.floor], card.key).toBeTruthy();
    }
  });

  it("gives each card a leader that is one of its roles", () => {
    for (const card of COMPANY_SQUAD_CARDS) {
      expect(card.roles.map((role) => role.key), card.key).toContain(card.leader);
    }
  });

  it("refuses a card whose leader is not a role", () => {
    const card = companySquadCard("planning");
    const bad = { ...card, leader: "nobody" };

    expect(CompanySquadCardSchema.safeParse(bad).success).toBe(false);
  });

  it("refuses a card with a role that names an unknown instruction template", () => {
    const card = companySquadCard("planning");
    const bad = {
      ...card,
      roles: card.roles.map((role, i) =>
        i === 0 ? { ...role, agentTemplate: "no_such_template" } : role,
      ),
    };

    expect(CompanySquadCardSchema.safeParse(bad).success).toBe(false);
  });

  it("gives every role an instruction template that exists", () => {
    const ids = new Set(AGENT_INSTRUCTION_TEMPLATES.map((t) => t.id));
    for (const card of COMPANY_SQUAD_CARDS) {
      for (const role of card.roles) {
        expect(ids.has(role.agentTemplate), `${card.key}/${role.key}`).toBe(true);
      }
    }
  });

  it("names the shared squads' agents as the squad quickstart does, so reruns reuse them", () => {
    const quickstartNames = new Map(
      Object.values(SQUAD_ROLES).map((role) => [role.name, role.agentTemplate]),
    );
    for (const card of COMPANY_SQUAD_CARDS) {
      for (const role of card.roles) {
        const template = quickstartNames.get(role.name);
        if (template) {
          expect(role.agentTemplate, `${card.key}: ${role.name}`).toBe(template);
        }
      }
    }
  });

  it("keeps the phase 4 fields at their phase 3 stubs", () => {
    for (const card of COMPANY_SQUAD_CARDS) {
      expect(card.governance, card.key).toEqual({
        autonomyLevel: 1,
        budgetSharePct: 0,
        goalsScaffold: [],
      });
    }
  });

  it("gives the Secretary the existing Secretary runtime and no other role", () => {
    const secretaries = COMPANY_SQUAD_CARDS.flatMap((card) =>
      card.roles.filter((role) => role.runtime === "secretary").map((role) => `${card.key}/${role.key}`),
    );

    expect(secretaries).toEqual(["executive_office/secretary"]);
  });
});

describe("pipeline stages", () => {
  it("names one owner for each stage, and that owner is a card", () => {
    expect(COMPANY_STAGE_OWNERS.map((s) => s.key)).toEqual([...COMPANY_STAGE_KEYS]);
    for (const stage of COMPANY_STAGE_OWNERS) {
      expect(COMPANY_SQUAD_KEYS, stage.key).toContain(stage.owner);
      expect(COMPANY_SQUAD_CARDS.some((c) => c.key === stage.owner), stage.key).toBe(true);
    }
  });

  it("lists each stage in its owner's card", () => {
    for (const stage of COMPANY_STAGE_OWNERS) {
      expect(companySquadCard(stage.owner).stages, stage.key).toContain(stage.key);
    }
  });

  it("gives each stage owner a leader with an instruction template", () => {
    const ids = new Set(AGENT_INSTRUCTION_TEMPLATES.map((t) => t.id));
    for (const stage of COMPANY_STAGE_OWNERS) {
      const card = companySquadCard(stage.owner);
      const leader = card.roles.find((role) => role.key === card.leader);
      expect(leader && ids.has(leader.agentTemplate), stage.key).toBe(true);
    }
  });

  it("keeps a card's stage list to stages whose owner it is or co-owns", () => {
    for (const card of COMPANY_SQUAD_CARDS) {
      for (const stage of card.stages) {
        expect(COMPANY_STAGE_KEYS, `${card.key}: ${stage}`).toContain(stage);
      }
    }
    expect(companySquadCard("growth_gtm").stages).toEqual(["launch", "operate"]);
    expect(companySquadCard("build").stages).toEqual(["build", "learn"]);
  });

  it("names every unowned stage when a squad is left out", () => {
    expect(unownedStages(ALL)).toEqual([]);
    expect(unownedStages([]).map((s) => s.key)).toEqual([...COMPANY_STAGE_KEYS]);
    expect(unownedStages(["product_discovery"]).map((s) => s.key)).toEqual([
      "define",
      "plan",
      "build",
      "launch",
      "operate",
      "learn",
    ]);
  });
});

describe("routines", () => {
  it("creates paused every routine except the Secretary's brief and the market scan", () => {
    const active = COMPANY_SQUAD_CARDS.flatMap((card) =>
      card.routines.filter((r) => r.activeAtCreate).map((r) => `${card.key}: ${r.title}`),
    );

    expect(active.sort()).toEqual(["executive_office: Daily brief", "market_research: Market scan"]);
  });

  it("gives every routine a cron the wizard can send", () => {
    for (const card of COMPANY_SQUAD_CARDS) {
      for (const routine of card.routines) {
        expect(routine.cron.split(" "), `${card.key}: ${routine.title}`).toHaveLength(5);
      }
    }
  });

  it("starts nothing for the squads that have no routine", () => {
    expect(routinesStartedBy(["build", "finance_risk"], "all")).toEqual([]);
  });

  it("leaves paused everything but the brief and the scan under leave_paused", () => {
    const started = routinesStartedBy(ALL, "leave_paused").map((s) => s.routine.title);

    expect(started.sort()).toEqual(["Daily brief", "Market scan"]);
  });

  it("starts each floor's lead routine under per_floor", () => {
    const started = routinesStartedBy(ALL, "per_floor").map((s) => s.routine.title);

    expect(started).toContain("Librarian daily triage");
    expect(started).toContain("Opportunity generation");
    expect(started).toContain("Operating review");
    expect(started).toContain("Release checklist audit");
    expect(started).not.toContain("Librarian weekly freshness review");
    expect(started).not.toContain("Daily triage");
  });

  it("starts every routine under all", () => {
    const total = COMPANY_SQUAD_CARDS.reduce((n, card) => n + card.routines.length, 0);

    expect(routinesStartedBy(ALL, "all")).toHaveLength(total);
  });
});

describe("estimateCompanyRuns", () => {
  it("counts runs per week from each routine's cadence", () => {
    // Daily brief (7) and Market scan (1) under leave_paused.
    expect(estimateCompanyRuns(["executive_office", "market_research"], "leave_paused").runsPerWeek).toBe(8);
  });

  it("prices runs at the stated default until a measured average exists", () => {
    const estimate = estimateCompanyRuns(["market_research"], "leave_paused");

    expect(estimate.costIsDefault).toBe(true);
    expect(estimate.costPerWeekUsd).toBeCloseTo(DEFAULT_RUN_COST_USD, 10);
  });

  it("prices runs at the measured average when one is given", () => {
    const estimate = estimateCompanyRuns(["market_research"], "leave_paused", 0.25);

    expect(estimate.costIsDefault).toBe(false);
    expect(estimate.costPerWeekUsd).toBeCloseTo(0.25, 10);
  });

  it("adds nothing when no squad is selected", () => {
    expect(estimateCompanyRuns([], "all")).toEqual({
      runsPerWeek: 0,
      costPerWeekUsd: 0,
      costIsDefault: true,
    });
  });

  it("grows from leave_paused to per_floor to all", () => {
    const at = (start: "leave_paused" | "per_floor" | "all") =>
      estimateCompanyRuns(ALL, start).runsPerWeek;

    expect(at("leave_paused")).toBeLessThan(at("per_floor"));
    expect(at("per_floor")).toBeLessThan(at("all"));
  });
});
