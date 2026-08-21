import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { IssueStatusEntry } from "@multica/core/types";
import en from "../../locales/en/issues.json";
import { useStatusOptions } from "./status-options";
import { useStatusLabel } from "./status-label";

// The catalog itself is server state; this suite is about what the picker and
// the filter are allowed to offer, so the catalog is fed in directly.
let catalogEntries: IssueStatusEntry[] | undefined;

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog(catalogEntries),
}));

// The REAL en locale, not a stub: the whole point of useStatusLabel is that a
// built-in resolves through i18n rather than through the catalog's
// server-seeded English name, and a stub would not prove the difference.
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (accessor: (dict: unknown) => string) => accessor(en),
  }),
}));

function entry(overrides: Partial<IssueStatusEntry>): IssueStatusEntry {
  return {
    id: overrides.key ?? "id",
    workspace_id: "workspace-1",
    key: "custom",
    name: "Custom",
    description: "",
    category: "in_review",
    color: "#ff0000",
    is_system: false,
    position: 1,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

const BUILT_INS: IssueStatusEntry[] = (
  ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"] as const
).map((key) =>
  entry({
    id: key,
    key,
    // Seeded in English by the server on purpose — a label resolved from here
    // instead of i18n is the regression this fixture exists to catch.
    name: key === "in_progress" ? "In Progress" : key,
    category: key,
    is_system: true,
    position: 0,
  }),
);

describe("useStatusOptions", () => {
  // A cold render must offer the same 7 statuses it always did, or the picker
  // opens empty on first paint and on any workspace whose catalog fetch failed.
  it("offers the 7 built-ins when the catalog has not loaded", () => {
    catalogEntries = undefined;
    const { result } = renderHook(() => useStatusOptions("workspace-1"));

    expect(result.current.map((o) => o.key)).toEqual([
      "backlog",
      "todo",
      "in_progress",
      "in_review",
      "done",
      "blocked",
      "cancelled",
    ]);
  });

  // One flat list, never nested by category (MUL-6399): a custom status sits
  // directly after the built-in of the category it behaves as, so the whole
  // catalog reads top to bottom in canonical order.
  it("places a custom status inline, after the built-in of its category", () => {
    catalogEntries = [...BUILT_INS, entry({ key: "qa", name: "QA", category: "in_review" })];
    const { result } = renderHook(() => useStatusOptions("workspace-1"));

    expect(result.current.map((o) => o.key)).toEqual([
      "backlog",
      "todo",
      "in_progress",
      "in_review",
      "qa",
      "done",
      "blocked",
      "cancelled",
    ]);
  });

  // The category is what the row's icon and hover color are drawn from, so it
  // travels with the option now that no heading states it.
  it("carries the category a custom status behaves as", () => {
    catalogEntries = [...BUILT_INS, entry({ key: "qa", name: "QA", category: "in_review" })];
    const { result } = renderHook(() => useStatusOptions("workspace-1"));

    expect(result.current.find((o) => o.key === "qa")?.category).toBe("in_review");
  });

  // Archiving retires a status from FUTURE assignment. Offering it here would
  // hand the user a value the server rejects.
  it("does not offer an archived status", () => {
    catalogEntries = [
      ...BUILT_INS,
      entry({ key: "qa", name: "QA", archived_at: "2026-01-01T00:00:00Z" }),
    ];
    const { result } = renderHook(() => useStatusOptions("workspace-1"));

    expect(result.current.map((o) => o.key)).not.toContain("qa");
  });

  // Built-ins keep their semantic token color; a hex here would override it.
  it("carries a color for custom statuses only", () => {
    catalogEntries = [...BUILT_INS, entry({ key: "qa", name: "QA", color: "#ff0000" })];
    const { result } = renderHook(() => useStatusOptions("workspace-1"));

    const byKey = new Map(result.current.map((o) => [o.key, o.color]));
    expect(byKey.get("qa")).toBe("#ff0000");
    expect(byKey.get("in_review")).toBeNull();
  });
});

describe("useStatusLabel", () => {
  // The server seeds built-in names in English. Reading them back verbatim
  // would show "In Progress" to every workspace that has picked another
  // language, silently undoing the whole i18n layer for status names.
  it("labels a built-in from i18n, not from the catalog's seeded name", () => {
    catalogEntries = BUILT_INS.map((e) =>
      e.key === "in_progress" ? { ...e, name: "SEEDED-ENGLISH" } : e,
    );
    const { result } = renderHook(() => useStatusLabel("workspace-1"));

    expect(result.current("in_progress")).toBe(en.status.in_progress);
  });

  it("labels a custom status from the catalog", () => {
    catalogEntries = [...BUILT_INS, entry({ key: "qa", name: "QA Review" })];
    const { result } = renderHook(() => useStatusLabel("workspace-1"));

    expect(result.current("qa")).toBe("QA Review");
  });

  // A status created seconds ago in another session, or one since deleted.
  // Showing the raw key beats showing nothing.
  it("falls back to the raw key for a status the catalog does not know", () => {
    catalogEntries = BUILT_INS;
    const { result } = renderHook(() => useStatusLabel("workspace-1"));

    expect(result.current("never-seen")).toBe("never-seen");
  });
});
