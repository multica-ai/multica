import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import type { IssuePriority } from "@multica/core/types";
import { PriorityIcon } from "./priority-icon";

describe("PriorityIcon", () => {
  it("renders for every known priority", () => {
    const priorities: IssuePriority[] = ["urgent", "high", "medium", "low", "none"];
    for (const priority of priorities) {
      expect(() => render(<PriorityIcon priority={priority} />)).not.toThrow();
    }
  });

  // FIR-2662 — the same enum-drift crash class as StatusIcon: an unrecognized
  // priority value must render a neutral fallback, not throw.
  it("does not crash on an unrecognized priority", () => {
    const unknownPriority = "some_future_priority" as IssuePriority;
    expect(() => render(<PriorityIcon priority={unknownPriority} />)).not.toThrow();
  });
});
