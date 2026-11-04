// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { buildCompanyTemplateApplyRequest } from "./company-template-mutations";
import {
  CompanyTemplateApplyResultSchema,
  FALLBACK_COMPANY_TEMPLATE_RESULT,
  isCompanyTemplateComplete,
} from "./company-template-schema";
import type { CompanyTemplateApplyResult } from "./company-template-types";

const ENDPOINT = { endpoint: "POST /api/company/company-template/apply" };

function read(body: unknown): CompanyTemplateApplyResult {
  return parseWithFallback(body, CompanyTemplateApplyResultSchema, FALLBACK_COMPANY_TEMPLATE_RESULT, ENDPOINT);
}

describe("CompanyTemplateApplyResultSchema", () => {
  it("reads a complete response", () => {
    const result = read({
      steps: [
        { key: "squad:market_research", status: "created", message: "Market Research created" },
        { key: "agent:Market Scout", status: "reused", message: "already there" },
      ],
      pipeline_id: "00000000-0000-0000-0000-000000000001",
      unowned_stages: ["plan"],
    });

    expect(result.steps).toHaveLength(2);
    expect(result.steps[0]).toEqual({
      key: "squad:market_research",
      status: "created",
      message: "Market Research created",
    });
    expect(result.pipeline_id).toBe("00000000-0000-0000-0000-000000000001");
    expect(result.unowned_stages).toEqual(["plan"]);
  });

  it("fills missing optional fields with defaults", () => {
    const result = read({ steps: [{ key: "agent:Librarian", status: "created" }] });

    expect(result.steps[0]!.message).toBe("");
    expect(result.pipeline_id).toBeNull();
    expect(result.unowned_stages).toEqual([]);
  });

  it("reads an unknown step status as failed, so it is never reported as done", () => {
    const result = read({
      steps: [
        { key: "a", status: "skipped_by_newer_server", message: "" },
        { key: "b", status: 7, message: "" },
        { key: "c", message: "" },
      ],
    });

    expect(result.steps.map((step) => step.status)).toEqual(["failed", "failed", "failed"]);
  });

  it("falls back to an empty result when the body is not an object", () => {
    expect(read("not json")).toEqual(FALLBACK_COMPANY_TEMPLATE_RESULT);
    expect(read(null)).toEqual(FALLBACK_COMPANY_TEMPLATE_RESULT);
  });

  it("falls back when steps is not a list", () => {
    expect(read({ steps: "created", pipeline_id: null, unowned_stages: [] })).toEqual(
      FALLBACK_COMPANY_TEMPLATE_RESULT,
    );
  });

  it("falls back when one step is not an object, so a partial list is never read as complete", () => {
    expect(read({ steps: [{ key: "a", status: "created" }, null] })).toEqual(
      FALLBACK_COMPANY_TEMPLATE_RESULT,
    );
  });

  it("degrades a malformed pipeline id to none without losing the steps", () => {
    const result = read({
      steps: [{ key: "a", status: "created", message: "" }],
      pipeline_id: 42,
      unowned_stages: ["build"],
    });

    expect(result.pipeline_id).toBeNull();
    expect(result.steps).toHaveLength(1);
    expect(result.unowned_stages).toEqual(["build"]);
  });

  it("degrades malformed unowned stages to none without losing the steps", () => {
    const result = read({
      steps: [{ key: "a", status: "created", message: "" }],
      unowned_stages: "validate",
    });

    expect(result.unowned_stages).toEqual([]);
    expect(result.steps).toHaveLength(1);
  });
});

describe("isCompanyTemplateComplete", () => {
  it("is true only when there are steps and none failed", () => {
    expect(isCompanyTemplateComplete(read({ steps: [{ key: "a", status: "created" }, { key: "b", status: "reused" }] }))).toBe(true);
    expect(isCompanyTemplateComplete(read({ steps: [{ key: "a", status: "created" }, { key: "b", status: "failed" }] }))).toBe(false);
  });

  it("is false for an unreadable response, which has no steps", () => {
    expect(isCompanyTemplateComplete(read("garbage"))).toBe(false);
  });
});

describe("buildCompanyTemplateApplyRequest", () => {
  it("leaves out a blank model and trims a real one", () => {
    const base = { runtimeId: "rt-1", squads: ["market_research" as const], start: "leave_paused" as const };

    expect(buildCompanyTemplateApplyRequest({ ...base, model: "   " })).toEqual({
      runtime_id: "rt-1",
      squads: ["market_research"],
      start: "leave_paused",
    });
    expect(buildCompanyTemplateApplyRequest({ ...base, model: " gpt-x " })).toEqual({
      runtime_id: "rt-1",
      model: "gpt-x",
      squads: ["market_research"],
      start: "leave_paused",
    });
  });
});
