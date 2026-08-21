import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  RevisionConflictCompare,
  revisionConflictRows,
} from "./revision-conflict-compare";

describe("RevisionConflictCompare", () => {
  it("aligns replacements and keeps every line in the comparison", () => {
    expect(
      revisionConflictRows(
        "same\nserver only\ntail",
        "same\nlocal only\ntail\nextra",
      ),
    ).toEqual([
      {
        server: { text: "same", changed: false },
        local: { text: "same", changed: false },
      },
      {
        server: { text: "server only", changed: true },
        local: { text: "local only", changed: true },
      },
      {
        server: { text: "tail", changed: false },
        local: { text: "tail", changed: false },
      },
      { server: undefined, local: { text: "extra", changed: true } },
    ]);
  });

  it("renders a scrollable full diff with explicit additions and removals", () => {
    const serverValue = Array.from(
      { length: 12 },
      (_, index) => `server line ${index + 1}`,
    ).join("\n");
    const localValue = `${serverValue.replace(
      "server line 6",
      "local line 6",
    )}\nlocal line 13`;
    const { container } = render(
      <RevisionConflictCompare
        title="Compare both versions"
        serverLabel="Latest server version"
        localLabel="Your local version"
        serverValue={serverValue}
        localValue={localValue}
      />,
    );

    expect(screen.getAllByText("server line 12")).toHaveLength(2);
    expect(screen.getByText("local line 13")).toBeVisible();
    expect(
      container.querySelector('[data-diff-kind="remove"]'),
    ).toHaveTextContent("server line 6");
    expect(
      container.querySelectorAll('[data-diff-kind="add"]')[0],
    ).toHaveTextContent("local line 6");
    expect(container.querySelector("[data-revision-diff-scroll]")).toHaveClass(
      "max-h-80",
      "overflow-y-auto",
    );
    expect(container.querySelector("[class*='line-clamp']")).toBeNull();
  });
});
