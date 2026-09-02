// @vitest-environment node
import { expect, it } from "vitest";
import {
  formatReferenceRate,
  hasPriceChanges,
  parsePriceDrafts,
  previewLegacyPrices,
  toPriceDraft,
} from "./pricing-drafts";

it("removes feed conversion noise only from the public reference display", () => {
  expect(formatReferenceRate(0.0000004 * 1_000_000)).toBe("0.4");
  expect(formatReferenceRate(0.0028)).toBe("0.0028");
  expect(formatReferenceRate(0.0145)).toBe("0.0145");
  expect(formatReferenceRate(0.0000000000000000028)).toBe("2.8e-18");
  expect(formatReferenceRate(0)).toBe("0");
  const row = {
    input: 0.39999999999999997,
    output: Number("0.12345678901234567"),
    cacheRead: 0,
    cacheWrite: 0,
  };
  expect(parsePriceDrafts({ custom: toPriceDraft(row) })).toEqual({
    custom: row,
  });
});

it("preserves sub-cent rates through editing", () => {
  const row = {
    input: 0.0028,
    output: 0.0145,
    cacheRead: 0.0000001,
    cacheWrite: 0,
  };
  expect(toPriceDraft(row)).toEqual({
    input: "0.0028",
    output: "0.0145",
    cacheRead: "1e-7",
    cacheWrite: "0",
  });
  expect(parsePriceDrafts({ model: toPriceDraft(row) })).toEqual({
    model: row,
  });
});

it("treats an empty row as removal rather than free pricing", () => {
  expect(parsePriceDrafts({ model: toPriceDraft() })).toEqual({});
  expect(
    parsePriceDrafts({ model: { ...toPriceDraft(), input: "1" } }),
  ).toBeNull();
});

it("rejects invalid or out-of-range rates before saving any rows", () => {
  for (const input of ["-1", "Infinity", "NaN", "1000000001", " "]) {
    expect(
      parsePriceDrafts({
        model: { input, output: "1", cacheRead: "0", cacheWrite: "0" },
      }),
    ).toBeNull();
  }
});

it("previews local imports without replacing workspace overrides or edited drafts", () => {
  const row = { input: 1, output: 2, cacheRead: 0, cacheWrite: 0 };
  const legacy = { existing: row, edited: row, empty: row, added: row };
  expect(
    previewLegacyPrices(
      legacy,
      { existing: row },
      {
        edited: { ...toPriceDraft(), input: "7" },
        empty: toPriceDraft(),
      },
    ),
  ).toEqual({ empty: row, added: row });
  expect(legacy).toHaveProperty("existing");
});

it("only treats model or rate changes as changed workspace prices", () => {
  const rate = { input: 0.0028, output: 0.0145, cacheRead: 0, cacheWrite: 1 };
  expect(
    hasPriceChanges({ model: { ...rate, source: "custom" } }, { model: rate }),
  ).toBe(false);
  expect(
    hasPriceChanges({ model: rate }, { model: { ...rate, cacheWrite: 2 } }),
  ).toBe(true);
  expect(hasPriceChanges({ model: rate }, {})).toBe(true);
  expect(hasPriceChanges({}, { model: rate })).toBe(true);
});
