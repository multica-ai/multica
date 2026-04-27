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
  it("renders real blank lines as separate markdown paragraphs", () => {
    const { container } = render(
      <ReadonlyContent content={["First paragraph", "", "Second paragraph"].join("\n")} />,
    );

    const paragraphs = Array.from(container.querySelectorAll("p")).map((p) => p.textContent);
    expect(paragraphs).toEqual(["First paragraph", "Second paragraph"]);
  });

  it("does not decode escaped newline text at render time", () => {
    const { container } = render(
      <ReadonlyContent content={"First paragraph\\n\\nSecond paragraph"} />,
    );

    expect(container.querySelectorAll("p")).toHaveLength(1);
    expect(container.textContent).toContain("First paragraph\\n\\nSecond paragraph");
  });

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
