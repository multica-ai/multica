"use client";

import { createContext, useContext } from "react";

/**
 * The page header's middle slot. The floor tabs and their actions render into it,
 * so the floor plan needs no row of its own under the header. Null outside the
 * company page, where the controls fall back to rendering in place.
 */
export const CompanyHeaderSlotContext = createContext<HTMLElement | null>(null);

export function useCompanyHeaderSlot() {
  return useContext(CompanyHeaderSlotContext);
}
