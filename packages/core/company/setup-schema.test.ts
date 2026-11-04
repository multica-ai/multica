import { describe, expect, it } from "vitest";
import {
  FALLBACK_COMPANY_SETUP_READINESS,
  isCompanySetupReady,
  CompanySetupReadinessSchema,
  parseCompanySetupRefusal,
  type CompanySetupReadiness,
} from "./setup-schema";

const complete: CompanySetupReadiness = {
  runtime_online: true,
  secretary_id: "agent-1",
  secretary_desk_project_id: "project-1",
  executive_floor: true,
  autopilot_daily: true,
  autopilot_weekly_paused: true,
  ready: true,
};

describe("CompanySetupReadinessSchema", () => {
  it("reads a complete checklist as it is", () => {
    expect(CompanySetupReadinessSchema.parse(complete)).toEqual(complete);
  });

  it("defaults every missing check to not set up", () => {
    expect(CompanySetupReadinessSchema.parse({ runtime_online: true })).toEqual({
      runtime_online: true,
      secretary_id: null,
      secretary_desk_project_id: null,
      executive_floor: false,
      autopilot_daily: false,
      autopilot_weekly_paused: false,
      ready: false,
    });
  });

  it("reads the Secretary desk id, and an empty one as not set up", () => {
    expect(CompanySetupReadinessSchema.parse(complete).secretary_desk_project_id).toBe("project-1");
    expect(
      CompanySetupReadinessSchema.parse({ secretary_desk_project_id: "" }).secretary_desk_project_id,
    ).toBeNull();
  });

  it("reads an empty secretary id as no secretary", () => {
    expect(CompanySetupReadinessSchema.parse({ secretary_id: "" }).secretary_id).toBeNull();
  });

  it("keeps fields a newer server adds", () => {
    expect(CompanySetupReadinessSchema.parse({ ...complete, weekly_summary: "x" })).toMatchObject({
      weekly_summary: "x",
    });
  });

  it("rejects a malformed check rather than reading it as true", () => {
    expect(CompanySetupReadinessSchema.safeParse({ runtime_online: "yes" }).success).toBe(false);
    expect(CompanySetupReadinessSchema.safeParse({ executive_floor: 1 }).success).toBe(false);
    expect(CompanySetupReadinessSchema.safeParse({ secretary_id: 42 }).success).toBe(false);
    expect(CompanySetupReadinessSchema.safeParse(null).success).toBe(false);
    expect(CompanySetupReadinessSchema.safeParse([]).success).toBe(false);
  });

  it("falls back to nothing set up, so an unreadable checklist asks for setup", () => {
    expect(FALLBACK_COMPANY_SETUP_READINESS).toEqual({
      runtime_online: false,
      secretary_id: null,
      secretary_desk_project_id: null,
      executive_floor: false,
      autopilot_daily: false,
      autopilot_weekly_paused: false,
      ready: false,
    });
  });
});

describe("isCompanySetupReady", () => {
  it("is true only when every check holds", () => {
    expect(isCompanySetupReady(complete)).toBe(true);
  });

  it("does not trust the server's ready flag over a missing check", () => {
    for (const key of [
      "runtime_online",
      "executive_floor",
      "autopilot_daily",
      "autopilot_weekly_paused",
    ] as const) {
      expect(isCompanySetupReady({ ...complete, [key]: false }), key).toBe(false);
    }
    expect(isCompanySetupReady({ ...complete, secretary_id: null })).toBe(false);
  });
});

describe("parseCompanySetupRefusal", () => {
  it("reads the runtime_required refusal and its install path", () => {
    expect(
      parseCompanySetupRefusal({
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
    expect(parseCompanySetupRefusal({ code: "runtime_required" })).toEqual({
      code: "runtime_required",
      hint: "",
      docs_path: "/install-agent-runtime",
    });
  });

  it("returns null for other bodies", () => {
    expect(parseCompanySetupRefusal({ error: "forbidden" })).toBeNull();
    expect(parseCompanySetupRefusal({ code: 7 })).toBeNull();
    expect(parseCompanySetupRefusal("runtime_required")).toBeNull();
    expect(parseCompanySetupRefusal(undefined)).toBeNull();
  });
});
