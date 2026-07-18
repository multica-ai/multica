import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  DEFAULT_MANUAL_CREATE_FIELDS,
  DEFAULT_QUICK_CREATE_FIELDS,
  useIssueCreateSettingsStore,
} from "@multica/core/issues/stores/issue-create-settings-store";
import { renderWithI18n } from "../../test/i18n";
import { IssueTab } from "./issue-tab";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/properties", () => ({ propertyListOptions: () => ({ queryKey: ["properties"] }) }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [{ id: "value-id", name: "Business value (DKK)", type: "number", icon: "", archived: false }] }),
}));

function resetStore() {
  useIssueCreateSettingsStore.setState({
    quickCreateFields: DEFAULT_QUICK_CREATE_FIELDS,
    manualCreateFields: DEFAULT_MANUAL_CREATE_FIELDS,
    hiddenManualPropertyIds: [],
  });
}

describe("IssueTab", () => {
  beforeEach(resetStore);
  afterEach(() => { cleanup(); resetStore(); });

  it("renders standard and custom field visibility", () => {
    renderWithI18n(<IssueTab />);
    expect(screen.getAllByRole("switch")).toHaveLength(10);
    expect(screen.getByRole("switch", { name: "Business value (DKK)" })).toBeChecked();
  });

  it("persists hiding a custom property without changing standard fields", async () => {
    const user = userEvent.setup();
    renderWithI18n(<IssueTab />);
    await user.click(screen.getByRole("switch", { name: "Business value (DKK)" }));
    expect(useIssueCreateSettingsStore.getState().hiddenManualPropertyIds).toEqual(["value-id"]);
    expect(useIssueCreateSettingsStore.getState().manualCreateFields).toEqual(DEFAULT_MANUAL_CREATE_FIELDS);
  });
});
