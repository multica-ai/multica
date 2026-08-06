import { describe, expect, it } from "vitest";
import { splitSkillMarkdown } from "./markdown";

describe("splitSkillMarkdown", () => {
  it("keeps a YAML prefix byte-for-byte while exposing only the Markdown body", () => {
    const content = "---\nname: release-check\ntags:\n  - deploy\n---\n# Release\n";

    expect(splitSkillMarkdown(content)).toEqual({
      frontmatter: "---\nname: release-check\ntags:\n  - deploy\n---\n",
      body: "# Release\n",
    });
  });

  it("keeps a document without frontmatter intact", () => {
    expect(splitSkillMarkdown("# Plain document\n")).toEqual({
      frontmatter: "",
      body: "# Plain document\n",
    });
  });
});
