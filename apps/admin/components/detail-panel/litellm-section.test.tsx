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
          members: [],
          keySpend: null,
          cost24h: null,
          cost30d: null,
          tokens24h: null,
        }}
      />,
    );
    expect(screen.getByText("No LiteLLM key linked to this workspace.")).toBeInTheDocument();
  });

  it("renders 'No members reported' for a linked key with an empty member list", () => {
    render(
      <LiteLlmSection
        litellm={{
          linked: true,
          keyAlias: "acme-workspace",
          teamAlias: "acme-team",
          members: [],
          keySpend: 8.9,
          cost24h: 1.23,
          cost30d: 45.67,
          tokens24h: 1000,
        }}
      />,
    );
    expect(screen.getByText("No members reported.")).toBeInTheDocument();
    expect(screen.getByText("$1.23")).toBeInTheDocument();
    expect(screen.getByText("$8.90")).toBeInTheDocument();
  });

  it("renders an em dash for the key spend stat when keySpend is null", () => {
    render(
      <LiteLlmSection
        litellm={{
          linked: true,
          keyAlias: "acme-workspace",
          teamAlias: "acme-team",
          members: [],
          keySpend: null,
          cost24h: 1.23,
          cost30d: 45.67,
          tokens24h: 1000,
        }}
      />,
    );
    expect(screen.getByText("Key spend")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
