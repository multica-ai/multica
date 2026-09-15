import { describe, expect, it } from "vitest";
import { extractLatexDelimiters } from "./latex-delimiters";

describe("extractLatexDelimiters", () => {
  it("extracts paired inline and display LaTeX delimiters", () => {
    const result = extractLatexDelimiters(
      String.raw`Inline \(E=mc^2\).

\[
\begin{pmatrix}
1 & 2 \\
3 & 4
\end{pmatrix}
\]`,
    );

    expect(result.formulas).toEqual([
      { kind: "inline", value: "E=mc^2" },
      {
        kind: "display",
        value: "\n\\begin{pmatrix}\n1 & 2 \\\\\n3 & 4\n\\end{pmatrix}\n",
      },
    ]);
    expect(result.markdown).not.toContain(String.raw`\(E=mc^2\)`);
    expect(result.markdown).not.toContain(String.raw`\begin{pmatrix}`);
  });

  it("leaves delimiters literal in inline and fenced code", () => {
    const markdown = [
      "Keep `\\(x\\) and \\[y\\]` inside code",
      "",
      "~~~tex",
      String.raw`\[z\]`,
      "~~~",
    ].join("\n");

    expect(extractLatexDelimiters(markdown)).toEqual({
      markdown,
      formulas: [],
    });
  });

  it("leaves delimiters literal in CommonMark list and quote fences", () => {
    const markdown = [
      "- ~~~~tex",
      String.raw`  \[list\]`,
      "  ~~~~",
      "",
      "> ```tex",
      String.raw`> \(quote\)`,
      "> ```",
    ].join("\n");

    expect(extractLatexDelimiters(markdown)).toEqual({
      markdown,
      formulas: [],
    });
  });

  it("leaves deliberately escaped and incomplete delimiters literal", () => {
    const markdown = [
      String.raw`Literal \\(x\\) and \\[y\\].`,
      String.raw`Streaming \(x + y`,
      String.raw`Streaming \[x + y`,
    ].join("\n");

    expect(extractLatexDelimiters(markdown)).toEqual({
      markdown,
      formulas: [],
    });
  });

  it("does not alter existing dollar math or currency text", () => {
    const markdown = [
      "$$x^2 + y^2$$",
      "Item A costs $120 and item B costs $85.",
    ].join("\n\n");

    expect(extractLatexDelimiters(markdown)).toEqual({
      markdown,
      formulas: [],
    });
  });

  it("ignores delimiters in HTML attributes and Markdown link destinations", () => {
    const markdown = [
      String.raw`<span title="\(attribute\)">plain</span>`,
      String.raw`[target](https://example.com/\(literal\))`,
    ].join("\n\n");

    expect(extractLatexDelimiters(markdown)).toEqual({
      markdown,
      formulas: [],
    });
  });

  it("never reuses a placeholder token found in user content", () => {
    const userToken = "\uE000multica-latex-0\uE001";
    const result = extractLatexDelimiters(
      `${userToken} and ${String.raw`\(x\)`}`,
    );

    expect(result.formulas).toEqual([{ kind: "inline", value: "x" }]);
    expect(result.markdown.split(userToken)).toHaveLength(2);
  });
});
