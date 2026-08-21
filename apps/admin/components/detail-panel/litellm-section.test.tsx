// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { LiteLlmSection } from "./litellm-section";

describe("LiteLlmSection", () => {
  it("renders the 'not linked' empty state instead of fabricated numbers", () => {
    render(
      <LiteLlmSection
        litellm={{
          linked: false,
          keyAlias: null,
          teamAlias: null,
          keySpend: null,
          costPerTicket: null,
        }}
      />,
    );
    expect(screen.getByText("No LiteLLM key linked to this workspace.")).toBeInTheDocument();
  });

  it("renders cost per active ticket for a linked key", () => {
    render(
      <LiteLlmSection
        litellm={{
          linked: true,
          keyAlias: "acme-workspace",
          teamAlias: "acme-team",
          keySpend: 8.9,
          costPerTicket: 2.5,
        }}
      />,
    );
    expect(screen.getByText("Cost / active ticket")).toBeInTheDocument();
    expect(screen.getByText("$8.90")).toBeInTheDocument();
    expect(screen.getByText("$2.50")).toBeInTheDocument();
  });

  it("renders an em dash for cost per ticket when there are no active tickets to divide by", () => {
    render(
      <LiteLlmSection
        litellm={{
          linked: true,
          keyAlias: "acme-workspace",
          teamAlias: "acme-team",
          keySpend: null,
          costPerTicket: null,
        }}
      />,
    );
    expect(screen.getByText("Key spend")).toBeInTheDocument();
    expect(screen.getAllByText("—")).toHaveLength(2);
  });
});
