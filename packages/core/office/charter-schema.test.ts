// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  EMPTY_SQUAD_ROUTINE,
  SquadCharterSchema,
  SquadRoutineListSchema,
  SquadRoutineSchema,
  emptySquadCharter,
  parseCharterRevisionConflict,
} from "./charter-schema";
import { parseWithFallback } from "../api/schema";
import { charterKeys } from "./charter-queries";

const charter = {
  squad_id: "s1",
  mission: "Find the market gaps",
  responsibilities: [{ key: "r1", text: "Track competitors" }],
  outputs: [{ key: "o1", kind: "kb_document", kb_key: "market", cadence_hint: "weekly" }],
  boundaries: "No pricing changes",
  escalation: "Ask the boss about spend",
  intake: "Other squads may ask for data",
  response_hours: 24,
  operating_review_enabled: false,
  revision: 3,
  updated_by_type: "member",
  updated_by_id: "u1",
  updated_at: "2026-11-01T00:00:00Z",
};

const routine = {
  autopilot_id: "ap1",
  squad_id: "s1",
  autopilot_title: "Weekly scan",
  autopilot_status: "active",
  purpose: "Weekly market scan",
  expected_output: "report",
  required: true,
  grace_hours: 24,
  state: "late",
  schedule: { cron_expression: "0 9 * * 1", timezone: "Europe/Paris" },
  next_run_at: "2026-11-09T08:00:00Z",
  last_run: { id: "r1", status: "completed", source: "schedule", triggered_at: "t", completed_at: "t" },
  created_at: "2026-10-01T00:00:00Z",
};

const fallback = emptySquadCharter("s1");

describe("SquadCharterSchema", () => {
  it("parses a full server response", () => {
    const parsed = parseWithFallback(charter, SquadCharterSchema, fallback, { endpoint: "t" });
    expect(parsed).toMatchObject({ mission: "Find the market gaps", revision: 3, response_hours: 24 });
    expect(parsed.responsibilities).toEqual([{ key: "r1", text: "Track competitors" }]);
    expect(parsed.outputs[0]).toMatchObject({ kind: "kb_document", kb_key: "market" });
  });

  it("defaults the fields an older server omits", () => {
    const parsed = SquadCharterSchema.parse({ squad_id: "s1" });
    expect(parsed).toMatchObject({
      mission: "",
      responsibilities: [],
      outputs: [],
      response_hours: 48,
      operating_review_enabled: true,
      revision: 0,
      updated_by_id: null,
      updated_at: null,
    });
  });

  it("reads a null list as empty", () => {
    expect(SquadCharterSchema.parse({ responsibilities: null, outputs: null }).responsibilities).toEqual([]);
  });

  it("keeps unknown fields from newer servers", () => {
    expect(SquadCharterSchema.parse({ ...charter, theme: "dark" })).toMatchObject({ theme: "dark" });
  });

  it("reads an output kind this client does not know as unknown", () => {
    const parsed = SquadCharterSchema.parse({
      ...charter,
      outputs: [{ key: "o1", kind: "decision", kb_key: "", cadence_hint: "" }],
    });
    expect(parsed.outputs[0]?.kind).toBe("unknown");
  });

  it("falls back to the empty charter for a malformed body", () => {
    expect(parseWithFallback({ ...charter, revision: "three" }, SquadCharterSchema, fallback, { endpoint: "t" })).toBe(fallback);
    expect(parseWithFallback({ ...charter, responsibilities: "nope" }, SquadCharterSchema, fallback, { endpoint: "t" })).toBe(fallback);
    expect(parseWithFallback({ ...charter, outputs: [{ key: 1 }] }, SquadCharterSchema, fallback, { endpoint: "t" })).toBe(
      fallback,
    );
    expect(parseWithFallback("garbage", SquadCharterSchema, fallback, { endpoint: "t" })).toBe(fallback);
  });

  it("gives an unwritten charter revision 0 and the default hours", () => {
    expect(emptySquadCharter("s9")).toMatchObject({ squad_id: "s9", revision: 0, response_hours: 48, operating_review_enabled: true });
  });
});

describe("parseCharterRevisionConflict", () => {
  it("returns the current revision from a conflict body", () => {
    expect(parseCharterRevisionConflict({ code: "charter_revision_conflict", revision: 5, error: "x" })).toBe(5);
  });

  it("returns null for other errors and malformed bodies", () => {
    expect(parseCharterRevisionConflict({ code: "routine_already_linked" })).toBeNull();
    expect(parseCharterRevisionConflict({ code: "charter_revision_conflict", revision: "5" })).toBeNull();
    expect(parseCharterRevisionConflict(null)).toBeNull();
  });
});

describe("SquadRoutineSchema", () => {
  it("parses a full routine", () => {
    const parsed = SquadRoutineSchema.parse(routine);
    expect(parsed).toMatchObject({
      autopilot_id: "ap1",
      state: "late",
      expected_output: "report",
      schedule: { cron_expression: "0 9 * * 1", timezone: "Europe/Paris" },
      last_run: { status: "completed", source: "schedule" },
    });
  });

  it("reads a never-run routine with no schedule, next run or last run", () => {
    const parsed = SquadRoutineSchema.parse({
      ...routine,
      state: "never_ran",
      schedule: null,
      next_run_at: null,
      last_run: null,
    });
    expect(parsed).toMatchObject({ state: "never_ran", schedule: null, next_run_at: null, last_run: null });
  });

  it("reads a state or expected output from a newer server as unknown", () => {
    expect(SquadRoutineSchema.parse({ ...routine, state: "held" }).state).toBe("unknown");
    expect(SquadRoutineSchema.parse({ ...routine, expected_output: "decision" }).expected_output).toBe("unknown");
  });

  it("defaults a schedule with no timezone to UTC", () => {
    expect(SquadRoutineSchema.parse({ ...routine, schedule: { cron_expression: "0 9 * * *" } }).schedule).toEqual({
      cron_expression: "0 9 * * *",
      timezone: "UTC",
    });
  });

  it("requires an autopilot id", () => {
    expect(SquadRoutineSchema.safeParse({ ...routine, autopilot_id: undefined }).success).toBe(false);
  });
});

describe("SquadRoutineListSchema", () => {
  it("parses a list and reads null as empty", () => {
    expect(SquadRoutineListSchema.parse([routine])).toHaveLength(1);
    expect(SquadRoutineListSchema.parse(null)).toEqual([]);
  });

  it("falls back to an empty list when one routine is malformed", () => {
    const parsed = parseWithFallback([routine, { purpose: "no id" }], SquadRoutineListSchema, [], { endpoint: "t" });
    expect(parsed).toEqual([]);
    expect(parseWithFallback({ routines: [] }, SquadRoutineListSchema, [], { endpoint: "t" })).toEqual([]);
  });

  it("degrades a routine response that cannot be read to the empty routine", () => {
    expect(parseWithFallback("nope", SquadRoutineSchema, EMPTY_SQUAD_ROUTINE, { endpoint: "t" })).toBe(EMPTY_SQUAD_ROUTINE);
  });
});

describe("charter query keys", () => {
  it("scope the charter and routines to the workspace and the squad", () => {
    expect(charterKeys.charter("ws-1", "s1")).toEqual(["workspaces", "ws-1", "squads", "s1", "charter"]);
    expect(charterKeys.routines("ws-1", "s1")).toEqual(["workspaces", "ws-1", "squads", "s1", "routines"]);
    expect(charterKeys.routines("ws-2", "s1")).not.toEqual(charterKeys.routines("ws-1", "s1"));
  });
});
