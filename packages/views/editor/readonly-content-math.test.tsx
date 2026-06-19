import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { Markdown } from "@multica/ui/markdown";
import { ReadonlyContent } from "./readonly-content";

// Prose with two dollar amounts and `~` (approximately) markers. With
// remark-math's default single-dollar parsing, everything between the two
// `$` signs is swallowed into a KaTeX inline-math span and rendered in an
// italic math font, with `~` treated as a TeX non-breaking space.
const FINANCE_TEXT =
  "Revenue ≈ $120/mo gross (~$85 net of fees), 12 active subscriptions";

describe("dollar amounts in markdown", () => {
  it("Markdown renders $ amounts as plain text, not inline math", () => {
    const { container } = render(<Markdown>{FINANCE_TEXT}</Markdown>);
    expect(container.querySelector(".katex")).toBeNull();
    expect(container.textContent).toContain(
      "$120/mo gross (~$85 net of fees)",
    );
  });

  it("ReadonlyContent renders $ amounts as plain text, not inline math", () => {
    const { container } = render(<ReadonlyContent content={FINANCE_TEXT} />);
    expect(container.querySelector(".katex")).toBeNull();
    expect(container.textContent).toContain(
      "$120/mo gross (~$85 net of fees)",
    );
  });

  it("still renders explicit $$ display math", () => {
    const { container } = render(<Markdown>{"$$\nE = mc^2\n$$"}</Markdown>);
    expect(container.querySelector(".katex")).not.toBeNull();
  });
});
