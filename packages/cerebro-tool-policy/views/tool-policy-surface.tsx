"use client";

// TECH-3156 — the shared permission surface used on all five flader (workspace,
// runtime, agent, group, member). Previously only the workspace Settings tab
// grouped the catalog into Multica / Runtime / Repos / Connections tabs; the
// other four surfaces rendered a bare <ToolPolicyTable> with no tabs, so the
// Connections group (and its per-tool config) was invisible there. This
// component owns the tab container once and every surface renders it, so the
// Connections tab — including per-connection tool config — appears everywhere.
//
// The tab row is horizontal (Mangel 3): we rely on the Tabs primitive's default
// horizontal orientation and do NOT force `flex-col`, which previously stacked
// the TabsList vertically on the workspace surface.

import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import {
  ToolPolicyTable,
  type ToolPolicyView,
} from "./tool-policy-table";

export interface ToolPolicySurfaceProps {
  wsId: string;
  /** Which page this surface is on — decides the editable layer + write subject. */
  view: ToolPolicyView;
  /**
   * The id the page's layer keys on: workspace id (workspace), runtime id
   * (runtime), agent id (agent), group id (group), or member's USER id (member).
   */
  subjectId: string;
  /** The runtime the agent runs on — shows the inherited Runtime column (agent view). */
  runtimeId?: string | null;
  /** The viewing user, so the Effective column reflects the ceiling (agent view). */
  userId?: string | null;
  groupIds?: string[];
}

// ToolPolicySurface renders the four-tab permission catalog for one surface.
// Each tab passes the same view/subject context through to ToolPolicyTable and
// only differs by `tabFilter`, which narrows the visible rows.
export function ToolPolicySurface({
  wsId,
  view,
  subjectId,
  runtimeId,
  userId,
  groupIds,
}: ToolPolicySurfaceProps) {
  const shared = { wsId, view, subjectId, runtimeId, userId, groupIds };
  return (
    <Tabs defaultValue="multica">
      <TabsList>
        <TabsTrigger value="multica">Multica</TabsTrigger>
        <TabsTrigger value="runtime">Runtime</TabsTrigger>
        <TabsTrigger value="repos">Repos</TabsTrigger>
        <TabsTrigger value="connections">Connections</TabsTrigger>
      </TabsList>
      <TabsContent value="multica" className="mt-4">
        <ToolPolicyTable {...shared} tabFilter="multica" />
      </TabsContent>
      <TabsContent value="runtime" className="mt-4">
        <ToolPolicyTable {...shared} tabFilter="runtime" />
      </TabsContent>
      <TabsContent value="repos" className="mt-4">
        <ToolPolicyTable {...shared} tabFilter="repos" />
      </TabsContent>
      <TabsContent value="connections" className="mt-4">
        <ToolPolicyTable {...shared} tabFilter="connections" />
      </TabsContent>
    </Tabs>
  );
}
