// @vitest-environment node

/**
 * Pins the plan-write route table against the server's contract, route by
 * route and verb by verb.
 *
 * This is transcription, so it is worth asserting literally: three entries
 * differ from the shape a REST convention would suggest — both reorders are
 * PATCH rather than POST, and
 * LinkIssue carries the issue id in the path rather than a body. A test that
 * only checked nesting would have let all three through.
 *
 * If a route moves, this file is where the move becomes visible and reviewed
 * rather than silent.
 */

import { describe, expect, it } from "vitest";
import { PLAN_WRITE_METHODS, planWriteRoutes } from "./write-routes";

const P = "proj-1";
const PLAN = "plan-1";

describe("planWriteRoutes", () => {
  it("nests plan paths under the project, matching the read routes", () => {
    expect(planWriteRoutes.createPlan(P)).toBe("/api/projects/proj-1/plans");
    expect(planWriteRoutes.updatePlan(P, PLAN)).toBe("/api/projects/proj-1/plans/plan-1");
    expect(planWriteRoutes.deletePlan(P, PLAN)).toBe("/api/projects/proj-1/plans/plan-1");
    expect(planWriteRoutes.supersedePlan(P, PLAN)).toBe(
      "/api/projects/proj-1/plans/plan-1/supersede",
    );
  });

  it("nests phase paths under the plan", () => {
    expect(planWriteRoutes.addPhase(P, PLAN)).toBe("/api/projects/proj-1/plans/plan-1/phases");
    expect(planWriteRoutes.updatePhase(P, PLAN, "ph-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/ph-1",
    );
    expect(planWriteRoutes.deletePhase(P, PLAN, "ph-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/ph-1",
    );
    expect(planWriteRoutes.reorderPhases(P, PLAN)).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/reorder",
    );
  });

  it("creates a part under its phase but addresses an existing one by id", () => {
    // `AddPart` needs the phase (it validates the phase belongs to the plan);
    // `UpdatePart` / `DeletePart` take only plan + part.
    expect(planWriteRoutes.addPart(P, PLAN, "ph-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/ph-1/parts",
    );
    expect(planWriteRoutes.updatePart(P, PLAN, "pt-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/parts/pt-1",
    );
    expect(planWriteRoutes.reorderParts(P, PLAN, "ph-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/phases/ph-1/parts/reorder",
    );
  });

  it("names the issue in the path for both link and unlink", () => {
    // Link and unlink share one path; only the verb differs. An earlier cut
    // POSTed to the collection with an {issue_id} body — the server has no
    // such route.
    expect(planWriteRoutes.linkIssue(P, PLAN, "pt-1", "iss-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/parts/pt-1/issues/iss-1",
    );
    expect(planWriteRoutes.unlinkIssue(P, PLAN, "pt-1", "iss-1")).toBe(
      "/api/projects/proj-1/plans/plan-1/parts/pt-1/issues/iss-1",
    );
  });

  it("matches the server's verb for every operation", () => {
    expect(PLAN_WRITE_METHODS).toEqual({
      createPlan: "POST",
      updatePlan: "PATCH",
      supersedePlan: "POST",
      deletePlan: "DELETE",
      addPhase: "POST",
      updatePhase: "PATCH",
      // Both reorders are PATCH. They were POST in the provisional table.
      reorderPhases: "PATCH",
      deletePhase: "DELETE",
      addPart: "POST",
      updatePart: "PATCH",
      reorderParts: "PATCH",
      deletePart: "DELETE",
      linkIssue: "POST",
      unlinkIssue: "DELETE",
    });
  });

  it("declares a verb for every route", () => {
    expect(Object.keys(PLAN_WRITE_METHODS).sort()).toEqual(Object.keys(planWriteRoutes).sort());
  });

  it("uses a non-mutating verb for nothing", () => {
    expect(Object.values(PLAN_WRITE_METHODS)).not.toContain("GET");
  });
});
