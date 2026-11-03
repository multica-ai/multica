// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_INITIATIVES,
  EMPTY_PIPELINES,
  FALLBACK_INITIATIVE,
  FALLBACK_INITIATIVE_DETAIL,
  InitiativeDetailSchema,
  InitiativeSchema,
  InitiativesResponseSchema,
  PipelinesResponseSchema,
} from "./schemas";

const stage = {
  key: "validate",
  name: "Validate",
  owner_squad_id: "sq-research",
  objective: "Decide with evidence.",
  instructions: "Read the thesis first.",
  artifacts: [
    { kind: "validation_report", required: true },
    { kind: "customer_insight", required: false },
  ],
  exit: { mode: "gate", criteria: "A go / no-go recommendation." },
  sla_days: 14,
};

const pipeline = { id: "pl-1", name: "Default", is_default: true, stages: [stage] };

const initiative = {
  id: "in-1",
  identifier: "INI-1",
  title: "Invoicing for small firms",
  thesis: "Slow month-end close",
  pipeline_id: "pl-1",
  stage_key: "validate",
  owner_squad_id: "sq-research",
  status: "active",
  issue_id: "iss-1",
  build_project_id: null,
  stage_entered_at: "2026-10-20T00:00:00Z",
  days_in_stage: 13,
  sla_state: "stalled",
  artifacts_required: 3,
  artifacts_present: 1,
  pending_gate_proposal_id: null,
};

describe("PipelinesResponseSchema", () => {
  it("unwraps the pipelines and their stages", () => {
    const list = parseWithFallback([], PipelinesResponseSchema, EMPTY_PIPELINES, { endpoint: "t" });
    expect(list).toEqual([]);
    const parsed = parseWithFallback({ pipelines: [pipeline] }, PipelinesResponseSchema, EMPTY_PIPELINES, {
      endpoint: "t",
    });
    expect(parsed).toHaveLength(1);
    expect(parsed[0]).toMatchObject({ id: "pl-1", is_default: true });
    expect(parsed[0]?.stages[0]).toMatchObject({
      key: "validate",
      owner_squad_id: "sq-research",
      exit: { mode: "gate", criteria: "A go / no-go recommendation." },
      sla_days: 14,
    });
  });

  it("degrades an unknown exit mode to unknown and keeps the stage", () => {
    const parsed = parseWithFallback(
      { pipelines: [{ ...pipeline, stages: [{ ...stage, exit: { mode: "vote", criteria: "" } }] }] },
      PipelinesResponseSchema,
      EMPTY_PIPELINES,
      { endpoint: "t" },
    );
    expect(parsed[0]?.stages[0]?.exit.mode).toBe("unknown");
  });

  it("fills defaults for fields an older server omits", () => {
    const parsed = parseWithFallback(
      { pipelines: [{ id: "pl-2", stages: [{ key: "build" }] }] },
      PipelinesResponseSchema,
      EMPTY_PIPELINES,
      { endpoint: "t" },
    );
    expect(parsed[0]).toMatchObject({ id: "pl-2", name: "", is_default: false });
    expect(parsed[0]?.stages[0]).toMatchObject({
      owner_squad_id: null,
      artifacts: [],
      exit: { mode: "auto", criteria: "" },
      sla_days: null,
    });
  });

  it("returns the empty list for malformed responses", () => {
    for (const bad of [null, "oops", 42, [pipeline], { pipelines: "none" }, { pipelines: [{ id: 5 }] }]) {
      expect(parseWithFallback(bad, PipelinesResponseSchema, EMPTY_PIPELINES, { endpoint: "t" })).toBe(
        EMPTY_PIPELINES,
      );
    }
  });
});

describe("InitiativesResponseSchema", () => {
  it("reads the board fields", () => {
    const [parsed] = parseWithFallback({ initiatives: [initiative] }, InitiativesResponseSchema, EMPTY_INITIATIVES, {
      endpoint: "t",
    });
    expect(parsed).toMatchObject({
      identifier: "INI-1",
      stage_key: "validate",
      days_in_stage: 13,
      sla_state: "stalled",
      artifacts_required: 3,
      artifacts_present: 1,
      status: "active",
      pending_gate_proposal_id: null,
    });
  });

  it("degrades an unknown status to unknown and an unknown sla state to ok", () => {
    const [parsed] = parseWithFallback(
      { initiatives: [{ ...initiative, status: "archived", sla_state: "critical" }] },
      InitiativesResponseSchema,
      EMPTY_INITIATIVES,
      { endpoint: "t" },
    );
    expect(parsed?.status).toBe("unknown");
    expect(parsed?.sla_state).toBe("ok");
  });

  it("returns the empty list for malformed responses", () => {
    for (const bad of [null, [initiative], { initiatives: null }, { initiatives: [{ title: "no id" }] }, "x"]) {
      expect(parseWithFallback(bad, InitiativesResponseSchema, EMPTY_INITIATIVES, { endpoint: "t" })).toBe(
        EMPTY_INITIATIVES,
      );
    }
  });
});

describe("InitiativeDetailSchema", () => {
  const detail = {
    initiative,
    artifacts: [
      { kind: "validation_report", stage_key: "validate", required: true, knowledge_link_id: "kl-1", waived: false, waived_note: "" },
      { kind: "competitor_profile", stage_key: "validate", required: true, knowledge_link_id: null, waived: true, waived_note: "Covered by the market report" },
    ],
    events: [
      { id: "ev-1", kind: "created", from_stage: null, to_stage: "validate", actor_type: "member", actor_id: "u-1", note: "", created_at: "2026-10-20T00:00:00Z" },
      { id: "ev-2", kind: "attested", from_stage: "validate", to_stage: null, actor_type: "agent", actor_id: "a-1", note: "Ready", created_at: "2026-10-30T00:00:00Z" },
    ],
    gates: [{ id: "p-9", state: "pending", title: "Commit to a product?", issue_identifier: "MUL-42", created_at: "2026-10-31T00:00:00Z" }],
  };

  it("reads the initiative, its artifacts, events and gates", () => {
    const parsed = parseWithFallback(detail, InitiativeDetailSchema, FALLBACK_INITIATIVE_DETAIL, { endpoint: "t" });
    expect(parsed.initiative.id).toBe("in-1");
    expect(parsed.artifacts[1]).toMatchObject({ waived: true, waived_note: "Covered by the market report", knowledge_link_id: null });
    expect(parsed.events.map((event) => event.kind)).toEqual(["created", "attested"]);
    expect(parsed.gates[0]).toMatchObject({ state: "pending", issue_identifier: "MUL-42" });
  });

  it("reads an operate review's day, and reads a stage gate as no day", () => {
    const parsed = parseWithFallback(
      { ...detail, gates: [{ id: "p-30", state: "pending", gate_day: 30 }, { id: "p-9", state: "pending" }] },
      InitiativeDetailSchema,
      FALLBACK_INITIATIVE_DETAIL,
      { endpoint: "t" },
    );
    expect(parsed.gates.map((gate) => gate.gate_day)).toEqual([30, null]);
  });

  it("degrades an unknown gate state to unknown", () => {
    const parsed = parseWithFallback(
      { ...detail, gates: [{ id: "p-9", state: "withdrawn" }] },
      InitiativeDetailSchema,
      FALLBACK_INITIATIVE_DETAIL,
      { endpoint: "t" },
    );
    expect(parsed.gates[0]?.state).toBe("unknown");
  });

  it("defaults missing lists to empty", () => {
    const parsed = parseWithFallback({ initiative }, InitiativeDetailSchema, FALLBACK_INITIATIVE_DETAIL, { endpoint: "t" });
    expect(parsed).toMatchObject({ artifacts: [], events: [], gates: [] });
  });

  it("returns the fallback for a malformed detail", () => {
    for (const bad of [null, "x", [], { initiative: "in-1" }, { initiative: { title: "no id" } }, { ...detail, events: "none" }]) {
      expect(parseWithFallback(bad, InitiativeDetailSchema, FALLBACK_INITIATIVE_DETAIL, { endpoint: "t" })).toBe(
        FALLBACK_INITIATIVE_DETAIL,
      );
    }
  });
});

// A transition (advance, shelve, close, and the others) and a create answer with
// the initiative itself, read with the same schema as one row of the board.
describe("InitiativeSchema as a transition response", () => {
  it("reads the initiative a transition returns", () => {
    expect(parseWithFallback(initiative, InitiativeSchema, FALLBACK_INITIATIVE, { endpoint: "t" })).toMatchObject({
      id: "in-1",
      identifier: "INI-1",
      stage_key: "validate",
      status: "active",
    });
  });

  it("degrades an unknown status from a newer server to unknown", () => {
    expect(
      parseWithFallback({ ...initiative, status: "paused" }, InitiativeSchema, FALLBACK_INITIATIVE, { endpoint: "t" })
        .status,
    ).toBe("unknown");
  });

  it("returns the id-less fallback for a malformed response", () => {
    for (const bad of [null, "in-1", [initiative], {}, { id: 5 }, { initiative }, { title: "no id" }]) {
      expect(parseWithFallback(bad, InitiativeSchema, FALLBACK_INITIATIVE, { endpoint: "t" })).toBe(FALLBACK_INITIATIVE);
    }
  });
});
