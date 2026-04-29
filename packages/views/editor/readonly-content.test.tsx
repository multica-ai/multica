import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
  }),
  useWorkspaceSlug: () => "test",
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId, fallbackLabel }: { issueId: string; fallbackLabel?: string }) => (
    <span data-testid="issue-mention-card">{fallbackLabel ?? issueId}</span>
  ),
}));

vi.mock("./extensions/image-view", () => ({
  ImageLightbox: () => null,
}));

vi.mock("./link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

vi.mock("./utils/link-handler", () => ({
  openLink: vi.fn(),
  isMentionHref: (href?: string) => Boolean(href?.startsWith("mention://")),
}));

import { ReadonlyContent } from "./readonly-content";

describe("ReadonlyContent math rendering", () => {
  it("renders inline and block LaTeX with KaTeX markup", () => {
    const { container } = render(
      <ReadonlyContent
        content={[
          "Inline math: $E = mc^2$",
          "",
          "$$",
          "\\int_0^1 x^2 \\, dx",
          "$$",
        ].join("\n")}
      />,
    );

    const text = container.textContent?.replace(/\s+/g, " ") ?? "";
    expect(container.querySelectorAll(".katex").length).toBeGreaterThanOrEqual(2);
    expect(container.querySelector(".katex-display")).not.toBeNull();
    expect(text).toContain("E = mc^2");
    expect(text).toContain("\\int_0^1 x^2 \\, dx");
  });
});

describe("ReadonlyContent line breaks", () => {
  // Issue panel comments are the primary user-visible surface for agent
  // output. CommonMark's default soft-break behavior collapses single
  // newlines into spaces; agent text often relies on a single newline as a
  // visible break. remark-breaks must remain wired into ReadonlyContent's
  // remark plugin chain or comments lose their formatting again.
  it("converts a single newline into a <br>", () => {
    const { container } = render(<ReadonlyContent content={"line one\nline two"} />);
    expect(container.querySelector("br")).not.toBeNull();
  });

  it("renders a blank-line gap as separate paragraphs", () => {
    const { container } = render(<ReadonlyContent content={"para one\n\npara two"} />);
    expect(container.querySelectorAll("p").length).toBeGreaterThanOrEqual(2);
  });
});
