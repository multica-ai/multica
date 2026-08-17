import { describe, expect, it } from "vitest";
import { codeLowlight, highlightCode } from "./syntax-highlight";

describe("syntax highlighting", () => {
  it("renders unlabelled code as plaintext instead of auto-detecting a language", () => {
    const source = "const answer = 42;";

    const tree = codeLowlight.highlightAuto(source);

    expect(tree.data?.language).toBe("plaintext");
    expect(tree.children).toEqual([{ type: "text", value: source }]);
  });

  it("keeps highlighting explicitly registered languages", () => {
    const tree = highlightCode("const answer: number = 42;", "typescript");

    expect(tree.data?.language).toBe("typescript");
    expect(tree.children.some((child) => child.type === "element")).toBe(true);
  });

  it("renders unknown language labels as plaintext", () => {
    const source = "some domain-specific syntax";

    const tree = highlightCode(source, "not-a-registered-language");

    expect(tree.data?.language).toBe("plaintext");
    expect(tree.children).toEqual([{ type: "text", value: source }]);
  });
});
