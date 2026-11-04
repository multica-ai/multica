// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  CompanyStateSchema,
  EMPTY_GOVERNANCE_EVENT_PAGE,
  FALLBACK_COMPANY_STATE,
  GovernanceEventPageSchema,
} from "./schemas";

const event = {
  id: "ev-1",
  kind: "budget_hold",
  actor_type: "system",
  actor_id: null,
  subject_type: "budget",
  subject_id: "bud-1",
  summary: "Held one routine run",
  details: { task_id: "t-1" },
  created_at: "2026-11-02T10:00:00Z",
};

describe("CompanyStateSchema", () => {
  it("reads a full company state", () => {
    const parsed = CompanyStateSchema.parse({
      state: "paused",
      quiet_hours: { enabled: true, start: "22:00", end: "07:00", timezone: "Europe/London" },
      updated_at: "2026-11-02T10:00:00Z",
    });
    expect(parsed).toMatchObject({
      state: "paused",
      quiet_hours: { enabled: true, start: "22:00", end: "07:00", timezone: "Europe/London" },
    });
  });

  it("reads the boss's setting beside the state in force", () => {
    const parsed = CompanyStateSchema.parse({ state: "quiet", setting: "running" });
    expect(parsed).toMatchObject({ state: "quiet", setting: "running" });
  });

  it("takes the setting from the state when the server omits it", () => {
    expect(CompanyStateSchema.parse({ state: "paused" }).setting).toBe("paused");
  });

  it("maps an unknown setting to unknown", () => {
    expect(CompanyStateSchema.parse({ state: "running", setting: "draining" }).setting).toBe("unknown");
  });

  it("defaults missing quiet hours and timestamps", () => {
    const parsed = CompanyStateSchema.parse({ state: "running" });
    expect(parsed.quiet_hours).toEqual({ enabled: false, start: "22:00", end: "07:00", timezone: "UTC" });
    expect(parsed.updated_at).toBe("");
  });

  it("fills in defaults for a partly filled quiet-hours object", () => {
    const parsed = CompanyStateSchema.parse({ state: "quiet", quiet_hours: { enabled: true } });
    expect(parsed.quiet_hours).toEqual({ enabled: true, start: "22:00", end: "07:00", timezone: "UTC" });
  });

  it("maps a state this client does not know to unknown", () => {
    expect(CompanyStateSchema.parse({ state: "draining" }).state).toBe("unknown");
  });

  it("treats a missing state as unknown rather than running", () => {
    expect(CompanyStateSchema.parse({}).state).toBe("unknown");
  });

  it("rejects a malformed body so the caller falls back", () => {
    expect(CompanyStateSchema.safeParse("paused").success).toBe(false);
    expect(CompanyStateSchema.safeParse(null).success).toBe(false);
    expect(CompanyStateSchema.safeParse({ state: "running", quiet_hours: "nope" }).success).toBe(false);
  });

  it("names unknown state as the fallback", () => {
    expect(FALLBACK_COMPANY_STATE.state).toBe("unknown");
  });
});

describe("GovernanceEventPageSchema", () => {
  it("reads a page with its cursor", () => {
    const page = GovernanceEventPageSchema.parse({ events: [event], next_cursor: "c-2" });
    expect(page.next_cursor).toBe("c-2");
    expect(page.events).toHaveLength(1);
    expect(page.events[0]).toMatchObject({
      id: "ev-1",
      kind: "budget_hold",
      actor_type: "system",
      actor_id: null,
      subject_id: "bud-1",
      details: { task_id: "t-1" },
    });
  });

  it("ends the log when next_cursor is missing or empty", () => {
    expect(GovernanceEventPageSchema.parse({ events: [] }).next_cursor).toBeNull();
    expect(GovernanceEventPageSchema.parse({ events: [], next_cursor: "" }).next_cursor).toBeNull();
  });

  it("maps an event kind this client does not know to unknown and keeps the summary", () => {
    const page = GovernanceEventPageSchema.parse({
      events: [{ ...event, kind: "floor_rebalanced", summary: "Moved a squad" }],
      next_cursor: null,
    });
    expect(page.events[0]).toMatchObject({ kind: "unknown", summary: "Moved a squad" });
  });

  it("maps an unknown or missing actor type to unknown", () => {
    const page = GovernanceEventPageSchema.parse({
      events: [
        { ...event, actor_type: "robot" },
        { ...event, id: "ev-2", actor_type: undefined },
      ],
      next_cursor: null,
    });
    expect(page.events.map((item) => item.actor_type)).toEqual(["unknown", "unknown"]);
  });

  it("defaults optional fields on a sparse event", () => {
    const page = GovernanceEventPageSchema.parse({ events: [{ id: "ev-3" }], next_cursor: null });
    expect(page.events[0]).toEqual({
      id: "ev-3",
      kind: "unknown",
      actor_type: "unknown",
      actor_id: null,
      subject_type: "",
      subject_id: null,
      summary: "",
      details: {},
      created_at: "",
    });
  });

  it("replaces non-object details with an empty object", () => {
    const page = GovernanceEventPageSchema.parse({
      events: [{ ...event, details: "not-an-object" }],
      next_cursor: null,
    });
    expect(page.events[0]?.details).toEqual({});
  });

  it("rejects an event without an id, so the page falls back to empty", () => {
    expect(GovernanceEventPageSchema.safeParse({ events: [{ kind: "budget_hold" }], next_cursor: null }).success).toBe(false);
    expect(GovernanceEventPageSchema.safeParse({ events: "none" }).success).toBe(false);
    expect(GovernanceEventPageSchema.safeParse(undefined).success).toBe(false);
  });

  it("has an empty fallback page", () => {
    expect(EMPTY_GOVERNANCE_EVENT_PAGE).toEqual({ events: [], next_cursor: null });
  });
});
