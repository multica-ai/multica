// @vitest-environment node
import { describe, it, expect } from "vitest";
import { RuntimeProfileSchema } from "./schemas";
import { parseWithFallback } from "./schema";
const profile = {
  id: "p",
  workspace_id: "ws",
  display_name: "Custom",
  protocol_family: "pi",
  command_name: "wrapper",
};
describe("runtime profile response compatibility", () => {
  it("defaults old or malformed identities to the protocol", () => {
    for (const runtime_type of [undefined, null, 42, ""]) {
      expect(
        RuntimeProfileSchema.parse({ ...profile, runtime_type }).runtime_type,
      ).toBe("pi");
    }
  });
  it("preserves runtime identities including future targets", () => {
    for (const runtime_type of ["omp", "future-runtime"]) {
      expect(
        RuntimeProfileSchema.parse({ ...profile, runtime_type }).runtime_type,
      ).toBe(runtime_type);
    }
  });
  it("uses a safe fallback for malformed responses", () => {
    expect(
      parseWithFallback(
        { ...profile, command_name: 42 },
        RuntimeProfileSchema,
        null,
        { endpoint: "runtimeProfile" },
      ),
    ).toBeNull();
  });
});
