// @vitest-environment node

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  browserTimezone,
  resetBrowserTimezoneCache,
  timezoneOptions,
} from "./timezone-select";

type IntlWithSupportedValues = typeof Intl & {
  supportedValuesOf?: (key: "timeZone") => string[];
};

const intlWithSupportedValues = Intl as IntlWithSupportedValues;
const realSupportedValuesOf = intlWithSupportedValues.supportedValuesOf;

beforeEach(() => {
  resetBrowserTimezoneCache();
});

afterEach(() => {
  intlWithSupportedValues.supportedValuesOf = realSupportedValuesOf;
  vi.restoreAllMocks();
  resetBrowserTimezoneCache();
});

describe("timezoneOptions", () => {
  it("uses the canonical Kyiv identifier for legacy Kiev runtime values", () => {
    intlWithSupportedValues.supportedValuesOf = () => [
      "Europe/Kiev",
      "Europe/Kyiv",
      "Asia/Tokyo",
    ];
    vi.spyOn(Intl, "DateTimeFormat").mockImplementation(
      () =>
        ({
          resolvedOptions: () => ({ timeZone: "Europe/Kiev" }),
        }) as Intl.DateTimeFormat,
    );

    const options = timezoneOptions("Asia/Shanghai");

    expect(options).not.toContain("Europe/Kiev");
    expect(options.filter((timezone) => timezone === "Europe/Kyiv")).toHaveLength(1);
    expect(options).toContain("Asia/Tokyo");
    expect(browserTimezone()).toBe("Europe/Kyiv");
  });

  it("keeps a legacy current value selectable without adding a duplicate alias", () => {
    intlWithSupportedValues.supportedValuesOf = () => ["Europe/Kiev", "Europe/Kyiv"];

    const options = timezoneOptions("Europe/Kiev");

    expect(options[0]).toBe("Europe/Kiev");
    expect(options).not.toContain("Europe/Kyiv");
  });
});
