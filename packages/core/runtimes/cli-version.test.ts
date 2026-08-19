import { describe, it, expect } from "vitest";
import {
  chatProjectContextSupported,
  checkQuickCreateCliVersion,
  checkQuickCreateFieldsCliVersion,
  handoffSupported,
  MIN_CHAT_PROJECT_CONTEXT_CLI_VERSION,
  MIN_HANDOFF_CLI_VERSION,
  runtimeAdvertisesLocalWorktree,
} from "./cli-version";

describe("checkQuickCreateCliVersion", () => {
  it("returns ok for a tagged release at or above the minimum", () => {
    expect(checkQuickCreateCliVersion("v0.2.21").state).toBe("ok");
    expect(checkQuickCreateCliVersion("0.3.1").state).toBe("ok");
  });

  it("returns too_old for a tagged release below the minimum", () => {
    expect(checkQuickCreateCliVersion("v0.2.20").state).toBe("too_old");
    expect(checkQuickCreateCliVersion("v0.2.15").state).toBe("too_old");
  });

  it("returns missing for empty or unparsable input", () => {
    expect(checkQuickCreateCliVersion("").state).toBe("missing");
    expect(checkQuickCreateCliVersion(undefined).state).toBe("missing");
    expect(checkQuickCreateCliVersion("not-a-version").state).toBe("missing");
  });

  it("treats git-describe dev builds as ok regardless of base tag", () => {
    expect(checkQuickCreateCliVersion("v0.2.15-235-gdaf0e935").state).toBe("ok");
    expect(checkQuickCreateCliVersion("v0.2.15-235-gdaf0e935-dirty").state).toBe("ok");
    expect(checkQuickCreateCliVersion("0.1.0-1-gabc1234").state).toBe("ok");
  });
});

describe("checkQuickCreateFieldsCliVersion", () => {
  it("requires the first daemon release that transports explicit fields", () => {
    expect(checkQuickCreateFieldsCliVersion("0.4.2").state).toBe("too_old");
    expect(checkQuickCreateFieldsCliVersion("0.4.3").state).toBe("ok");
    expect(checkQuickCreateFieldsCliVersion("v0.4.3-1-gabc1234").state).toBe("ok");
  });
});

// Mirrors server/pkg/agent/handoff_version_test.go so the frontend soft-gate
// signal and the server's authoritative one agree by construction.
describe("handoffSupported", () => {
  it("supports a tagged release at or above the minimum", () => {
    expect(handoffSupported(MIN_HANDOFF_CLI_VERSION)).toBe(true);
    expect(handoffSupported("0.4.0")).toBe(true);
    expect(handoffSupported("v0.3.28")).toBe(true);
  });

  it("does not support a tagged release below the minimum", () => {
    expect(handoffSupported("0.3.26")).toBe(false);
    expect(handoffSupported("0.2.21")).toBe(false);
  });

  it("fails closed on empty or unparsable input", () => {
    expect(handoffSupported("")).toBe(false);
    expect(handoffSupported(undefined)).toBe(false);
    expect(handoffSupported(null)).toBe(false);
    expect(handoffSupported("garbage")).toBe(false);
  });

  it("treats git-describe dev builds as supported regardless of base tag", () => {
    expect(handoffSupported("v0.3.0-5-gabc1234")).toBe(true);
    expect(handoffSupported("v0.1.0-235-gdaf0e935-dirty")).toBe(true);
  });
});

describe("chatProjectContextSupported", () => {
  it("supports a tagged release at or above the minimum", () => {
    expect(chatProjectContextSupported(MIN_CHAT_PROJECT_CONTEXT_CLI_VERSION)).toBe(true);
    expect(chatProjectContextSupported("v0.4.10")).toBe(true);
    expect(chatProjectContextSupported("0.5.0")).toBe(true);
  });

  it("does not support a tagged release below the minimum", () => {
    expect(chatProjectContextSupported("0.4.9")).toBe(false);
    expect(chatProjectContextSupported("0.3.28")).toBe(false);
  });

  it("fails closed on empty or unparsable input", () => {
    expect(chatProjectContextSupported("")).toBe(false);
    expect(chatProjectContextSupported(undefined)).toBe(false);
    expect(chatProjectContextSupported(null)).toBe(false);
    expect(chatProjectContextSupported("garbage")).toBe(false);
  });

  it("treats git-describe dev builds as supported regardless of base tag", () => {
    expect(chatProjectContextSupported("v0.4.8-37-g5d0275d68")).toBe(true);
    expect(chatProjectContextSupported("v0.1.0-235-gdaf0e935-dirty")).toBe(true);
  });
});

describe("runtimeAdvertisesLocalWorktree", () => {
  const capable = { capabilities: ["skill-bundles-v1", "local-worktree-v1"] };
  const incapable = { cli_version: "9.9.9", capabilities: null };
  const row = (metadata: unknown, last_seen_at: string | null = "2026-08-13T00:00:00Z") => ({
    daemon_id: "d1",
    last_seen_at,
    metadata,
  });

  it("reads the advertised capability", () => {
    expect(runtimeAdvertisesLocalWorktree([row(capable)], "d1")).toBe(true);
    expect(runtimeAdvertisesLocalWorktree([row({ capabilities: ["skill-bundles-v1"] })], "d1")).toBe(
      false,
    );
  });

  // The whole reason this replaced a version check: a dev-built daemon reports a
  // git-describe string that the version floor exempts, so a binary with no
  // worktree implementation passed and two tasks ran in the user's own
  // directory (MUL-5707).
  it("ignores the daemon version string in both directions", () => {
    expect(runtimeAdvertisesLocalWorktree([row({ cli_version: "9.9.9" })], "d1")).toBe(false);
    expect(
      runtimeAdvertisesLocalWorktree(
        [row({ cli_version: "0.0.1", capabilities: ["local-worktree-v1"] })],
        "d1",
      ),
    ).toBe(true);
  });

  // Everything unreadable is simply "has not advertised". This function no
  // longer tries to say WHY — an old daemon, a row written before the server
  // recorded capabilities, and a server too old to record them are
  // indistinguishable from here, and pretending otherwise is what blocked a
  // user on the newest release with an instruction that could not help (#7113).
  it("says nothing about the reason when no row advertises", () => {
    for (const metadata of [{}, { cli_version: "v0.4.28" }, undefined, null, "nope", 42]) {
      expect(runtimeAdvertisesLocalWorktree([row(metadata)], "d1")).toBe(false);
    }
    expect(runtimeAdvertisesLocalWorktree([row(incapable)], "d1")).toBe(false);
    expect(runtimeAdvertisesLocalWorktree([row({ capabilities: "local-worktree-v1" })], "d1")).toBe(
      false,
    );
  });

  // Deregistering only marks a runtime offline; its metadata survives and the
  // list endpoint still returns it. An any-row match would keep preselecting
  // parallel for a machine that has since downgraded.
  it("ignores a stale capable row once a newer row lacks the capability", () => {
    expect(
      runtimeAdvertisesLocalWorktree(
        [row(capable, "2026-08-01T00:00:00Z"), row(incapable, "2026-08-13T00:00:00Z")],
        "d1",
      ),
    ).toBe(false);
  });

  it("recognises an upgrade: newest row advertises it", () => {
    expect(
      runtimeAdvertisesLocalWorktree(
        [row(incapable, "2026-08-01T00:00:00Z"), row(capable, "2026-08-13T00:00:00Z")],
        "d1",
      ),
    ).toBe(true);
  });

  it("does not depend on array order", () => {
    const rows = [row(incapable, "2026-08-13T00:00:00Z"), row(capable, "2026-08-01T00:00:00Z")];
    expect(runtimeAdvertisesLocalWorktree(rows, "d1")).toBe(false);
    expect(runtimeAdvertisesLocalWorktree([...rows].reverse(), "d1")).toBe(false);
  });

  it("a row that never reported loses to one that did", () => {
    expect(
      runtimeAdvertisesLocalWorktree(
        [row(capable, null), row(incapable, "2026-08-13T00:00:00Z")],
        "d1",
      ),
    ).toBe(false);
  });

  it("ignores other daemons, and answers no with no rows or no id", () => {
    const other = [{ daemon_id: "d2", last_seen_at: "2026-08-13T00:00:00Z", metadata: capable }];
    expect(runtimeAdvertisesLocalWorktree(other, "d1")).toBe(false);
    expect(runtimeAdvertisesLocalWorktree([], "d1")).toBe(false);
    expect(runtimeAdvertisesLocalWorktree(other, null)).toBe(false);
  });
});
