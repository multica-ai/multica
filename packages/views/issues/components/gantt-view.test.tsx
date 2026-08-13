import { createStore } from "zustand/vanilla";
import { describe, expect, it, vi, beforeAll, afterAll } from "vitest";
import { screen } from "@testing-library/react";
import {
  type IssueViewState,
  viewStoreSlice,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueContextMenuProvider } from "../actions";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

// The row's start / due range lives in a tooltip. This test is about how the
// dates are formatted, not about hover timing, so render the content inline.
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => children,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => render,
  TooltipContent: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("../../navigation", () => ({
  AppLink: ({
    children,
    href,
  }: {
    children: React.ReactNode;
    href: string;
  }) => <a href={href}>{children}</a>,
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues" }),
  resolveClickIntent: () => "push",
  useIntentNavigate: () => () => {},
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspaceSlug: () => "acme",
    useRequiredWorkspaceSlug: () => "acme",
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});

import { GanttView } from "./gantt-view";

const ISSUE = {
  id: "issue-1",
  identifier: "MUL-1",
  number: 1,
  title: "Ship the thing",
  description: "",
  status: "todo",
  priority: "medium",
  workspace_id: "workspace-1",
  project_id: null,
  parent_issue_id: null,
  assignee_id: null,
  assignee_type: null,
  creator_id: "user-1",
  creator_type: "member",
  labels: [],
  position: 0,
  stage: null,
  start_date: "2026-03-02",
  due_date: "2026-03-06",
  created_at: "2026-03-01T00:00:00Z",
  updated_at: "2026-03-01T00:00:00Z",
} as unknown as Issue;

function renderGantt(locale: "en" | "zh-Hans") {
  const store = createStore<IssueViewState>()(viewStoreSlice);
  return renderWithI18n(
    <ViewStoreProvider store={store}>
      <IssueContextMenuProvider>
        <GanttView issues={[ISSUE]} />
      </IssueContextMenuProvider>
    </ViewStoreProvider>,
    { locale },
  );
}

describe("GanttView date localization", () => {
  beforeAll(() => {
    // The browser speaks English while the UI does not. Dates must follow the
    // UI language, not this — see useLocale in packages/views/i18n.
    vi.stubGlobal("navigator", {
      ...globalThis.navigator,
      language: "en-US",
      languages: ["en-US"],
    });
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-03-04T12:00:00Z"));
  });

  afterAll(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("formats axis dates in the UI language, not the browser's", () => {
    const { container, unmount } = renderGantt("zh-Hans");

    expect(screen.getByText("2026年3月")).toBeTruthy();
    // Nothing may fall back to the browser language.
    expect(container.textContent ?? "").not.toMatch(/Mar\b/);

    unmount();
  });

  it("formats the row's start / due range in the UI language", () => {
    const { unmount } = renderGantt("zh-Hans");

    const range = screen.getByText(/2026年3月2日/);
    expect(range.textContent).toContain("2026年3月6日");

    unmount();
  });

  it("still formats in English when the UI is English", () => {
    const { container } = renderGantt("en");

    expect(screen.getByText("Mar 2026")).toBeTruthy();
    expect(container.textContent ?? "").not.toMatch(/年/);
  });
});
