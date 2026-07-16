import { describe, expect, it } from "vitest";
import { validateAppManifest } from "./schema";

describe("mini app contracts", () => {
  it("validates responsive in-chat view declarations", () => {
    expect(validateAppManifest({ schema_version: "1", name: "Allergen Formatter", version: "1.0.0", scopes: [], views: [{ id: "approve", type: "approval", title: "Approve formatting" }] })).toEqual([]);
    expect(validateAppManifest({ schema_version: "1", name: "App", version: "latest", scopes: [], views: [{ id: "x", type: "html" }] }).length).toBeGreaterThan(0);
  });
});
