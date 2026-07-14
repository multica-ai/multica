import { describe, expect, it } from "vitest";
import { validateAppManifest, validateWorkflowDefinition } from "./schema";

describe("mini app contracts", () => {
  it("accepts the shared linear workflow JSON used by UI and CLI", () => {
    expect(validateWorkflowDefinition({
      schema_version: "1",
      trigger: { id: "trigger", type: "data_event", config: { source_id: "products" } },
      steps: [
        { id: "read", type: "registry.read", config: { source_id: "products" } },
        { id: "filter", type: "filter", config: { field: "read.count", operator: "gt", value: 0 } },
        { id: "view", type: "view.show_and_wait", config: { view_id: "approve" } },
        { id: "write", type: "registry.write", config: { destination_id: "products" } },
      ],
    })).toEqual([]);
  });

  it("rejects branching, loops, duplicate ids, and unknown nodes in v1", () => {
    const errors = validateWorkflowDefinition({ schema_version: "1", trigger: { id: "same", type: "manual", config: {} }, steps: [{ id: "same", type: "loop", config: {} }] });
    expect(errors.join(" ")).toMatch(/duplicate/i);
    expect(errors.join(" ")).toMatch(/not supported/i);
  });

  it("validates responsive in-chat view declarations", () => {
    expect(validateAppManifest({ schema_version: "1", name: "Allergen Formatter", version: "1.0.0", scopes: [], views: [{ id: "approve", type: "approval", title: "Approve formatting" }] })).toEqual([]);
    expect(validateAppManifest({ schema_version: "1", name: "App", version: "latest", scopes: [], views: [{ id: "x", type: "html" }] }).length).toBeGreaterThan(0);
  });
});
