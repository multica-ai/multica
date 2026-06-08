"use client";

import { createElement, useMemo } from "react";
import { Coins } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { CurrencySettingsTab } from "./currency-settings-tab";

// Mirrors @multica/views ExtraSettingsTab. Defined locally to avoid the
// cerebro <-> views dependency cycle (same reasoning as the other cerebro
// settings-tab entrypoints).
interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

/**
 * The workspace display-currency tab, present only when the
 * `cerebro_display_currency` flag is on. Hook (not a const) because the flag is
 * read via a hook — mirrors useCerebroToolPolicySettingsTabs.
 */
export function useCerebroDisplayCurrencyTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_display_currency");
  return useMemo(
    () =>
      enabled
        ? [
            {
              value: "cerebro-display-currency",
              label: "Currency",
              icon: Coins,
              content: createElement(CurrencySettingsTab),
            },
          ]
        : [],
    [enabled],
  );
}
