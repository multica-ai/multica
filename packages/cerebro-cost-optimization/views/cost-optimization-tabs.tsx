"use client";

import { useCallback, useMemo } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { CostOptimizationSettingsPanel } from "./settings-panel";
import { CostOptimizationAnalyticsTab } from "./analytics-tab";
import { CostOptimizationInspectorTab } from "./inspector-tab";

export type CostOptimizationSubtab = "settings" | "analytics" | "inspector";

const SUBTAB_KEY = "subtab";
const DEFAULT_SUBTAB: CostOptimizationSubtab = "settings";
const VALID_SUBTABS = new Set<CostOptimizationSubtab>([
  "settings",
  "analytics",
  "inspector",
]);

export function CostOptimizationTabs() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const activeSubtab = useMemo(() => {
    const raw = searchParams.get(SUBTAB_KEY);
    if (raw && VALID_SUBTABS.has(raw as CostOptimizationSubtab)) {
      return raw as CostOptimizationSubtab;
    }
    return DEFAULT_SUBTAB;
  }, [searchParams]);

  const onSubtabChange = useCallback(
    (value: string) => {
      const params = new URLSearchParams(searchParams.toString());
      params.set(SUBTAB_KEY, value);
      router.replace(`${pathname}?${params.toString()}`);
    },
    [pathname, router, searchParams],
  );

  return (
    <Tabs value={activeSubtab} onValueChange={onSubtabChange} className="space-y-4">
      <TabsList>
        <TabsTrigger value="settings">Settings</TabsTrigger>
        <TabsTrigger value="analytics">Analytics</TabsTrigger>
        <TabsTrigger value="inspector">System prompt inspector</TabsTrigger>
      </TabsList>
      <TabsContent value="settings">
        <CostOptimizationSettingsPanel />
      </TabsContent>
      <TabsContent value="analytics">
        <CostOptimizationAnalyticsTab />
      </TabsContent>
      <TabsContent value="inspector">
        <CostOptimizationInspectorTab />
      </TabsContent>
    </Tabs>
  );
}
