import { describe, expect, it } from "vitest";
import {
  CREATABLE_RUNTIME_PROFILE_PROTOCOL_FAMILIES,
  RUNTIME_PROFILE_PROTOCOL_FAMILIES,
  isCreatableRuntimeProfileProtocolFamily,
} from "./agent";

describe("runtime profile protocol families", () => {
  it("keeps Prime readable but excludes it from public creation", () => {
    expect(RUNTIME_PROFILE_PROTOCOL_FAMILIES).toContain("prime");
    expect(CREATABLE_RUNTIME_PROFILE_PROTOCOL_FAMILIES).not.toContain("prime");
    expect(RUNTIME_PROFILE_PROTOCOL_FAMILIES).toEqual([
      ...CREATABLE_RUNTIME_PROFILE_PROTOCOL_FAMILIES,
      "prime",
    ]);
    expect(isCreatableRuntimeProfileProtocolFamily("prime")).toBe(false);
    expect(isCreatableRuntimeProfileProtocolFamily("codex")).toBe(true);
  });
});
