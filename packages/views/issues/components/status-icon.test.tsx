// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { PriorityIcon } from "./priority-icon";
import { StatusIcon } from "./status-icon";
import { ISSUE_STATUS_ICONS } from "@multica/core/types/issue-status";

afterEach(cleanup);

describe("issue icons", () => {
  it.each(ISSUE_STATUS_ICONS)("renders %s independently of category", (icon) => {
    const { container, rerender } = render(<StatusIcon status="qa" category="started" color="#123456" icon={icon} />);
    const geometry = container.querySelector("svg")!.innerHTML;
    rerender(<StatusIcon status="qa" category="closed" color="#123456" icon={icon} />);
    expect(container.querySelector("svg")!.innerHTML).toBe(geometry);
    expect(container.querySelector("svg")!.style.color).toBe("var(--palette-blue, #123456)");
  });

  it("names a custom colour's slot so a theme can recolour it", () => {
    const { container, rerender } = render(<StatusIcon status="qa" category="started" color="#ef4444" />);
    const svg = container.querySelector("svg")!;
    expect(svg).toHaveAttribute("data-color", "#ef4444");
    expect(svg).toHaveAttribute("data-palette", "red");
    expect(svg.style.color).toBe("var(--palette-red, #ef4444)");
    rerender(<StatusIcon status="qa" category="started" color="#ef4444" inheritColor />);
    expect(svg).toHaveAttribute("data-palette", "red");
    expect(svg.style.color).toBe("");
    rerender(<StatusIcon status="done" color="#ef4444" />);
    expect(svg).not.toHaveAttribute("data-color");
    expect(svg).not.toHaveAttribute("data-palette");
  });

  it("keeps built-in geometry and token color locked", () => {
    const { container, rerender } = render(<StatusIcon status="in_review" />);
    const markup = container.innerHTML;
    rerender(<StatusIcon status="in_review" icon="cross" color="#123456" />);
    expect(container.innerHTML).toBe(markup);
  });

  it.each([undefined, null, "", "future-icon", "toString", "__proto__"])("falls back safely for %s", (icon) => {
    const { container, rerender } = render(<StatusIcon status="qa" category="started" />);
    const markup = container.innerHTML;
    rerender(<StatusIcon status="qa" category="started" icon={icon} />);
    expect(container.innerHTML).toBe(markup);
  });
  it("renders a muted fallback for unknown status values", () => {
    const { container } = render(<StatusIcon status="unexpected_status" />);

    const icon = container.querySelector("svg");
    expect(icon).toHaveClass("text-muted-foreground");
  });

  it("renders a muted fallback for unknown priority values", () => {
    const { container } = render(<PriorityIcon priority="unexpected_priority" />);

    const icon = container.querySelector("svg");
    expect(icon).toHaveClass("text-priority-none");
  });
});
