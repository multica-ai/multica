// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { FeatureFlagsProvider, useFlag, useVariant } from "./context";
import { FeatureFlagService } from "./service";
import { StaticProvider } from "./static-provider";

function FlagBadge({ flagKey, defaultValue }: { flagKey: string; defaultValue: boolean }) {
  const enabled = useFlag(flagKey, defaultValue);
  return <span data-testid="flag">{enabled ? "ON" : "OFF"}</span>;
}

function VariantBadge({ flagKey, defaultValue }: { flagKey: string; defaultValue: string }) {
  const variant = useVariant(flagKey, defaultValue);
  return <span data-testid="variant">{variant}</span>;
}

describe("FeatureFlagsProvider + hooks", () => {
  it("useFlag returns provider value inside the tree", () => {
    const service = new FeatureFlagService(
      new StaticProvider({ demo: { default: true } }),
    );
    render(
      <FeatureFlagsProvider service={service}>
        <FlagBadge flagKey="demo" defaultValue={false} />
      </FeatureFlagsProvider>,
    );
    expect(screen.getByTestId("flag").textContent).toBe("ON");
  });

  it("useFlag falls back to default outside any provider (tests / stories)", () => {
    render(<FlagBadge flagKey="anything" defaultValue={true} />);
    expect(screen.getByTestId("flag").textContent).toBe("ON");
  });

  it("useFlag respects the EvalContext attached to the provider", () => {
    const service = new FeatureFlagService(
      new StaticProvider({
        internal: { default: false, allow: ["user-internal"] },
      }),
    );
    render(
      <FeatureFlagsProvider service={service} context={{ userId: "user-internal" }}>
        <FlagBadge flagKey="internal" defaultValue={false} />
      </FeatureFlagsProvider>,
    );
    expect(screen.getByTestId("flag").textContent).toBe("ON");
  });

  it("useVariant returns the variant identifier", () => {
    const service = new FeatureFlagService(
      new StaticProvider({
        algo: { default: true, variant: "experiment-v2" },
      }),
    );
    render(
      <FeatureFlagsProvider service={service}>
        <VariantBadge flagKey="algo" defaultValue="control" />
      </FeatureFlagsProvider>,
    );
    expect(screen.getByTestId("variant").textContent).toBe("experiment-v2");
  });

  it("useVariant falls back to default outside any provider", () => {
    render(<VariantBadge flagKey="algo" defaultValue="control" />);
    expect(screen.getByTestId("variant").textContent).toBe("control");
  });
});
