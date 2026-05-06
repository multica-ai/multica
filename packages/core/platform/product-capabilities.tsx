"use client";

import { createContext, useContext, type ReactNode } from "react";

import {
  LOCAL_PRODUCT_CAPABILITIES,
  type ProductCapabilities,
} from "../config/local-product";

const ProductCapabilitiesContext = createContext<ProductCapabilities>(
  LOCAL_PRODUCT_CAPABILITIES,
);

export function ProductCapabilitiesProvider({
  children,
  capabilities = LOCAL_PRODUCT_CAPABILITIES,
}: {
  children: ReactNode;
  capabilities?: ProductCapabilities;
}) {
  return (
    <ProductCapabilitiesContext.Provider value={capabilities}>
      {children}
    </ProductCapabilitiesContext.Provider>
  );
}

export function useProductCapabilities(): ProductCapabilities {
  return useContext(ProductCapabilitiesContext);
}
