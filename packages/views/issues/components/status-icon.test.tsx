import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import type { IssueStatus } from "@multica/core/types";
import { StatusIcon } from "./status-icon";

describe("StatusIcon", () => {
  it("renders for every known status", () => {
    const statuses: IssueStatus[] = [
      "backlog",
      "todo",
      "in_progress",
      "in_review",
      "done",
      "blocked",
      "cancelled",
    ];
    for (const status of statuses) {
      expect(() => render(<StatusIcon status={status} />)).not.toThrow();
    }
  });

  // FIR-2662 — a server-driven status value the frontend doesn't recognize
  // (enum drift) used to crash with "Cannot read properties of undefined
  // (reading 'iconColor')" and take down the whole inbox detail panel.
  it("does not crash on an unrecognized status", () => {
    const unknownStatus = "some_future_status" as IssueStatus;
    expect(() => render(<StatusIcon status={unknownStatus} />)).not.toThrow();
  });
});
