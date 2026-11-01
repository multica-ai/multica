import { describe, expect, it } from "vitest";
import {
  FALLBACK_OFFICE_SETUP_READINESS,
  isOfficeSetupReady,
  OfficeSetupReadinessSchema,
  parseOfficeSetupRefusal,
  type OfficeSetupReadiness,
} from "./setup-schema";

const complete: OfficeSetupReadiness = {
  runtime_online: true,
  secretary_id: "agent-1",
  executive_floor: true,
  autopilot_daily: true,
  autopilot_weekly_paused: true,
  ready: true,
};

describe("OfficeSetupReadinessSchema", () => {
  it("reads a complete checklist as it is", () => {
    expect(OfficeSetupReadinessSchema.parse(complete)).toEqual(complete);
  });

  it("defaults every missing check to not set up", () => {
    expect(OfficeSetupReadinessSchema.parse({ runtime_online: true })).toEqual({
      runtime_online: true,
      secretary_id: null,
      executive_floor: false,
      autopilot_daily: false,
      autopilot_weekly_paused: false,
      ready: false,
    });
  });

  it("reads an empty secretary id as no secretary", () => {
    expect(OfficeSetupReadinessSchema.parse({ secretary_id: "" }).secretary_id).toBeNull();
  });

  it("keeps fields a newer server adds", () => {
    expect(OfficeSetupReadinessSchema.parse({ ...complete, weekly_summary: "x" })).toMatchObject({
      weekly_summary: "x",
    });
  });

  it("rejects a malformed check rather than reading it as true", () => {
    expect(OfficeSetupReadinessSchema.safeParse({ runtime_online: "yes" }).success).toBe(false);
    expect(OfficeSetupReadinessSchema.safeParse({ executive_floor: 1 }).success).toBe(false);
    expect(OfficeSetupReadinessSchema.safeParse({ secretary_id: 42 }).success).toBe(false);
    expect(OfficeSetupReadinessSchema.safeParse(null).success).toBe(false);
    expect(OfficeSetupReadinessSchema.safeParse([]).success).toBe(false);
  });

  it("falls back to nothing set up, so an unreadable checklist asks for setup", () => {
    expect(FALLBACK_OFFICE_SETUP_READINESS).toEqual({
      runtime_online: false,
      secretary_id: null,
      executive_floor: false,
      autopilot_daily: false,
      autopilot_weekly_paused: false,
      ready: false,
    });
  });
});

describe("isOfficeSetupReady", () => {
  it("is true only when every check holds", () => {
    expect(isOfficeSetupReady(complete)).toBe(true);
  });

  it("does not trust the server's ready flag over a missing check", () => {
    for (const key of [
      "runtime_online",
      "executive_floor",
      "autopilot_daily",
      "autopilot_weekly_paused",
    ] as const) {
      expect(isOfficeSetupReady({ ...complete, [key]: false }), key).toBe(false);
    }
    expect(isOfficeSetupReady({ ...complete, secretary_id: null })).toBe(false);
  });
});

describe("parseOfficeSetupRefusal", () => {
  it("reads the runtime_required refusal and its install path", () => {
    expect(
      parseOfficeSetupRefusal({
        error: "an online Hermes runtime is required",
        code: "runtime_required",
        hint: "Install Hermes.",
        docs_path: "/install-agent-runtime",
      }),
    ).toMatchObject({
      code: "runtime_required",
      hint: "Install Hermes.",
      docs_path: "/install-agent-runtime",
    });
  });

  it("supplies the install path when an older server omits it", () => {
    expect(parseOfficeSetupRefusal({ code: "runtime_required" })).toEqual({
      code: "runtime_required",
      hint: "",
      docs_path: "/install-agent-runtime",
    });
  });

  it("returns null for other bodies", () => {
    expect(parseOfficeSetupRefusal({ error: "forbidden" })).toBeNull();
    expect(parseOfficeSetupRefusal({ code: 7 })).toBeNull();
    expect(parseOfficeSetupRefusal("runtime_required")).toBeNull();
    expect(parseOfficeSetupRefusal(undefined)).toBeNull();
  });
});
