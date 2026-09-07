// @vitest-environment jsdom

import { cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { IssueStatusEntry } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";
import { StatusPicker } from "./status-picker";

// The catalog is server state; this suite is about what the picker PAINTS with
// it, so the entries are fed in directly. The color matrix behind `colorOf`
// lives in packages/core/issue-statuses/queries.test.ts — this file only proves
// the trigger and the list read it from the same place.
let catalogEntries: IssueStatusEntry[] | undefined;

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog(catalogEntries),
}));

function entry(overrides: Partial<IssueStatusEntry>): IssueStatusEntry {
  return {
    id: overrides.key ?? "id",
    workspace_id: "workspace-1",
    key: "custom",
    name: "Custom",
    description: "",
    category: "in_review",
    // Seeded per status by the server — including for the built-ins, which
    // are not recolorable and must ignore it.
    color: "#22c55e",
    is_system: false,
    position: 1,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

const IN_REVIEW = entry({
  id: "in_review",
  key: "in_review",
  name: "In Review",
  is_system: true,
  position: 0,
});

const QA = entry({ id: "qa", key: "qa", name: "QA", color: "#ec7a2d" });

/** StatusIcon's own glyph, told apart from the lucide check mark beside it. */
const STATUS_ICON = 'svg[viewBox="0 0 14 14"]';

function iconOf(row: Element | null): SVGElement | null {
  return row?.querySelector<SVGElement>(STATUS_ICON) ?? null;
}

/** The list row for one status — the trigger is a button carrying the same
 *  label, so the picker-item marker is what tells the two apart. */
function optionRow(label: string): HTMLElement {
  const row = Array.from(
    document.querySelectorAll<HTMLElement>("button[data-picker-item]"),
  ).find((el) => el.textContent?.trim() === label);
  if (!row) throw new Error(`no picker row for "${label}"`);
  return row;
}

afterEach(() => {
  cleanup();
  catalogEntries = undefined;
});

describe("StatusPicker trigger color", () => {
  // The bug: the trigger read the catalog entry's raw color while the list read
  // the resolved one, so a built-in rendered as the server's seeded #22c55e in
  // one and as the `text-success` token in the other — the same status in two
  // visibly different greens, side by side. (MUL-6440)
  it("paints a built-in from the token, exactly like its row in the list", () => {
    catalogEntries = [IN_REVIEW, QA];
    const { container } = renderWithI18n(
      <StatusPicker status="in_review" onUpdate={() => {}} open onOpenChange={() => {}} />,
    );

    const trigger = iconOf(container);
    const row = iconOf(optionRow("In Review"));

    // No inline color on either: an inline color is precisely what overrides
    // the token and produces the two-greens mismatch.
    expect(trigger?.getAttribute("style")).toBeNull();
    expect(row?.getAttribute("style")).toBeNull();
    expect(trigger?.getAttribute("class")).toContain("text-success");
    expect(row?.getAttribute("class")).toContain("text-success");
  });

  // The other half of the same rule: a CUSTOM status has no token to fall back
  // on, so its own color has to reach both controls.
  it("paints a custom status from its own color in both places", () => {
    catalogEntries = [IN_REVIEW, QA];
    const { container } = renderWithI18n(
      <StatusPicker status="qa" onUpdate={() => {}} open onOpenChange={() => {}} />,
    );

    const trigger = iconOf(container);
    const row = iconOf(optionRow("QA"));

    expect(trigger?.style.color).toBe("rgb(236, 122, 45)");
    expect(row?.style.color).toBe(trigger?.style.color);
  });
});
