// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enIssues from "../locales/en/issues.json";
import { ActorIssuesPanel } from "./actor-issues-panel";

// The gutter is overridden with a sentinel rather than compared against the
// real constant. `PAGE_GUTTER` is `px-4` today, so asserting the real value
// would pass just as happily against a hand-spelled `px-4` — the very thing
// this guards. Only a component that reads the constant picks the sentinel up.
const SENTINEL_GUTTER = "px-[13px]";
vi.mock("../layout/page-header", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../layout/page-header")>()),
  PAGE_GUTTER: "px-[13px]",
}));

// MUL-7107: this toolbar is the leading edge the agent detail tab bar has to
// line up with, but it lives here rather than in the pane, and the pane's own
// suite mocks this component out. Without an assertion here the shared gutter
// is unasserted anywhere, which is how the toolbar came to spell its own
// `px-4` and stayed aligned only by coincidence.
//
// IssueSurface is stubbed down to its renderHeader slot so the real
// ActorIssuesHeader renders without the query/api layer; the toolbar's own
// heavy children are stubbed for the same reason.
vi.mock("../issues/surface/issue-surface", () => ({
  IssueSurface: ({
    renderHeader,
  }: {
    renderHeader?: (ctx: { controller: unknown }) => React.ReactNode;
  }) => (
    <div>
      {renderHeader?.({
        controller: {
          surfaceIssues: [],
          isRefreshing: false,
          facetCountsExact: true,
          tableFacetCounts: undefined,
          setActiveTableFacet: vi.fn(),
        },
      })}
    </div>
  ),
}));
vi.mock("../issues/components/issues-header", () => ({
  IssueDisplayControls: () => <div>display-controls</div>,
  ViewRefreshIndicator: () => <div>refresh-indicator</div>,
}));
vi.mock("../issues/components/filter-chips-bar", () => ({
  FilterChipsBar: () => <div>filter-chips-bar</div>,
}));

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

describe("ActorIssuesPanel toolbar alignment", () => {
  it("reads the shared page gutter instead of spelling its own", () => {
    const { container } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ActorIssuesPanel actorType="agent" actorId="agent-1" />
      </I18nProvider>,
    );

    const toolbar = container.querySelector("div.border-b");
    expect(toolbar).toBeTruthy();
    expect(toolbar).toHaveClass(SENTINEL_GUTTER);
  });
});
