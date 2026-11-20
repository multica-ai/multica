// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import {
  DEFAULT_CONTEXT_USAGE_SETTINGS,
  useContextUsageSettingsStore,
} from "./context-usage-store";

afterEach(() => {
  useContextUsageSettingsStore.setState(DEFAULT_CONTEXT_USAGE_SETTINGS);
});

describe("context usage settings", () => {
  it("ships off, with the issue row on underneath", () => {
    // The pair is read together: nothing shows until `enabled`, and the second
    // level only narrows where it shows once it is on.
    expect(DEFAULT_CONTEXT_USAGE_SETTINGS).toEqual({ enabled: false, showOnIssueRow: true });
    const state = useContextUsageSettingsStore.getState();
    expect(state.enabled).toBe(false);
    expect(state.showOnIssueRow).toBe(true);
  });

  it("patches one switch without disturbing the other", () => {
    useContextUsageSettingsStore.getState().set({ enabled: true });
    expect(useContextUsageSettingsStore.getState()).toMatchObject({
      enabled: true,
      showOnIssueRow: true,
    });

    useContextUsageSettingsStore.getState().set({ showOnIssueRow: false });
    expect(useContextUsageSettingsStore.getState()).toMatchObject({
      enabled: true,
      showOnIssueRow: false,
    });
  });

  it("falls back to the defaults for anything a stored copy is missing", () => {
    // An installed client that predates the second switch has only the first.
    const merged = useContextUsageSettingsStore.persist.getOptions().merge?.(
      { enabled: true },
      useContextUsageSettingsStore.getState(),
    );
    expect(merged).toMatchObject({ enabled: true, showOnIssueRow: true });

    const empty = useContextUsageSettingsStore.persist.getOptions().merge?.(
      undefined,
      useContextUsageSettingsStore.getState(),
    );
    expect(empty).toMatchObject(DEFAULT_CONTEXT_USAGE_SETTINGS);
  });
});
