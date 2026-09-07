// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue, IssueStatusEntry } from "@multica/core/types";
import {
  BOARD_CATEGORIES,
  CLOSED_CATEGORIES,
  buildIssueStatusCatalog,
  isCustomStatus,
  issueBehavesAs,
  issueBehavesAsAny,
  issueColumnCategory,
  issueStatusColor,
  statusOptions,
} from "./issue-status";

function entry(
  key: string,
  category: string,
  overrides: Partial<IssueStatusEntry> = {},
): IssueStatusEntry {
  return {
    id: key,
    workspace_id: "ws-1",
    key,
    name: key,
    description: "",
    category: category as IssueStatusEntry["category"],
    color: "#123456",
    is_system: false,
    position: 0,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function issue(overrides: Partial<Issue>): Pick<Issue, "status" | "status_category"> {
  return { status: "todo", ...overrides } as Pick<Issue, "status" | "status_category">;
}

describe("issueColumnCategory", () => {
  // Grouping must not wait on the catalog, so it reads the category the server
  // already resolved onto the payload.
  it("prefers the server-resolved category", () => {
    expect(
      issueColumnCategory(issue({ status: "human_review", status_category: "in_review" })),
    ).toBe("in_review");
  });

  it("falls back to the key when it is a built-in", () => {
    expect(issueColumnCategory(issue({ status: "blocked" }))).toBe("blocked");
  });

  // An unresolvable custom key lands SOMEWHERE rather than nowhere: a row in a
  // possibly-wrong section is recoverable, a row in no section is invisible —
  // which is the bug this file exists to prevent (MUL-6457).
  it("never leaves an unresolved custom status without a section", () => {
    expect(issueColumnCategory(issue({ status: "qa" }))).toBe("todo");
  });

  it("ignores a category value this build does not know", () => {
    expect(
      issueColumnCategory(issue({ status: "qa", status_category: "started" as never })),
    ).toBe("todo");
  });

  it("puts every built-in in a board section except cancelled", () => {
    for (const key of ["backlog", "todo", "in_progress", "in_review", "done", "blocked"]) {
      expect(BOARD_CATEGORIES).toContain(issueColumnCategory(issue({ status: key })));
    }
    expect(BOARD_CATEGORIES).not.toContain(issueColumnCategory(issue({ status: "cancelled" })));
  });
});

describe("buildIssueStatusCatalog", () => {
  // A status has to render on the very first paint. Built-in keys are their own
  // category, so an unloaded catalog still resolves all 7 — which is what keeps
  // a workspace with no custom statuses identical before the request lands.
  it("resolves every built-in with no catalog loaded", () => {
    const c = buildIssueStatusCatalog(undefined);
    expect(c.isLoaded).toBe(false);
    for (const key of ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"]) {
      expect(c.categoryOf(key)).toBe(key);
    }
    expect(c.labelOf("in_review")).toBe("In Review");
    expect(c.colorOf("in_review")).toBeNull();
  });

  it("maps a custom status to its category and name", () => {
    const c = buildIssueStatusCatalog([entry("human_review", "in_review", { name: "Human Review" })]);
    expect(c.categoryOf("human_review")).toBe("in_review");
    expect(c.labelOf("human_review")).toBe("Human Review");
    expect(c.colorOf("human_review")).toBe("#123456");
  });

  // An issue can carry a status created moments ago in another session. Naming
  // it by its raw key beats rendering blank.
  it("falls back for a status the catalog does not know", () => {
    const c = buildIssueStatusCatalog([]);
    expect(c.categoryOf("ghost")).toBe("todo");
    expect(c.labelOf("ghost")).toBe("ghost");
    expect(c.entryOf("ghost")).toBeUndefined();
  });

  // The 7 built-ins carry a server-seeded name and hex that nobody can edit.
  // Mobile owns their copy and paints them from their category token, so
  // reading the catalog row for either would drift from every other surface.
  it("keeps mobile's own copy and token colour for a built-in", () => {
    const builtIn = entry("in_review", "in_review", {
      name: "In Review",
      is_system: true,
      color: "#22c55e",
    });
    const c = buildIssueStatusCatalog([builtIn, entry("qa", "in_review", { name: "QA" })]);
    expect(c.labelOf("in_review")).toBe("In Review");
    expect(c.colorOf("in_review")).toBeNull();
    expect(c.colorOf("qa")).toBe("#123456");
  });

  it("issueStatusColor answers the same question for an entry in hand", () => {
    expect(issueStatusColor(undefined)).toBeNull();
    expect(issueStatusColor(entry("qa", "in_review", { is_system: true }))).toBeNull();
    expect(issueStatusColor(entry("qa", "in_review"))).toBe("#123456");
  });
});

// Archiving retires a status from FUTURE assignment but leaves existing issues
// on it. Those issues must keep their real name, colour and category — dropping
// archived rows from resolution would degrade them to a raw key.
describe("archived statuses stay resolvable", () => {
  const archived = entry("gate_approved", "done", {
    name: "Gate Approved",
    archived_at: "2026-01-01T00:00:00Z",
  });
  const active = entry("human_review", "in_review", { name: "Human Review" });
  const c = buildIssueStatusCatalog([active, archived]);

  it("keeps name and category for an issue left on an archived status", () => {
    expect(c.labelOf("gate_approved")).toBe("Gate Approved");
    expect(c.categoryOf("gate_approved")).toBe("done");
  });

  it("excludes archived from the assignable set", () => {
    expect(c.activeStatuses.map((e) => e.key)).toEqual(["human_review"]);
    expect(c.inCategory("done")).toEqual([]);
  });
});

describe("statusOptions", () => {
  // The picker and the status filter both read this list. A cold render — or a
  // backend that predates the catalog — must still offer all 7 lifecycle steps
  // rather than an empty sheet.
  it("offers the 7 built-ins with no catalog loaded", () => {
    const options = statusOptions(buildIssueStatusCatalog(undefined));
    expect(options.map((o) => o.key)).toEqual([
      "backlog",
      "todo",
      "in_progress",
      "in_review",
      "done",
      "blocked",
      "cancelled",
    ]);
    expect(options.every((o) => o.color === null)).toBe(true);
  });

  // A category's catalog rows REPLACE its built-in fallback, so the built-in
  // must come back through its own is_system row — otherwise turning on custom
  // statuses would silently remove "In Review" from the picker.
  it("keeps the built-in alongside its category's custom statuses", () => {
    const c = buildIssueStatusCatalog([
      entry("in_review", "in_review", { name: "In Review", is_system: true }),
      entry("human_review", "in_review", { name: "Human Review" }),
    ]);
    const inReview = statusOptions(c).filter((o) => o.category === "in_review");
    expect(inReview.map((o) => o.key)).toEqual(["in_review", "human_review"]);
    expect(inReview.map((o) => o.label)).toEqual(["In Review", "Human Review"]);
    expect(inReview.map((o) => o.color)).toEqual([null, "#123456"]);
  });

  it("never offers an archived status", () => {
    const c = buildIssueStatusCatalog([
      entry("done", "done", { name: "Done", is_system: true }),
      entry("gate_approved", "done", { archived_at: "2026-01-01T00:00:00Z" }),
    ]);
    expect(statusOptions(c).map((o) => o.key)).not.toContain("gate_approved");
  });
});

describe("issueBehavesAs", () => {
  // The regression: the mention bar dimmed closed issues with
  // `status === "done"`, so an issue on a custom status in the done category
  // rendered at full opacity as though the work were still open. A custom
  // status inherits its category's behavior in full. (MUL-6243)
  it("treats a custom status in the done category as done", () => {
    const shipped = issue({ status: "shipped", status_category: "done" });
    expect(issueBehavesAs(shipped, "done")).toBe(true);
    expect(issueBehavesAsAny(shipped, CLOSED_CATEGORIES)).toBe(true);
  });

  it("treats a custom status in the cancelled category as closed", () => {
    expect(
      issueBehavesAsAny(issue({ status: "wont_do", status_category: "cancelled" }), CLOSED_CATEGORIES),
    ).toBe(true);
  });

  it("still answers for the built-ins", () => {
    expect(issueBehavesAsAny(issue({ status: "done" }), CLOSED_CATEGORIES)).toBe(true);
    expect(issueBehavesAsAny(issue({ status: "cancelled" }), CLOSED_CATEGORIES)).toBe(true);
    expect(issueBehavesAsAny(issue({ status: "in_review" }), CLOSED_CATEGORIES)).toBe(false);
  });

  // An unresolved custom key fails SAFE: false keeps the row at full opacity
  // rather than dimming work that may well still be open.
  it("says no for a status it cannot resolve", () => {
    expect(issueBehavesAsAny(issue({ status: "qa" }), CLOSED_CATEGORIES)).toBe(false);
  });
});

describe("isCustomStatus", () => {
  // Drives the row chip: a section header already names the category, so the
  // chip must speak only when the status adds something the header does not.
  it("is true for a custom status the catalog knows", () => {
    const c = buildIssueStatusCatalog([entry("qa", "in_review", { name: "QA" })]);
    expect(isCustomStatus(c, "qa")).toBe(true);
  });

  it("stays silent for a built-in", () => {
    const c = buildIssueStatusCatalog([
      entry("in_review", "in_review", { name: "In Review", is_system: true }),
    ]);
    expect(isCustomStatus(c, "in_review")).toBe(false);
  });

  // Backstop for a server that omits `is_system` — the schema defaults it to
  // false, and a built-in must stay silent either way.
  it("stays silent for a built-in row missing is_system", () => {
    const c = buildIssueStatusCatalog([entry("done", "done", { name: "Done" })]);
    expect(isCustomStatus(c, "done")).toBe(false);
  });

  // No catalog means no name to show, so the row renders exactly as it did
  // before the catalog existed rather than flashing a chip.
  it("stays silent when the catalog has not landed", () => {
    expect(isCustomStatus(buildIssueStatusCatalog(undefined), "qa")).toBe(false);
    expect(isCustomStatus(buildIssueStatusCatalog([]), "qa")).toBe(false);
  });
});
