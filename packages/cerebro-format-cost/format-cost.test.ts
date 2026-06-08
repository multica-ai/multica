import { describe, expect, it } from "vitest";
import {
  DEFAULT_DISPLAY_CURRENCY,
  formatCostCents,
  formatCostUsd,
  formatCostWithOriginal,
  isSupportedCurrency,
  normalizeCurrency,
} from "./index";

// Independent reference formatter, so the conversion-math assertions don't
// depend on hardcoded locale glyphs/separators that vary by ICU version.
const ref = (amount: number, currency: string, locale: string) =>
  new Intl.NumberFormat(locale, {
    style: "currency",
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(amount);

describe("USD (default, unchanged behavior)", () => {
  it("formats zero", () => {
    expect(formatCostCents(0)).toBe("$0.00");
  });
  it("formats cents", () => {
    expect(formatCostCents(123)).toBe("$1.23");
  });
  it("formats large amounts with grouping", () => {
    expect(formatCostCents(123456)).toBe("$1,234.56");
  });
  it("shows the sub-cent affordance for fractional USD", () => {
    expect(formatCostUsd(0.004)).toBe("<$0.01");
  });
  it("treats nullish as zero", () => {
    expect(formatCostCents(null)).toBe("$0.00");
    expect(formatCostCents(undefined)).toBe("$0.00");
  });
  it("compact drops decimals at or above 100 (mirrors fmtMoney)", () => {
    expect(formatCostUsd(1234, { compact: true })).toBe("$1,234");
    expect(formatCostUsd(99.5, { compact: true })).toBe("$99.50");
    expect(formatCostUsd(100, { compact: true })).toBe("$100");
  });
  it("explicit USD currency matches the default", () => {
    expect(formatCostCents(60, { displayCurrency: "USD" })).toBe("$0.60");
  });
});

describe("DKK / EUR conversion", () => {
  it("converts USD cents to DKK at the given rate", () => {
    // $0.60 * 6.8 = 4.08 DKK
    expect(formatCostCents(60, { displayCurrency: "DKK", rate: 6.8 })).toBe(
      ref(4.08, "DKK", "da-DK"),
    );
  });
  it("converts USD cents to EUR at the given rate", () => {
    // $2.00 * 0.92 = 1.84 EUR
    expect(formatCostCents(200, { displayCurrency: "EUR", rate: 0.92 })).toBe(
      ref(1.84, "EUR", "da-DK"),
    );
  });
  it("honors a locale override", () => {
    expect(
      formatCostCents(100, { displayCurrency: "EUR", rate: 0.9, locale: "de-DE" }),
    ).toBe(ref(0.9, "EUR", "de-DE"));
  });
});

describe("graceful degradation", () => {
  it("falls back to USD when a non-USD rate is missing", () => {
    expect(formatCostCents(60, { displayCurrency: "DKK" })).toBe("$0.60");
  });
  it("falls back to USD when the rate is invalid", () => {
    expect(formatCostCents(60, { displayCurrency: "DKK", rate: 0 })).toBe("$0.60");
    expect(
      formatCostCents(60, { displayCurrency: "DKK", rate: Number.NaN }),
    ).toBe("$0.60");
  });
});

describe("formatCostWithOriginal", () => {
  it("keeps the original USD for billing traceability", () => {
    const r = formatCostWithOriginal(60, { displayCurrency: "DKK", rate: 6.8 });
    expect(r.originalUsd).toBe("$0.60");
    expect(r.display).toBe(ref(4.08, "DKK", "da-DK"));
    expect(r.converted).toBe(true);
  });
  it("marks USD display as not converted", () => {
    const r = formatCostWithOriginal(60, { displayCurrency: "USD" });
    expect(r.converted).toBe(false);
    expect(r.display).toBe("$0.60");
    expect(r.originalUsd).toBe("$0.60");
  });
  it("marks a degraded (rate-less) conversion as not converted", () => {
    const r = formatCostWithOriginal(60, { displayCurrency: "DKK" });
    expect(r.converted).toBe(false);
    expect(r.display).toBe("$0.60");
  });
});

describe("currency helpers", () => {
  it("recognizes supported currencies", () => {
    expect(isSupportedCurrency("DKK")).toBe(true);
    expect(isSupportedCurrency("GBP")).toBe(false);
  });
  it("normalizes unknown/empty input to the default", () => {
    expect(normalizeCurrency("GBP")).toBe(DEFAULT_DISPLAY_CURRENCY);
    expect(normalizeCurrency(null)).toBe("USD");
    expect(normalizeCurrency("EUR")).toBe("EUR");
  });
});
