// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  TimezoneSelect,
  canonicalizeTimezone,
  resetBrowserTimezoneCache,
  timezoneOptions,
} from "./timezone-select";

function renderTimezoneSelect(
  value: string,
  props: Partial<{ browserSuffix: string }> = {},
) {
  render(
    <TimezoneSelect
      value={value}
      onValueChange={() => {}}
      browserSuffix={props.browserSuffix ?? ""}
    />,
  );
}

function stubResolvedTimeZone(tz: string) {
  const real = Intl.DateTimeFormat;
  const Impl = function (
    this: unknown,
    locales?: string | string[],
    options?: Intl.DateTimeFormatOptions,
  ) {
    if (!options || Object.keys(options).length === 0) {
      return {
        resolvedOptions: () => ({ timeZone: tz }),
      } as unknown as Intl.DateTimeFormat;
    }
    return new real(locales, options);
  } as unknown as typeof Intl.DateTimeFormat;
  Object.defineProperty(Intl, "DateTimeFormat", {
    value: Impl,
    configurable: true,
    writable: true,
  });
}

const realSupportedValuesOf = Intl.supportedValuesOf;
const realDateTimeFormat = Intl.DateTimeFormat;

function stubSupported(values: string[] | undefined) {
  Object.defineProperty(Intl, "supportedValuesOf", {
    value: values
      ? (((_key: "timeZone") => values) as typeof Intl.supportedValuesOf)
      : undefined,
    configurable: true,
    writable: true,
  });
}

function stubDateTimeFormat(throwFor: string[]) {
  const Impl = function (
    this: unknown,
    _locales?: string | string[],
    options?: Intl.DateTimeFormatOptions,
  ) {
    if (options?.timeZone && throwFor.includes(options.timeZone)) {
      throw new RangeError(`Invalid time zone: ${options.timeZone}`);
    }
    return new realDateTimeFormat(undefined, options);
  } as unknown as typeof Intl.DateTimeFormat;
  Object.defineProperty(Intl, "DateTimeFormat", {
    value: Impl,
    configurable: true,
    writable: true,
  });
}

beforeEach(() => {
  resetBrowserTimezoneCache();
});

afterEach(() => {
  Object.defineProperty(Intl, "supportedValuesOf", {
    value: realSupportedValuesOf,
    configurable: true,
    writable: true,
  });
  Object.defineProperty(Intl, "DateTimeFormat", {
    value: realDateTimeFormat,
    configurable: true,
    writable: true,
  });
  vi.restoreAllMocks();
});

describe("timezoneOptions legacy aliases", () => {
  it("maps Europe/Kiev to Europe/Kyiv when only the legacy name is reported", () => {
    stubSupported(["Europe/Kiev", "UTC"]);
    const options = timezoneOptions("UTC");
    expect(options).toContain("Europe/Kyiv");
    expect(options).not.toContain("Europe/Kiev");
  });

  it("collapses both names to a single Europe/Kyiv entry", () => {
    stubSupported(["Europe/Kiev", "Europe/Kyiv", "UTC"]);
    const options = timezoneOptions("UTC");
    expect(options.filter((tz) => tz === "Europe/Kyiv")).toHaveLength(1);
    expect(options).not.toContain("Europe/Kiev");
  });

  it("keeps a stored legacy value selectable without throwing", () => {
    stubSupported(["Europe/Kiev", "UTC"]);
    expect(() => timezoneOptions("Europe/Kiev")).not.toThrow();
  });

  it("renders a stored legacy value as its modern name in the trigger", () => {
    stubSupported(["Europe/Kiev", "UTC"]);
    renderTimezoneSelect("Europe/Kiev");
    expect(screen.getByRole("combobox")).toHaveTextContent("Europe/Kyiv");
    expect(screen.getByRole("combobox")).not.toHaveTextContent("Europe/Kiev");
  });

  it("marks the canonicalized browser zone with the browser suffix", () => {
    stubSupported(["Europe/Kiev", "UTC"]);
    stubResolvedTimeZone("Europe/Kiev");
    resetBrowserTimezoneCache();
    renderTimezoneSelect("Europe/Kiev", { browserSuffix: " (browser)" });
    expect(screen.getByRole("combobox")).toHaveTextContent(
      "Europe/Kyiv (browser)",
    );
  });

  it("keeps the legacy name on an old ICU build that rejects the modern name", () => {
    stubSupported(["Europe/Kiev", "UTC"]);
    stubDateTimeFormat(["Europe/Kyiv"]);
    const options = timezoneOptions("UTC");
    expect(options).toContain("Europe/Kiev");
    expect(options).not.toContain("Europe/Kyiv");
  });

  it("falls back to COMMON_TIMEZONES when supportedValuesOf is missing", () => {
    stubSupported(undefined);
    const options = timezoneOptions("UTC");
    expect(options).toContain("UTC");
  });
});

describe("canonicalizeTimezone", () => {
  it("returns an unmapped identifier unchanged", () => {
    expect(canonicalizeTimezone("Asia/Tokyo")).toBe("Asia/Tokyo");
  });

  it("maps a known legacy identifier to its modern name", () => {
    expect(canonicalizeTimezone("Europe/Kiev")).toBe("Europe/Kyiv");
  });
});
