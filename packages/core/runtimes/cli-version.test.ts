import { describe, it, expect } from "vitest";
import { checkQuickCreateCliVersion } from "./cli-version";

describe("checkQuickCreateCliVersion", () => {
  it("returns ok for tagged releases regardless of version", () => {
    expect(checkQuickCreateCliVersion("v0.2.21").state).toBe("ok");
    expect(checkQuickCreateCliVersion("0.3.1").state).toBe("ok");
    expect(checkQuickCreateCliVersion("v0.2.20").state).toBe("ok");
    expect(checkQuickCreateCliVersion("v0.2.15").state).toBe("ok");
  });

  it("returns ok for empty or unparsable input", () => {
    expect(checkQuickCreateCliVersion("").state).toBe("ok");
    expect(checkQuickCreateCliVersion(undefined).state).toBe("ok");
    expect(checkQuickCreateCliVersion("not-a-version").state).toBe("ok");
  });

  it("treats development/build-hash versions as ok", () => {
    expect(checkQuickCreateCliVersion("dev").state).toBe("ok");
    expect(checkQuickCreateCliVersion("development").state).toBe("ok");
    expect(checkQuickCreateCliVersion("db01ce593").state).toBe("ok");
    expect(checkQuickCreateCliVersion("gdb01ce593-dirty").state).toBe("ok");
  });

  it("treats git-describe dev builds as ok regardless of base tag", () => {
    expect(checkQuickCreateCliVersion("v0.2.15-235-gdaf0e935").state).toBe("ok");
    expect(checkQuickCreateCliVersion("v0.2.15-235-gdaf0e935-dirty").state).toBe("ok");
    expect(checkQuickCreateCliVersion("0.1.0-1-gabc1234").state).toBe("ok");
  });
});
