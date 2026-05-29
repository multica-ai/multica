import { describe, expect, it } from "vitest";
import { parseWithFallback } from "@multica/core/api";
import {
  EMPTY_STATUS_MODELS_LIST,
  statusModelsListSchema,
  statusModelSchema,
  EMPTY_STATUS_MODEL,
} from "./api-schemas";

describe("cerebro-status-models schemas (API drift defense)", () => {
  it("falls back when the list body is malformed", () => {
    const got = parseWithFallback(
      { status_models: "not-an-array" },
      statusModelsListSchema,
      EMPTY_STATUS_MODELS_LIST,
      { endpoint: "test" },
    );
    expect(got).toEqual(EMPTY_STATUS_MODELS_LIST);
  });

  it("keeps unknown server fields and defaults missing optional ones", () => {
    const got = parseWithFallback(
      {
        id: "m1",
        workspace_id: "w1",
        name: "Plan-først",
        statuses: [{ key: "plan", label: "Plan", base_status: "todo" }],
        created_by_id: "u1",
        created_by_type: "member",
        created_at: "t",
        updated_at: "t",
        future_field: "kept",
      },
      statusModelSchema,
      EMPTY_STATUS_MODEL,
      { endpoint: "test" },
    );
    expect(got.name).toBe("Plan-først");
    // color/position default in, project_count defaults to 0.
    const [first] = got.statuses;
    expect(first?.color).toBe("");
    expect(first?.position).toBe(0);
    expect(got.project_count).toBe(0);
    expect((got as unknown as Record<string, unknown>).future_field).toBe("kept");
  });

  it("returns the fallback on a non-object body", () => {
    const got = parseWithFallback(null, statusModelSchema, EMPTY_STATUS_MODEL, {
      endpoint: "test",
    });
    expect(got).toEqual(EMPTY_STATUS_MODEL);
  });
});
