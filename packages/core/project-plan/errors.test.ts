// @vitest-environment node

/**
 * Canonical coverage for plan-write error classification.
 *
 * The rule under test is the honest-error bar: a 409 must come out as a
 * conflict, and a failure with no server status must NOT — calling a dropped
 * connection a "conflict" is its own fabrication. Both the `code`-present and
 * `code`-absent paths matter, because the handler may or may not emit a
 * machine-readable code.
 */

import { describe, expect, it } from "vitest";
import { ApiError } from "../api/client";
import { classifyPlanWriteError } from "./errors";

const apiError = (status: number, body?: unknown) =>
  new ApiError(`API error: ${status}`, status, "", body);

describe("classifyPlanWriteError", () => {
  it("reads a coded 409 as its specific conflict kind", () => {
    const result = classifyPlanWriteError(
      apiError(409, { code: "position_conflict", error: "another phase already uses this position" }),
    );
    expect(result.kind).toBe("position_conflict");
    expect(result.conflict).toBe(true);
    expect(result.message).toBe("another phase already uses this position");
  });

  it("still reads an uncoded 409 as a conflict", () => {
    const result = classifyPlanWriteError(apiError(409, { error: "conflict" }));
    expect(result.conflict).toBe(true);
    expect(result.status).toBe(409);
  });

  it("treats a coded conflict as a conflict even on a non-409 status", () => {
    // A handler that maps `active_plan_exists` to 422 rather than 409 must
    // still read as a conflict to the person looking at it.
    const result = classifyPlanWriteError(apiError(422, { code: "active_plan_exists" }));
    expect(result.kind).toBe("active_plan_exists");
    expect(result.conflict).toBe(true);
  });

  it("does not call a 500 a conflict", () => {
    const result = classifyPlanWriteError(apiError(500));
    expect(result.kind).toBe("unavailable");
    expect(result.conflict).toBe(false);
  });

  it("maps 404 to not_found and 400/422 to invalid", () => {
    expect(classifyPlanWriteError(apiError(404)).kind).toBe("not_found");
    expect(classifyPlanWriteError(apiError(400)).kind).toBe("invalid");
    expect(classifyPlanWriteError(apiError(422)).kind).toBe("invalid");
  });

  it("carries the server's own message through in preference to any local copy", () => {
    const result = classifyPlanWriteError(
      apiError(400, { error: "only prd plans are supported in this release" }),
    );
    expect(result.message).toBe("only prd plans are supported in this release");
  });

  it("drops ApiClient's synthesized boilerplate so the caller's localized copy wins", () => {
    // A bodyless 404 leaves ApiError.message as "API error: 404" — no
    // information a reader can act on. Showing it verbatim leaked the raw
    // status into the dialog; an empty message hands the decision back to
    // usePlanWriteError, which has per-kind copy.
    const result = classifyPlanWriteError(apiError(404));
    expect(result.kind).toBe("not_found");
    expect(result.message).toBe("");
  });

  it("accepts `message` as well as `error` in the body", () => {
    expect(classifyPlanWriteError(apiError(404, { message: "phase not found" })).message).toBe(
      "phase not found",
    );
  });

  it("ignores a code the server invented that is not a known kind", () => {
    const result = classifyPlanWriteError(apiError(418, { code: "teapot" }));
    expect(result.kind).toBe("unknown");
    expect(result.conflict).toBe(false);
  });

  it("classifies a non-ApiError failure as unavailable with no status", () => {
    const result = classifyPlanWriteError(new TypeError("Failed to fetch"));
    expect(result.kind).toBe("unavailable");
    expect(result.status).toBeNull();
    expect(result.conflict).toBe(false);
    expect(result.message).toBe("Failed to fetch");
  });

  it("survives a body that is not an object", () => {
    expect(classifyPlanWriteError(apiError(409, "conflict")).conflict).toBe(true);
    expect(classifyPlanWriteError(apiError(500, null)).kind).toBe("unavailable");
  });
});
