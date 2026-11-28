import { describe, expect, it } from "vitest";

import {
  dashboardAgentRunTimeOptions,
  dashboardFailuresByAgentOptions,
  dashboardFailuresDailyOptions,
  dashboardModelRunTimeOptions,
  dashboardRunTimeDailyOptions,
  dashboardRuntimeDurationOptions,
  dashboardUsageByAgentOptions,
  dashboardUsageByRuntimeOptions,
  dashboardUsageDailyOptions,
} from "./queries";

type QueryOptionsWithPlaceholder = {
  queryKey: readonly unknown[];
  placeholderData?: unknown;
};

function resolvePlaceholder(
  options: QueryOptionsWithPlaceholder,
  previousData: unknown,
  previousKey: readonly unknown[] | undefined,
) {
  expect(options.placeholderData).toBeTypeOf("function");
  return (
    options.placeholderData as (
      data: unknown,
      query: { queryKey: readonly unknown[] } | undefined,
    ) => unknown
  )(
    previousData,
    previousKey ? { queryKey: previousKey } : undefined,
  );
}

const optionBuilders = [
  dashboardUsageDailyOptions,
  dashboardUsageByAgentOptions,
  dashboardAgentRunTimeOptions,
  dashboardRunTimeDailyOptions,
  dashboardFailuresDailyOptions,
  dashboardFailuresByAgentOptions,
  dashboardUsageByRuntimeOptions,
  dashboardRuntimeDurationOptions,
  dashboardModelRunTimeOptions,
] as const;

describe("dashboard range placeholders", () => {
  it.each(optionBuilders)(
    "keeps the previous %s result when only the day range changes",
    (buildOptions) => {
      const previous = [{ sentinel: "30d" }];
      const previousOptions = buildOptions("ws-1", 30, null, "UTC", null);
      const nextOptions = buildOptions("ws-1", 7, null, "UTC", null);

      expect(
        resolvePlaceholder(nextOptions, previous, previousOptions.queryKey),
      ).toBe(previous);
    },
  );

  it("does not carry placeholder data across workspace, project, timezone, or history-limit scopes", () => {
    const previous = [{ sentinel: "previous-scope" }];
    const nextOptions = dashboardUsageDailyOptions(
      "ws-1",
      7,
      "project-1",
      "Asia/Shanghai",
      null,
    );
    const previousScopes = [
      dashboardUsageDailyOptions(
        "ws-2",
        30,
        "project-1",
        "Asia/Shanghai",
        null,
      ),
      dashboardUsageDailyOptions("ws-1", 30, "project-2", "Asia/Shanghai", null),
      dashboardUsageDailyOptions("ws-1", 30, "project-1", "UTC", null),
      // A tightened plan limit: the 90-day result is outside the new plan.
      dashboardUsageDailyOptions("ws-1", 30, "project-1", "Asia/Shanghai", 30),
    ];

    for (const previousOptions of previousScopes) {
      expect(
        resolvePlaceholder(nextOptions, previous, previousOptions.queryKey),
      ).toBeUndefined();
    }
  });

  it("keeps the initial loading state honest when there is no previous query", () => {
    const options = dashboardUsageDailyOptions("ws-1", 30, null, "UTC", null);

    expect(resolvePlaceholder(options, undefined, undefined)).toBeUndefined();
  });
});

describe("dashboard history limit in queries", () => {
  it.each(optionBuilders)("waits for the limit and keys %s on it", (buildOptions) => {
    expect(buildOptions("ws-1", 30, null, "UTC", undefined).enabled).toBe(false);
    expect(buildOptions("ws-1", 30, null, "UTC", null).enabled).toBe(true);
    expect(buildOptions("ws-1", 30, null, "UTC", 30).queryKey).not.toEqual(
      buildOptions("ws-1", 30, null, "UTC", 90).queryKey,
    );
  });
});

describe("leaderboard Model and Runtime view queries", () => {
  const viewBuilders = [
    dashboardUsageByRuntimeOptions,
    dashboardRuntimeDurationOptions,
    dashboardModelRunTimeOptions,
  ] as const;

  it.each(viewBuilders)("keys %s on the history limit and the squad", (buildOptions) => {
    // days 7 and limit 30, so the limit segment cannot be the day range.
    const key = buildOptions("ws-1", 7, null, "UTC", 30, "squad-1").queryKey;
    expect(key[6]).toBe(30);
    expect(key[7]).toBe("squad-1");
    expect(key).not.toEqual(
      buildOptions("ws-1", 7, null, "UTC", 30, "squad-2").queryKey,
    );
    expect(key).not.toEqual(buildOptions("ws-1", 7, null, "UTC", 30).queryKey);
  });

  it("gives each view endpoint its own key", () => {
    const keys = viewBuilders.map(
      (buildOptions) => buildOptions("ws-1", 30, null, "UTC", null).queryKey,
    );
    expect(new Set(keys.map((key) => JSON.stringify(key))).size).toBe(3);
  });
});
