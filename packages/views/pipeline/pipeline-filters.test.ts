import { describe, expect, it } from "vitest";
import type { Initiative, PipelineStage } from "@multica/core/pipeline";
import {
  matchesStatusFilter,
  ownedOpenCount,
  parseStatusFilter,
  stageColumns,
  statusParamFor,
} from "./pipeline-filters";

const stage = (key: string, name = key): PipelineStage => ({
  key,
  name,
  owner_squad_id: null,
  objective: "",
  instructions: "",
  artifacts: [],
  exit: { mode: "auto", criteria: "" },
  sla_days: null,
});

function initiative(id: string, extra: Partial<Initiative> = {}): Initiative {
  return {
    id,
    identifier: `INI-${id}`,
    title: `Initiative ${id}`,
    thesis: "",
    pipeline_id: "pl-1",
    stage_key: "validate",
    owner_squad_id: "sq-research",
    status: "active",
    issue_id: `iss-${id}`,
    build_project_id: null,
    stage_entered_at: "",
    days_in_stage: 0,
    sla_state: "ok",
    artifacts_required: 0,
    artifacts_present: 0,
    pending_gate_proposal_id: null,
    ...extra,
  };
}

describe("parseStatusFilter", () => {
  it("reads a known status and falls back to open", () => {
    expect(parseStatusFilter("shelved")).toBe("shelved");
    expect(parseStatusFilter("open")).toBe("open");
    expect(parseStatusFilter(null)).toBe("open");
    expect(parseStatusFilter("archived")).toBe("open");
  });
});

describe("status filter", () => {
  it("asks the server for everything under Open and for the named status otherwise", () => {
    expect(statusParamFor("open")).toBeUndefined();
    expect(statusParamFor("shelved")).toBe("shelved");
  });

  it("shows running work under Open and leaves shelved and closed off the active board", () => {
    expect(matchesStatusFilter({ status: "active" }, "open")).toBe(true);
    expect(matchesStatusFilter({ status: "operating" }, "open")).toBe(true);
    expect(matchesStatusFilter({ status: "shelved" }, "open")).toBe(false);
    expect(matchesStatusFilter({ status: "closed" }, "open")).toBe(false);
    expect(matchesStatusFilter({ status: "shelved" }, "shelved")).toBe(true);
    expect(matchesStatusFilter({ status: "active" }, "shelved")).toBe(false);
  });
});

describe("stageColumns", () => {
  it("places each initiative in its stage column, in pipeline order", () => {
    const columns = stageColumns(
      [stage("validate", "Validate"), stage("define", "Define")],
      [initiative("1", { stage_key: "define" }), initiative("2", { stage_key: "validate" })],
    );
    expect(columns.map((column) => [column.key, column.initiatives.map((i) => i.id)])).toEqual([
      ["validate", ["2"]],
      ["define", ["1"]],
    ]);
    expect(columns[0]?.stage?.name).toBe("Validate");
  });

  it("keeps an initiative whose stage the pipeline does not list in its own column", () => {
    const columns = stageColumns([stage("validate")], [initiative("9", { stage_key: "retired" })]);
    const last = columns[columns.length - 1];
    expect(last).toMatchObject({ key: "retired", stage: null });
    expect(last?.initiatives.map((i) => i.id)).toEqual(["9"]);
  });

  it("keeps empty stages as columns", () => {
    expect(stageColumns([stage("validate"), stage("plan")], [])).toHaveLength(2);
  });
});

describe("ownedOpenCount", () => {
  it("counts only the open initiatives of one squad", () => {
    const list = [
      initiative("1"),
      initiative("2", { status: "operating" }),
      initiative("3", { status: "shelved" }),
      initiative("4", { status: "closed" }),
      initiative("5", { owner_squad_id: "sq-other" }),
      initiative("6", { owner_squad_id: null }),
    ];
    expect(ownedOpenCount(list, "sq-research")).toBe(2);
    expect(ownedOpenCount(list, "sq-none")).toBe(0);
  });
});
