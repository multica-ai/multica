"use client";

import { createElement, useMemo } from "react";
import { SlidersHorizontal } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { ModeSettingsTab } from "./mode-settings-tab";

export function useCerebroModeSettingsTabs() {
  const enabled = useFeatureFlag("cerebro_session_modes");
  return useMemo(() => enabled ? [{
    value: "cerebro-modes",
    label: "Modes",
    icon: SlidersHorizontal,
    content: createElement(ModeSettingsTab),
    wide: true,
  }] : [], [enabled]);
}
