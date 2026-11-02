// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import type { BriefFeedback } from "./brief-feedback-schema";
import {
  BriefFeedbackOrNoneSchema,
  BriefFeedbackSummarySchema,
  isBriefFeedbackTag,
} from "./brief-feedback-schema";

const rating = {
  id: "f1",
  brief_id: "b1",
  rating: "not_useful",
  tags: ["too_long", "wrong"],
  note: "Lead with decisions",
  created_at: "2026-11-02T08:00:00Z",
  updated_at: "2026-11-02T09:00:00Z",
};

describe("BriefFeedbackOrNoneSchema", () => {
  it("reads the caller's rating", () => {
    expect(parseWithFallback(rating, BriefFeedbackOrNoneSchema, null, { endpoint: "t" })).toEqual(rating);
  });

  it("reads null as no rating yet", () => {
    expect(parseWithFallback(null, BriefFeedbackOrNoneSchema, null, { endpoint: "t" })).toBeNull();
  });

  it("reads an unknown rating as no rating rather than guessing one", () => {
    expect(
      parseWithFallback({ ...rating, rating: "meh" }, BriefFeedbackOrNoneSchema, null, { endpoint: "t" }),
    ).toBeNull();
  });

  it("drops unknown tags and keeps the known ones in order", () => {
    const parsed = parseWithFallback<BriefFeedback | null>(
      { ...rating, tags: ["too_vague", "invented", "not_for_me"] },
      BriefFeedbackOrNoneSchema,
      null,
      { endpoint: "t" },
    );
    expect(parsed?.tags).toEqual(["too_vague", "not_for_me"]);
  });

  it("treats null tags and a missing note as empty", () => {
    const parsed = parseWithFallback<BriefFeedback | null>(
      { id: "f1", brief_id: "b1", rating: "useful", tags: null },
      BriefFeedbackOrNoneSchema,
      null,
      { endpoint: "t" },
    );
    expect(parsed).toMatchObject({ rating: "useful", tags: [], note: "" });
  });
});

describe("isBriefFeedbackTag", () => {
  it("accepts the five tags and rejects others", () => {
    expect(isBriefFeedbackTag("missing_something")).toBe(true);
    expect(isBriefFeedbackTag("boring")).toBe(false);
  });
});

describe("BriefFeedbackSummarySchema", () => {
  it("reads the rolling figure", () => {
    expect(
      parseWithFallback({ days: 14, ratings: 4, useful: 3, usefulness: 0.75 }, BriefFeedbackSummarySchema, null, {
        endpoint: "t",
      }),
    ).toEqual({ days: 14, ratings: 4, useful: 3, usefulness: 0.75 });
  });

  it("keeps a null usefulness for an empty window", () => {
    expect(
      parseWithFallback({ days: 14, ratings: 0, useful: 0, usefulness: null }, BriefFeedbackSummarySchema, null, {
        endpoint: "t",
      }),
    ).toMatchObject({ ratings: 0, usefulness: null });
  });

  it("reads an out-of-range usefulness as unknown rather than showing it", () => {
    expect(
      parseWithFallback({ days: 14, ratings: 2, useful: 2, usefulness: 3 }, BriefFeedbackSummarySchema, null, {
        endpoint: "t",
      }),
    ).toMatchObject({ ratings: 2, useful: 2, usefulness: null });
  });

  it("falls back to null for a body that is not a summary", () => {
    expect(parseWithFallback("nope", BriefFeedbackSummarySchema, null, { endpoint: "t" })).toBeNull();
  });
});
