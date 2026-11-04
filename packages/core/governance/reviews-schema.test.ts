// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ReviewListSchema, ScorecardListSchema } from "./reviews-schema";

describe("review and scorecard schemas", () => {
  it("defaults missing fields and reads an unknown rating as unknown", () => {
    const parsed = ReviewListSchema.parse({
      reviews: [{ id: "r1", rating: "great", recommendations: [{ type: "move" }] }],
    });
    const review = parsed.reviews[0];
    if (!review) throw new Error("expected one review");
    expect(review.rating).toBe("unknown");
    expect(review.narrative).toBe("");
    expect(review.recommendations[0]).toEqual({ type: "move", rationale: "", proposal_id: null, proposal_issue_id: null, refused: null });
  });

  it("keeps a known rating and a refused recommendation's reason", () => {
    const parsed = ReviewListSchema.parse({
      reviews: [
        {
          id: "r2",
          rating: "below",
          recommendations: [{ type: "hire", rationale: "x", proposal_id: null, refused: "limit reached" }],
        },
      ],
    });
    const review = parsed.reviews[0];
    const recommendation = review?.recommendations[0];
    if (!review || !recommendation) throw new Error("expected one review with one recommendation");
    expect(review.rating).toBe("below");
    expect(recommendation.refused).toBe("limit reached");
  });

  it("reads a scorecard metric with no value as null, not zero", () => {
    const parsed = ScorecardListSchema.parse({
      scorecards: [
        {
          id: "s1",
          subject_type: "agent",
          period: "month",
          status: "insufficient",
          metrics: { success_rate: { value: null, sample: 0 } },
        },
      ],
    });
    const sc = parsed.scorecards[0];
    if (!sc) throw new Error("expected one scorecard");
    expect(sc.status).toBe("insufficient");
    expect(sc.metrics.success_rate?.value).toBeNull();
  });

  it("degrades an unknown period and status to unknown", () => {
    const parsed = ScorecardListSchema.parse({ scorecards: [{ id: "s2", period: "day", status: "pending", subject_type: "robot" }] });
    expect(parsed.scorecards[0]).toMatchObject({ period: "unknown", status: "unknown", subject_type: "unknown" });
  });
});
