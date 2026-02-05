import { describe, it, expect } from "vitest";
import {
  matchAllowlist,
  addAllowlistEntry,
  recordAllowlistUse,
  removeAllowlistEntry,
  normalizeAllowlist,
} from "./exec-allowlist.js";
import type { ExecAllowlistEntry } from "./exec-approval-types.js";

describe("matchAllowlist", () => {
  const entries: ExecAllowlistEntry[] = [
    { id: "1", pattern: "git *" },
    { id: "2", pattern: "pnpm test" },
    { id: "3", pattern: "ls **" },
    { id: "4", pattern: "node --version" },
  ];

  it("matches wildcard patterns", () => {
    expect(matchAllowlist(entries, "git status")).toBeTruthy();
    expect(matchAllowlist(entries, "git push origin main")).toBeNull(); // * doesn't match spaces
    expect(matchAllowlist(entries, "git log")).toBeTruthy();
  });

  it("matches exact patterns", () => {
    expect(matchAllowlist(entries, "pnpm test")).toBeTruthy();
    expect(matchAllowlist(entries, "node --version")).toBeTruthy();
  });

  it("matches double-star patterns", () => {
    expect(matchAllowlist(entries, "ls -la /tmp/some/path")).toBeTruthy();
  });

  it("is case-insensitive", () => {
    expect(matchAllowlist(entries, "GIT status")).toBeTruthy();
    expect(matchAllowlist(entries, "PNPM TEST")).toBeTruthy();
  });

  it("returns null for non-matching commands", () => {
    expect(matchAllowlist(entries, "rm -rf /")).toBeNull();
    expect(matchAllowlist(entries, "curl http://evil.com")).toBeNull();
    expect(matchAllowlist(entries, "pnpm build")).toBeNull();
  });

  it("returns null for empty inputs", () => {
    expect(matchAllowlist([], "git status")).toBeNull();
    expect(matchAllowlist(entries, "")).toBeNull();
    expect(matchAllowlist(entries, "  ")).toBeNull();
  });
});

describe("addAllowlistEntry", () => {
  it("adds new entry with UUID", () => {
    const entries: ExecAllowlistEntry[] = [];
    const result = addAllowlistEntry(entries, "git *");
    expect(result).toHaveLength(1);
    expect(result[0]!.pattern).toBe("git *");
    expect(result[0]!.id).toBeTruthy();
    expect(result[0]!.lastUsedAt).toBeTruthy();
  });

  it("deduplicates by pattern", () => {
    const entries: ExecAllowlistEntry[] = [{ id: "1", pattern: "git *" }];
    const result = addAllowlistEntry(entries, "git *");
    expect(result).toHaveLength(1); // no new entry
  });

  it("deduplicates case-insensitively", () => {
    const entries: ExecAllowlistEntry[] = [{ id: "1", pattern: "Git *" }];
    const result = addAllowlistEntry(entries, "git *");
    expect(result).toHaveLength(1);
  });

  it("trims pattern", () => {
    const entries: ExecAllowlistEntry[] = [];
    const result = addAllowlistEntry(entries, "  git *  ");
    expect(result[0]!.pattern).toBe("git *");
  });

  it("preserves existing entries", () => {
    const entries: ExecAllowlistEntry[] = [{ id: "1", pattern: "ls *" }];
    const result = addAllowlistEntry(entries, "git *");
    expect(result).toHaveLength(2);
    expect(result[0]!.pattern).toBe("ls *");
  });
});

describe("recordAllowlistUse", () => {
  it("updates lastUsedAt and lastUsedCommand", () => {
    const entry: ExecAllowlistEntry = { id: "1", pattern: "git *" };
    const entries = [entry];
    const result = recordAllowlistUse(entries, entry, "git status");
    expect(result[0]!.lastUsedAt).toBeTruthy();
    expect(result[0]!.lastUsedCommand).toBe("git status");
  });

  it("matches by ID", () => {
    const entries: ExecAllowlistEntry[] = [
      { id: "1", pattern: "git *" },
      { id: "2", pattern: "ls *" },
    ];
    const result = recordAllowlistUse(entries, { id: "2", pattern: "ls *" }, "ls -la");
    expect(result[0]!.lastUsedCommand).toBeUndefined();
    expect(result[1]!.lastUsedCommand).toBe("ls -la");
  });

  it("matches by pattern when no ID", () => {
    const entries: ExecAllowlistEntry[] = [{ pattern: "git *" }];
    const result = recordAllowlistUse(entries, { pattern: "git *" }, "git log");
    expect(result[0]!.lastUsedCommand).toBe("git log");
  });
});

describe("removeAllowlistEntry", () => {
  it("removes by pattern", () => {
    const entries: ExecAllowlistEntry[] = [
      { id: "1", pattern: "git *" },
      { id: "2", pattern: "ls *" },
    ];
    const result = removeAllowlistEntry(entries, "git *");
    expect(result).toHaveLength(1);
    expect(result[0]!.pattern).toBe("ls *");
  });

  it("removes by ID", () => {
    const entries: ExecAllowlistEntry[] = [
      { id: "1", pattern: "git *" },
      { id: "2", pattern: "ls *" },
    ];
    const result = removeAllowlistEntry(entries, "1");
    expect(result).toHaveLength(1);
    expect(result[0]!.id).toBe("2");
  });

  it("is case-insensitive for patterns", () => {
    const entries: ExecAllowlistEntry[] = [{ id: "1", pattern: "Git *" }];
    const result = removeAllowlistEntry(entries, "git *");
    expect(result).toHaveLength(0);
  });
});

describe("normalizeAllowlist", () => {
  it("assigns IDs to entries without them", () => {
    const entries: ExecAllowlistEntry[] = [{ pattern: "git *" }];
    const result = normalizeAllowlist(entries);
    expect(result[0]!.id).toBeTruthy();
  });

  it("preserves existing IDs", () => {
    const entries: ExecAllowlistEntry[] = [{ id: "my-id", pattern: "git *" }];
    const result = normalizeAllowlist(entries);
    expect(result[0]!.id).toBe("my-id");
  });

  it("deduplicates by pattern", () => {
    const entries: ExecAllowlistEntry[] = [
      { id: "1", pattern: "git *" },
      { id: "2", pattern: "Git *" }, // duplicate (case-insensitive)
    ];
    const result = normalizeAllowlist(entries);
    expect(result).toHaveLength(1);
    expect(result[0]!.id).toBe("1"); // first one wins
  });
});
