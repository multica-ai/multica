import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "@multica/core/types";
import { runtimeComputerName } from "./runtime-computer-name";

// Minimal AgentRuntime factory — only the fields runtimeComputerName reads.
function rt(partial: Partial<AgentRuntime>): AgentRuntime {
  return { name: "", device_info: "", ...partial } as AgentRuntime;
}

describe("runtimeComputerName", () => {
  it("extracts the hostname from a 'base (hostname)' name", () => {
    expect(runtimeComputerName(rt({ name: "claude-code (Jespers-MacBook)" }))).toBe(
      "Jespers-MacBook",
    );
  });

  it("uses the whole name when there is no hostname in parens", () => {
    expect(runtimeComputerName(rt({ name: "Jespers-MacBook" }))).toBe(
      "Jespers-MacBook",
    );
  });

  it("falls back to the first device_info segment when the name has no machine", () => {
    expect(
      runtimeComputerName(
        rt({ name: "", device_info: "office-mini · macOS 15 · arm64" }),
      ),
    ).toBe("office-mini");
  });

  it("prefers the hostname over device_info", () => {
    expect(
      runtimeComputerName(
        rt({ name: "codex (build-box)", device_info: "other · linux" }),
      ),
    ).toBe("build-box");
  });

  it("returns null when nothing identifies the machine", () => {
    expect(runtimeComputerName(rt({ name: "", device_info: "" }))).toBeNull();
  });
});
