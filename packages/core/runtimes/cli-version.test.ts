import { describe, it, expect } from "vitest";
import {
  chatProjectContextSupported,
  checkQuickCreateCliVersion,
  checkQuickCreateFieldsCliVersion,
  handoffSupported,
  MIN_CHAT_PROJECT_CONTEXT_CLI_VERSION,
  MIN_HANDOFF_CLI_VERSION,
  runtimeSupportsLocalWorktree,
  daemonSupportsLocalWorktree,
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

describe("runtimeSupportsLocalWorktree", () => {
  it("reads the advertised capability", () => {
    expect(
      runtimeSupportsLocalWorktree({ capabilities: ["skill-bundles-v1", "local-worktree-v1"] }),
    ).toBe(true);
    expect(runtimeSupportsLocalWorktree({ capabilities: ["skill-bundles-v1"] })).toBe(false);
  });

  // The whole reason this replaced a version check: a dev-built daemon reports a
  // git-describe string that the version floor exempts, so a binary with no
  // worktree implementation passed and two tasks ran in the user's own
  // directory (MUL-5707). The capability must ignore versions entirely.
  it("ignores the version string in both directions", () => {
    expect(runtimeSupportsLocalWorktree({ cli_version: "v0.4.21-24-gcd3c0bb89" })).toBe(false);
    expect(runtimeSupportsLocalWorktree({ cli_version: "9.9.9" })).toBe(false);
    expect(
      runtimeSupportsLocalWorktree({ cli_version: "0.0.1", capabilities: ["local-worktree-v1"] }),
    ).toBe(true);
  });

  it("fails closed on anything it cannot read", () => {
    for (const metadata of [undefined, null, {}, "nope", 42, { capabilities: "local-worktree-v1" }]) {
      expect(runtimeSupportsLocalWorktree(metadata)).toBe(false);
    }
  });
});

describe("daemonSupportsLocalWorktree", () => {
  const capable = { capabilities: ["local-worktree-v1"] };
  const incapable = { cli_version: "9.9.9" };

  // Deregistering only marks a runtime offline; its metadata survives and the
  // list endpoint still returns it. An any-row match would therefore keep
  // vouching for a machine that has since downgraded, so the UI would offer a
  // mode the server refuses at claim time.
  it("ignores a stale capable row once a newer row lacks the capability", () => {
    expect(
      daemonSupportsLocalWorktree(
        [
          { daemon_id: "d1", last_seen_at: "2026-08-01T00:00:00Z", metadata: capable },
          { daemon_id: "d1", last_seen_at: "2026-08-13T00:00:00Z", metadata: incapable },
        ],
        "d1",
      ),
    ).toBe(false);
  });

  it("recognises an upgrade: newest row advertises it", () => {
    expect(
      daemonSupportsLocalWorktree(
        [
          { daemon_id: "d1", last_seen_at: "2026-08-01T00:00:00Z", metadata: incapable },
          { daemon_id: "d1", last_seen_at: "2026-08-13T00:00:00Z", metadata: capable },
        ],
        "d1",
      ),
    ).toBe(true);
  });

  it("does not depend on array order", () => {
    const rows = [
      { daemon_id: "d1", last_seen_at: "2026-08-13T00:00:00Z", metadata: incapable },
      { daemon_id: "d1", last_seen_at: "2026-08-01T00:00:00Z", metadata: capable },
    ];
    expect(daemonSupportsLocalWorktree(rows, "d1")).toBe(false);
    expect(daemonSupportsLocalWorktree([...rows].reverse(), "d1")).toBe(false);
  });

  it("a row that never reported loses to one that did", () => {
    expect(
      daemonSupportsLocalWorktree(
        [
          { daemon_id: "d1", last_seen_at: null, metadata: capable },
          { daemon_id: "d1", last_seen_at: "2026-08-13T00:00:00Z", metadata: incapable },
        ],
        "d1",
      ),
    ).toBe(false);
  });

  it("ignores other daemons, and fails closed with no rows or no id", () => {
    const other = [{ daemon_id: "d2", last_seen_at: "2026-08-13T00:00:00Z", metadata: capable }];
    expect(daemonSupportsLocalWorktree(other, "d1")).toBe(false);
    expect(daemonSupportsLocalWorktree([], "d1")).toBe(false);
    expect(daemonSupportsLocalWorktree(other, null)).toBe(false);
  });
});
