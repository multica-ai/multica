// @vitest-environment jsdom

import { screen, fireEvent } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { IssueLinkedPullRequest } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { LinkedPRIndicator } from "./linked-pr-indicator";

const pr = (state: string, number: number): IssueLinkedPullRequest => ({
  provider: "github",
  number,
  title: `Work ${number}`,
  state,
  html_url: `https://github.com/acme/repo/pull/${number}`,
});

describe("LinkedPRIndicator", () => {
  it("hides an empty list and opens a single PR directly", () => {
    const { rerender } = renderWithI18n(<LinkedPRIndicator prs={[]} />);
    expect(screen.queryByRole("link")).toBeNull();
    rerender(<LinkedPRIndicator prs={[pr("draft", 7)]} />);
    expect(screen.getByRole("link")).toHaveAttribute("href", "https://github.com/acme/repo/pull/7");
    expect(screen.getByRole("link")).toHaveAttribute("target", "_blank");
  });

  it("lists every linked PR when there are multiple and marks failed CI", () => {
    renderWithI18n(<LinkedPRIndicator prs={[
      { ...pr("open", 7), checks_rollup: "failure", snapshot_available: true },
      pr("merged", 8),
    ]} />);
    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByRole("link", { name: /Work 7/ })).toHaveAttribute("href", "https://github.com/acme/repo/pull/7");
    expect(screen.getByRole("link", { name: /Work 8/ })).toHaveAttribute("href", "https://github.com/acme/repo/pull/8");
    expect(screen.getAllByTestId("linked-pr-ci-failed")).toHaveLength(2);
  });
});
