import { beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_MANUAL_CREATE_FIELDS,
  DEFAULT_QUICK_CREATE_FIELDS,
  useIssueCreateSettingsStore,
} from "./issue-create-settings-store";

describe("issue create settings store", () => {
  beforeEach(() => {
    useIssueCreateSettingsStore.setState({
      quickCreateFields: DEFAULT_QUICK_CREATE_FIELDS,
      manualCreateFields: DEFAULT_MANUAL_CREATE_FIELDS,
      hiddenManualPropertyIds: [],
    });
  });

  it("defaults to project-only quick create and the classic manual toolbar", () => {
    expect(useIssueCreateSettingsStore.getState().quickCreateFields).toEqual(["project"]);
    expect(useIssueCreateSettingsStore.getState().manualCreateFields).toEqual([
      "status",
      "priority",
      "assignee",
      "project",
      "due_date",
    ]);
  });

  it("keeps quick create fields in canonical order regardless of toggle sequence", () => {
    const { setQuickCreateFieldVisible } = useIssueCreateSettingsStore.getState();

    setQuickCreateFieldVisible("due_date", true);
    setQuickCreateFieldVisible("priority", true);

    expect(useIssueCreateSettingsStore.getState().quickCreateFields).toEqual([
      "project",
      "priority",
      "due_date",
    ]);

    setQuickCreateFieldVisible("project", false);
    expect(useIssueCreateSettingsStore.getState().quickCreateFields).toEqual([
      "priority",
      "due_date",
    ]);
  });

  it("toggles manual create fields independently of quick create", () => {
    const { setManualCreateFieldVisible } = useIssueCreateSettingsStore.getState();

    setManualCreateFieldVisible("due_date", false);
    setManualCreateFieldVisible("start_date", true);

    expect(useIssueCreateSettingsStore.getState().manualCreateFields).toEqual([
      "status",
      "priority",
      "assignee",
      "project",
      "start_date",
    ]);
    expect(useIssueCreateSettingsStore.getState().quickCreateFields).toEqual(["project"]);
  });

  it("hides and restores custom properties without changing static field settings", () => {
    const { setManualPropertyVisible } = useIssueCreateSettingsStore.getState();

    setManualPropertyVisible("property-business-value", false);
    setManualPropertyVisible("property-effort", false);

    expect(useIssueCreateSettingsStore.getState().hiddenManualPropertyIds).toEqual([
      "property-business-value",
      "property-effort",
    ]);

    setManualPropertyVisible("property-business-value", true);

    expect(useIssueCreateSettingsStore.getState().hiddenManualPropertyIds).toEqual([
      "property-effort",
    ]);
    expect(useIssueCreateSettingsStore.getState().manualCreateFields).toEqual(
      DEFAULT_MANUAL_CREATE_FIELDS,
    );
  });
});
