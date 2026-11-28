import { render, screen } from "@testing-library/react";
import ReactMarkdown from "react-markdown";
import { describe, expect, it } from "vitest";
import { remarkCommitReferences } from "./commit-references";

const repository = "https://github.com/example/project";

function renderMarkdown(content: string) {
  return render(<ReactMarkdown remarkPlugins={[[remarkCommitReferences, repository]]}>{content}</ReactMarkdown>);
}

describe("commit references in Markdown", () => {
  it("links explicit commits and ranges while preserving their visible text", () => {
    renderMarkdown("commit 716c854 and head `a61a00da`. Compare 87757b4..f98adf6.");
    expect(screen.getByRole("link", { name: "716c854" })).toHaveAttribute("href", repository + "/commit/716c854");
    const inline = screen.getByRole("link", { name: "a61a00da" });
    expect(inline).toHaveAttribute("href", repository + "/commit/a61a00da");
    expect(inline.querySelector("code")).toHaveTextContent("a61a00da");
    expect(screen.getByRole("link", { name: "87757b4..f98adf6" })).toHaveAttribute("href", repository + "/compare/87757b4..f98adf6");
  });

  it("leaves ambiguous IDs, existing links and fenced code alone", () => {
    renderMarkdown("Agent a61a00da and `fa9ce2a`. [commit 716c854](https://example.com)\n\n```\ncommit f98adf6\n```");
    expect(screen.getAllByRole("link")).toHaveLength(1);
    expect(screen.getByRole("link", { name: "commit 716c854" })).toHaveAttribute("href", "https://example.com");
    expect(screen.getByText("commit f98adf6").closest("pre")).toBeInTheDocument();
  });
});
