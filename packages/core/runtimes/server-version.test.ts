import { describe, expect, it } from "vitest";
import { checkServerVersion } from "./server-version";

describe("checkServerVersion", () => {
  it("treats an equal version as ok", () => {
    expect(checkServerVersion("0.4.12", "0.4.12").state).toBe("ok");
  });

  it("treats a newer server as ok", () => {
    expect(checkServerVersion("0.5.0", "0.4.12").state).toBe("ok");
  });

  it("flags a server older than the app as too_old", () => {
    const r = checkServerVersion("0.4.11", "0.4.12");
    expect(r.state).toBe("too_old");
    expect(r.current).toBe("0.4.11");
    expect(r.min).toBe("0.4.12");
  });

  it("treats a missing server version as unknown (managed cloud omits it)", () => {
    expect(checkServerVersion("", "0.4.12").state).toBe("unknown");
    expect(checkServerVersion(undefined, "0.4.12").state).toBe("unknown");
  });

  it("treats an unparsable server version as unknown", () => {
    expect(checkServerVersion("not-a-version", "0.4.12").state).toBe("unknown");
  });

  it("treats a dev/source-built server (git-describe) as ok", () => {
    expect(checkServerVersion("v0.4.11-235-gdaf0e935", "0.4.12").state).toBe("ok");
  });

  it("treats an unparsable app version as unknown", () => {
    expect(checkServerVersion("0.4.12", "").state).toBe("unknown");
  });
});
