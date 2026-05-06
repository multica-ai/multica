import { describe, expect, it, vi } from "vitest";
import type { ReactElement } from "react";

import { LOCAL_PRODUCT_CAPABILITIES, type ProductCapabilities } from "../config/local-product";
import { CoreProvider } from "./core-provider";
import { ProductCapabilitiesProvider } from "./product-capabilities";

vi.mock("react", async () => {
  const actual = await vi.importActual<typeof import("react")>("react");

  return {
    ...actual,
    useMemo: <T,>(factory: () => T) => factory(),
  };
});

type ProviderElementProps = {
  value: ProductCapabilities;
};

describe("product capabilities platform context", () => {
  it("provides local product capabilities by default", () => {
    const element = ProductCapabilitiesProvider({
      children: null,
    }) as ReactElement<ProviderElementProps>;

    expect(element.props.value).toBe(LOCAL_PRODUCT_CAPABILITIES);
  });

  it("provides injected product capabilities", () => {
    const customCapabilities: ProductCapabilities = {
      ...LOCAL_PRODUCT_CAPABILITIES,
      auth: {
        ...LOCAL_PRODUCT_CAPABILITIES.auth,
        showLogin: true,
      },
    };
    const element = ProductCapabilitiesProvider({
      children: null,
      capabilities: customCapabilities,
    }) as ReactElement<ProviderElementProps>;

    expect(element.props.value).toBe(customCapabilities);
  });

  it("passes injected product capabilities through CoreProvider", () => {
    const customCapabilities: ProductCapabilities = {
      ...LOCAL_PRODUCT_CAPABILITIES,
      auth: {
        ...LOCAL_PRODUCT_CAPABILITIES.auth,
        showLogin: true,
      },
    };
    const element = CoreProvider({
      productCapabilities: customCapabilities,
      children: <div />,
    }) as ReactElement<{ capabilities: ProductCapabilities }>;

    expect(element.type).toBe(ProductCapabilitiesProvider);
    expect(element.props.capabilities).toBe(customCapabilities);
  });
});
