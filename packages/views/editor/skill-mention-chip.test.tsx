// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { SkillMentionChip } from "@multica/ui/components/common/skill-mention-chip";

afterEach(cleanup);

describe("SkillMentionChip", () => {
  it("renders with violet/purple tint classes", () => {
    render(<SkillMentionChip name="code-review" />);
    const chip = screen.getByText("code-review").closest("span.skill-mention-chip")!;
    expect(chip.className).toMatch(/bg-violet-100/);
    expect(chip.className).toMatch(/text-violet-700/);
    expect(chip.className).toMatch(/border-violet-200/);
  });

  it("renders the BookOpenText icon", () => {
    const { container } = render(<SkillMentionChip name="code-review" />);
    // BookOpenText renders an <svg> element as the first child icon.
    const chip = container.querySelector(".skill-mention-chip")!;
    const svg = chip.querySelector("svg");
    expect(svg).toBeInTheDocument();
  });

  it("displays the skill name", () => {
    render(<SkillMentionChip name="deploy-staging" />);
    expect(screen.getByText("deploy-staging")).toBeInTheDocument();
  });

  it("is not focusable by default", () => {
    render(<SkillMentionChip name="code-review" />);
    const chip = screen.getByText("code-review").closest("span.skill-mention-chip")!;
    expect(chip.getAttribute("tabIndex")).toBeNull();
  });

  it("is focusable when focusable prop is true", () => {
    render(<SkillMentionChip name="code-review" focusable />);
    const chip = screen.getByText("code-review").closest("span.skill-mention-chip")!;
    expect(chip.getAttribute("tabIndex")).toBe("0");
    expect(chip.className).toMatch(/focus-visible:ring/);
  });

  it("sets title attribute from description", () => {
    render(
      <SkillMentionChip
        name="code-review"
        description="Automated code review skill"
      />,
    );
    const chip = screen.getByText("code-review").closest("span.skill-mention-chip")!;
    expect(chip.getAttribute("title")).toBe("Automated code review skill");
  });

  it("sets the correct aria-label", () => {
    render(<SkillMentionChip name="deploy-staging" />);
    const chip = screen.getByLabelText("Skill: deploy-staging");
    expect(chip).toBeInTheDocument();
  });
});
