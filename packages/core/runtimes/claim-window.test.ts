import { describe, expect, it } from "vitest";
import {
  RUNTIME_CLAIM_WINDOW_DURATION_MINUTES,
  addMinutesToHHMM,
} from "./claim-window";

describe("addMinutesToHHMM", () => {
  it("uses the fixed five-hour duration", () => {
    expect(RUNTIME_CLAIM_WINDOW_DURATION_MINUTES).toBe(300);
    expect(addMinutesToHHMM("02:00", 300)).toBe("07:00");
  });

  it("wraps across midnight", () => {
    expect(addMinutesToHHMM("23:00", 300)).toBe("04:00");
  });

  it("returns an empty preview for malformed input", () => {
    expect(addMinutesToHHMM("2:00", 300)).toBe("");
  });
});
