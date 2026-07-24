import { describe, expect, it } from "vitest";
import { approvalExperienceEnabled } from "./availability";

describe("approvalExperienceEnabled", () => {
  it.each([
    { inbox: false, gate: false, visible: false },
    { inbox: true, gate: false, visible: true },
    { inbox: false, gate: true, visible: true },
    { inbox: true, gate: true, visible: true },
  ])("keeps a human decision path whenever Ask enforcement is active", ({ inbox, gate, visible }) => {
    expect(approvalExperienceEnabled(inbox, gate)).toBe(visible);
  });
});
