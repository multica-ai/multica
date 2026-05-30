import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createStore } from "zustand/vanilla";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  useIssueViewStore,
  viewStoreSlice,
  type IssueViewState,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { IssuesHeader } from "./issues-header";

vi.mock("@multica/core/issues/stores/issues-scope-store", () => ({
  useIssuesScopeStore: (selector: any) =>
    selector({ scope: "all", setScope: vi.fn() }),
}));

vi.mock("./workspace-agent-working-chip", () => ({
  WorkspaceAgentWorkingChip: ({
    value,
    onToggle,
  }: {
    value: boolean;
    onToggle: () => void;
  }) => (
    <button
      type="button"
      data-active={String(value)}
      data-testid="working-chip"
      onClick={onToggle}
    >
      Working
    </button>
  ),
}));

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

function createTestViewStore() {
  return createStore<IssueViewState>()((set) => viewStoreSlice(set));
}

function renderHeader(store = createTestViewStore()) {
  return {
    store,
    user: userEvent.setup(),
    ...render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ViewStoreProvider store={store}>
          <IssuesHeader scopedIssues={[]} />
        </ViewStoreProvider>
      </I18nProvider>,
    ),
  };
}

describe("IssuesHeader", () => {
  it("toggles the working filter on the current view store", async () => {
    const store = createTestViewStore();
    store.setState({ agentRunningFilter: false });
    useIssueViewStore.setState({ agentRunningFilter: false });

    const { user } = renderHeader(store);

    expect(screen.getByTestId("working-chip")).toHaveAttribute(
      "data-active",
      "false",
    );

    await user.click(screen.getByTestId("working-chip"));

    expect(store.getState().agentRunningFilter).toBe(true);
    expect(useIssueViewStore.getState().agentRunningFilter).toBe(false);
    expect(screen.getByTestId("working-chip")).toHaveAttribute(
      "data-active",
      "true",
    );
  });
});
