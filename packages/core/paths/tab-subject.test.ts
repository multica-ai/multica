import { describe, it, expect } from "vitest";
import { parseTabSubject, tabSubjectKey, type TabSubject } from "./tab-subject";

describe("parseTabSubject", () => {
  // Table-driven over every desktop workspace route, including collection vs
  // detail, the /new flow, nested runtime, container query selection, and
  // unknown paths. A new route type that isn't handled here will surface as
  // `unknown` — which the presentation layer renders neutrally, never as Issues.
  const cases: Array<[string, TabSubject]> = [
    // Collection / tool pages
    ["/acme/issues", { kind: "page", page: "issues" }],
    ["/acme/my-issues", { kind: "page", page: "myIssues" }],
    ["/acme/projects", { kind: "page", page: "projects" }],
    ["/acme/autopilots", { kind: "page", page: "autopilots" }],
    ["/acme/agents", { kind: "page", page: "agents" }],
    ["/acme/squads", { kind: "page", page: "squads" }],
    ["/acme/usage", { kind: "page", page: "usage" }],
    ["/acme/runtimes", { kind: "page", page: "runtimes" }],
    ["/acme/skills", { kind: "page", page: "skills" }],
    ["/acme/settings", { kind: "page", page: "settings" }],
    // Resource details
    ["/acme/issues/bug-1", { kind: "issue", id: "bug-1" }],
    ["/acme/projects/p1", { kind: "project", id: "p1" }],
    ["/acme/autopilots/a1", { kind: "autopilot", id: "a1" }],
    ["/acme/skills/s1", { kind: "skill", id: "s1" }],
    ["/acme/attachments/att1/preview", { kind: "attachment", id: "att1", filename: null }],
    [
      "/acme/attachments/att1/preview?name=report.pdf",
      { kind: "attachment", id: "att1", filename: "report.pdf" },
    ],
    [
      "/acme/attachments/att1/preview?name=my%20photo.png",
      { kind: "attachment", id: "att1", filename: "my photo.png" },
    ],
    // Actors
    ["/acme/agents/ag1", { kind: "actor", actorType: "agent", id: "ag1" }],
    ["/acme/members/m1", { kind: "actor", actorType: "member", id: "m1" }],
    ["/acme/squads/sq1", { kind: "actor", actorType: "squad", id: "sq1" }],
    // Flow — /new must win over the actor detail pattern
    ["/acme/agents/new", { kind: "flow", flow: "create-agent" }],
    // Runtime machine vs nested runtime
    ["/acme/runtimes/machine-1", { kind: "machine", machineId: "machine-1" }],
    [
      "/acme/runtimes/machine-1/runtime/rt-2",
      { kind: "runtime", machineId: "machine-1", runtimeId: "rt-2" },
    ],
    // Containers — selection (and archived sub-list) live in the query string
    ["/acme/inbox", { kind: "inbox", selectedKey: null, archived: false }],
    ["/acme/inbox?issue=MUL-9", { kind: "inbox", selectedKey: "MUL-9", archived: false }],
    ["/acme/inbox?view=archived", { kind: "inbox", selectedKey: null, archived: true }],
    [
      "/acme/inbox?view=archived&issue=MUL-9",
      { kind: "inbox", selectedKey: "MUL-9", archived: true },
    ],
    ["/acme/chat", { kind: "chat", sessionId: null }],
    ["/acme/chat?session=sess-1", { kind: "chat", sessionId: "sess-1" }],
    ["/acme/chat?agent=ag-1", { kind: "chat", sessionId: null }],
    // Members list route does not exist
    ["/acme/members", { kind: "unknown" }],
    // Unknown / too short
    ["/acme/nope", { kind: "unknown" }],
    ["/acme", { kind: "unknown" }],
    ["/", { kind: "unknown" }],
    ["", { kind: "unknown" }],
  ];

  it.each(cases)("parses %s", (url, expected) => {
    expect(parseTabSubject(url)).toEqual(expected);
  });

  it("ignores hash fragments", () => {
    expect(parseTabSubject("/acme/issues/bug-1#comment-3")).toEqual({
      kind: "issue",
      id: "bug-1",
    });
    expect(parseTabSubject("/acme/chat?session=s1#x")).toEqual({
      kind: "chat",
      sessionId: "s1",
    });
  });

  it("keeps sub-route ids at index 2 (issue filters/anchors don't leak)", () => {
    expect(parseTabSubject("/acme/issues/bug-1?comment=c1")).toEqual({
      kind: "issue",
      id: "bug-1",
    });
  });
});

describe("tabSubjectKey", () => {
  it("is stable for the same subject and distinct across selections", () => {
    expect(tabSubjectKey({ kind: "issue", id: "x" })).toBe("issue:x");
    expect(tabSubjectKey({ kind: "chat", sessionId: null })).toBe("chat:");
    expect(tabSubjectKey({ kind: "chat", sessionId: "s1" })).toBe("chat:s1");
    expect(
      tabSubjectKey({ kind: "actor", actorType: "member", id: "m1" }),
    ).toBe("actor:member:m1");
  });

  it("distinguishes archived inbox and the attachment filename", () => {
    expect(tabSubjectKey({ kind: "inbox", selectedKey: "k", archived: false })).toBe(
      "inbox:inbox:k",
    );
    expect(tabSubjectKey({ kind: "inbox", selectedKey: "k", archived: true })).toBe(
      "inbox:archived:k",
    );
    expect(
      tabSubjectKey({ kind: "attachment", id: "a1", filename: "x.pdf" }),
    ).toBe("attachment:a1:x.pdf");
  });
});
